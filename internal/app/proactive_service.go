package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/conversation"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/platform"
	"github.com/longyisang/emoagent/internal/proactive"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/turn"
)

// ProactiveService drives the one path in EmoAgent that can message the user
// without being asked. It owns nothing clever: the decision chain lives in
// internal/proactive, and delivery reuses the platform sinks. What it does own
// is the ordering — decide, then run a real Emotion turn, then deliver, then
// record what actually happened.
type ProactiveService struct {
	infra        *Infra
	chat         *ChatService
	platforms    *PlatformService
	personas     *PersonaService
	agentRuntime *AgentRuntimeService
	conversation *ConversationService
	agentAffect  *AgentAffectService
	promptCenter *PromptCenterService
	runner       *proactive.Runner
	logger       *slog.Logger
}

func NewProactiveService(
	infra *Infra,
	chat *ChatService,
	platforms *PlatformService,
	personas *PersonaService,
	agentRuntime *AgentRuntimeService,
	conversationService *ConversationService,
	agentAffect *AgentAffectService,
	promptCenter *PromptCenterService,
) *ProactiveService {
	service := &ProactiveService{
		infra:        infra,
		chat:         chat,
		platforms:    platforms,
		personas:     personas,
		agentRuntime: agentRuntime,
		conversation: conversationService,
		agentAffect:  agentAffect,
		promptCenter: promptCenter,
	}
	if infra != nil {
		service.logger = infra.Logger
	}
	return service
}

// Configure rebuilds the runner against the current config. Called at startup
// and whenever the proactive settings change.
func (s *ProactiveService) Configure(cfg config.ProactiveConfig) {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return
	}
	cfg = config.NormalizeProactiveConfig(cfg)

	client := s.infra.LLM
	model := ""
	params := llm.RequestParams{}
	if s.agentRuntime != nil {
		if runtime := s.agentRuntime.Active(); runtime != nil {
			client = runtime.EmotionMain.Client
			model = runtime.EmotionMain.Model
			params = cloneRequestParams(runtime.EmotionMain.Params)
		}
	}
	gate := proactive.NewGate(client, model, params, cfg.Gate, s.logger)
	if s.promptCenter != nil {
		gate = gate.WithPromptResolver(s.promptCenter.Resolver())
	}

	s.runner = proactive.NewRunner(
		s.infra.DB,
		gate,
		&runRegistryPresence{conversation: s.conversation},
		&runtimeSettingsQuietStore{db: s.infra.DB},
		&hostContextProvider{db: s.infra.DB, agentAffect: s.agentAffect},
		s.logger,
	)
}

// Tick runs one decision pass for a persona and, if the chain allows it,
// delivers a message. It returns the decision so callers can report why nothing
// was sent — which is the normal outcome.
func (s *ProactiveService) Tick(ctx context.Context, personaKey string) (proactive.Decision, error) {
	if s == nil || s.runner == nil {
		return proactive.Decision{Verdict: proactive.VerdictSkip, Reason: proactive.ReasonNotConfigured}, nil
	}
	cfg := config.DefaultProactiveConfig()
	if s.infra != nil && s.infra.Config != nil {
		cfg = s.infra.Config.Proactive
	}

	evaluation, err := s.runner.Evaluate(ctx, cfg, personaKey)
	if err != nil {
		return proactive.Decision{}, err
	}
	if evaluation.Decision.Verdict != proactive.VerdictSpeak {
		return evaluation.Decision, nil
	}
	if err := s.deliver(ctx, cfg, evaluation); err != nil {
		return evaluation.Decision, err
	}
	return evaluation.Decision, nil
}

func (s *ProactiveService) deliver(ctx context.Context, cfg config.ProactiveConfig, evaluation proactive.Evaluation) error {
	if s.chat == nil {
		return fmt.Errorf("chat service is not configured")
	}
	sink, ok := s.platforms.OutboundFor(evaluation.Target.AdapterInstanceID)
	if !ok {
		return fmt.Errorf("no outbound sink for adapter %q", evaluation.Target.AdapterInstanceID)
	}

	// Never talk over an in-flight reply. The valves already checked this, but
	// the registry is the authority and time has passed since.
	runCtx := ctx
	done := func() {}
	if s.conversation != nil && s.conversation.RunRegistry() != nil {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithCancel(ctx)
		unregister, registered := s.conversation.RunRegistry().TryRegister(conversation.RunRef{
			OriginKey: evaluation.Target.Origin.OriginKey,
			SessionID: evaluation.Target.SessionID,
			Kind:      "proactive",
		}, cancel)
		if !registered {
			cancel()
			return s.recordOutcome(ctx, evaluation.DecisionID, "", true)
		}
		done = func() {
			unregister()
			cancel()
		}
	}
	defer done()

	persona, ok := s.personas.Get(evaluation.PersonaKey)
	if !ok || persona == nil {
		return fmt.Errorf("persona not found: %s", evaluation.PersonaKey)
	}
	result, events, err := s.chat.SendProactiveTurn(runCtx, persona, evaluation, cfg)
	if err != nil {
		return err
	}

	delivered := turn.HasUserVisibleContent(events)
	if delivered {
		for _, event := range events {
			if !turn.IsUserVisibleEvent(event) || strings.TrimSpace(event.Content) == "" {
				continue
			}
			if emitErr := sink.Emit(ctx, platform.OutboundEvent{
				Type:       event.Type,
				Origin:     evaluation.Target.Origin,
				SessionID:  evaluation.Target.SessionID,
				PersonaKey: evaluation.PersonaKey,
				Content:    event.Content,
				Payload:    event.Payload,
			}); emitErr != nil {
				// A stale proactive message is worse than none, so no retry.
				s.warn("proactive delivery failed", emitErr)
				break
			}
		}
	}

	if err := s.recordOutcome(ctx, evaluation.DecisionID, result.TurnID, !delivered); err != nil {
		return err
	}
	if delivered && len(evaluation.Decision.CandidateIDs) > 0 {
		if err := s.infra.DB.MarkProactiveCandidates(ctx, evaluation.Decision.CandidateIDs,
			storage.ProactiveCandidateStatusConsumed, evaluation.DecisionID); err != nil {
			s.warn("mark consumed candidates failed", err)
		}
	}
	return nil
}

func (s *ProactiveService) recordOutcome(ctx context.Context, decisionID, turnID string, silenced bool) error {
	if s.infra == nil || s.infra.DB == nil {
		return nil
	}
	return s.infra.DB.UpdateProactiveDecisionOutcome(ctx, decisionID, turnID, silenced)
}

// NoteUserMessage attributes an incoming user message to the most recent
// proactive message on the same origin. This is the only real feedback the gate
// gets about whether its interruptions were welcome.
func (s *ProactiveService) NoteUserMessage(ctx context.Context, originKey string, at time.Time) {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return
	}
	cfg := config.DefaultProactiveConfig()
	if s.infra.Config != nil {
		cfg = config.NormalizeProactiveConfig(s.infra.Config.Proactive)
	}
	window := time.Duration(cfg.Cooldown.ReplyAttributionWindowMinutes) * time.Minute
	if _, err := s.infra.DB.BackfillProactiveUserReplied(ctx, originKey, at, window); err != nil {
		s.warn("backfill proactive reply failed", err)
	}
}

func (s *ProactiveService) warn(msg string, err error) {
	if s != nil && s.logger != nil {
		s.logger.Warn(msg, "error", err)
	}
}

// runRegistryPresence answers "is the user mid-conversation right now" from the
// in-flight run registry.
type runRegistryPresence struct {
	conversation *ConversationService
}

func (p *runRegistryPresence) HasActiveRun(string) bool {
	if p == nil || p.conversation == nil || p.conversation.RunRegistry() == nil {
		return false
	}
	return p.conversation.RunRegistry().HasActiveRuns()
}

// runtimeSettingsQuietStore reads the /quiet mute deadline. Mute state is runtime
// state, not deployment config, so it lives in runtime_settings.
type runtimeSettingsQuietStore struct {
	db *storage.DB
}

const (
	quietSettingScope = "proactive"
	quietSettingKey   = "quiet_until"
)

func (q *runtimeSettingsQuietStore) QuietUntil(context.Context) (time.Time, bool) {
	if q == nil || q.db == nil {
		return time.Time{}, false
	}
	setting, ok, err := q.db.GetRuntimeSetting(quietSettingScope, quietSettingKey)
	if err != nil || !ok {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.Trim(strings.TrimSpace(setting.ValueJSON), `"`))
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}
