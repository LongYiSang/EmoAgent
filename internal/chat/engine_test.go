package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/progress"
	"github.com/longyisang/emoagent/internal/promptcenter"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/turn"
	"github.com/longyisang/emoagent/internal/work"
)

type fakeLLMClient struct {
	lastRequest  llm.ChatRequest
	chatRequests []llm.ChatRequest
	chatResponse *llm.ChatResponse
	chatErr      error
	response     *llm.ChatResponse
	err          error
	deltas       []string
}

func (f *fakeLLMClient) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	f.chatRequests = append(f.chatRequests, req)
	if f.chatResponse != nil || f.chatErr != nil {
		return f.chatResponse, f.chatErr
	}
	return &llm.ChatResponse{
		ID:      "summary-1",
		Model:   req.Model,
		Content: engineSummaryContent("summarized"),
	}, nil
}

func (f *fakeLLMClient) ChatStream(_ context.Context, req llm.ChatRequest, cb llm.StreamCallback) (*llm.ChatResponse, error) {
	f.lastRequest = req
	for _, delta := range f.deltas {
		if cb != nil {
			cb(llm.StreamEvent{Content: delta})
		}
	}
	if cb != nil {
		cb(llm.StreamEvent{Done: true})
	}
	return f.response, f.err
}

type fakeMemoryBridge struct {
	ensureResult    MemorySegmentRef
	rolloverResult  MemorySegmentRef
	ensureErr       error
	rolloverErr     error
	appendUserErr   error
	appendAssistErr error
	retrieveBlock   string
	retrieveTrace   any
	retrieveErr     error
	manualNotice    string
	manualNoticeOK  bool

	ensureCalls    []memoryEnsureCall
	rolloverCalls  []memoryRolloverCall
	userEpisodes   []memoryEpisodeCall
	assistEpisodes []memoryEpisodeCall
	retrieveCalls  []memoryRetrieveCall
}

type memoryEnsureCall struct {
	ChatSessionID string
	PersonaID     string
}

type memoryRolloverCall struct {
	ChatSessionID string
	PersonaID     string
	Reason        string
}

type memoryEpisodeCall struct {
	SegmentID string
	MessageID string
	Content   string
}

type memoryRetrieveCall struct {
	ChatSessionID      string
	Query              string
	IncludePipeline    bool
	ExcludedEpisodeIDs []string
}

func (f *fakeMemoryBridge) EnsureSegment(_ context.Context, chatSessionID string, personaID string) (MemorySegmentRef, error) {
	f.ensureCalls = append(f.ensureCalls, memoryEnsureCall{ChatSessionID: chatSessionID, PersonaID: personaID})
	if f.ensureErr != nil {
		return MemorySegmentRef{}, f.ensureErr
	}
	if f.ensureResult.SegmentID == "" {
		f.ensureResult = MemorySegmentRef{SegmentID: "segment-current", MemorySessionID: "memory-current"}
	}
	return f.ensureResult, nil
}

func (f *fakeMemoryBridge) RolloverSegment(_ context.Context, chatSessionID string, personaID string, reason string) (MemorySegmentRef, error) {
	f.rolloverCalls = append(f.rolloverCalls, memoryRolloverCall{ChatSessionID: chatSessionID, PersonaID: personaID, Reason: reason})
	if f.rolloverErr != nil {
		return MemorySegmentRef{}, f.rolloverErr
	}
	if f.rolloverResult.SegmentID == "" {
		f.rolloverResult = MemorySegmentRef{SegmentID: "segment-next", MemorySessionID: "memory-next"}
	}
	return f.rolloverResult, nil
}

func (f *fakeMemoryBridge) AppendUserEpisode(_ context.Context, segmentID string, messageID string, content string) (string, error) {
	f.userEpisodes = append(f.userEpisodes, memoryEpisodeCall{SegmentID: segmentID, MessageID: messageID, Content: content})
	if f.appendUserErr != nil {
		return "", f.appendUserErr
	}
	return "episode-user", nil
}

func (f *fakeMemoryBridge) AppendAssistantEpisode(_ context.Context, segmentID string, messageID string, content string) (string, error) {
	f.assistEpisodes = append(f.assistEpisodes, memoryEpisodeCall{SegmentID: segmentID, MessageID: messageID, Content: content})
	if f.appendAssistErr != nil {
		return "", f.appendAssistErr
	}
	return "episode-assistant", nil
}

func (f *fakeMemoryBridge) RetrievePromptBlock(_ context.Context, chatSessionID string, query string, excludedEpisodeIDs ...string) (string, error) {
	block, _, err := f.retrievePrompt(chatSessionID, query, false, excludedEpisodeIDs...)
	return block, err
}

func (f *fakeMemoryBridge) RetrievePromptSnapshot(_ context.Context, chatSessionID string, query string, includePipelineTrace bool, excludedEpisodeIDs ...string) (string, any, error) {
	return f.retrievePrompt(chatSessionID, query, includePipelineTrace, excludedEpisodeIDs...)
}

func (f *fakeMemoryBridge) retrievePrompt(chatSessionID string, query string, includePipelineTrace bool, excludedEpisodeIDs ...string) (string, any, error) {
	f.retrieveCalls = append(f.retrieveCalls, memoryRetrieveCall{
		ChatSessionID:      chatSessionID,
		Query:              query,
		IncludePipeline:    includePipelineTrace,
		ExcludedEpisodeIDs: append([]string(nil), excludedEpisodeIDs...),
	})
	if f.retrieveErr != nil {
		return "", nil, f.retrieveErr
	}
	return f.retrieveBlock, f.retrieveTrace, nil
}

func (f *fakeMemoryBridge) FinalizeSegment(context.Context, string, string, string) error {
	return nil
}

func (f *fakeMemoryBridge) TakeManualMemoryNotice(chatSessionID string) (string, bool) {
	if !f.manualNoticeOK {
		return "", false
	}
	f.manualNoticeOK = false
	return f.manualNotice, true
}

func engineSummaryContent(goal string) string {
	payload := map[string]any{
		"running_summary": map[string]any{
			"session_goal": goal,
			"user_facts":   []string{},
			"relationship_state": map[string]any{
				"tone":           "",
				"recent_emotion": "",
				"promises_made":  []string{},
			},
			"open_loops":    []string{},
			"decisions":     []string{},
			"do_not_forget": []string{},
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return string(b)
}

type summaryDeadlineClient struct {
	hadDeadline bool
	timeUntil   time.Duration
}

func (c *summaryDeadlineClient) Chat(ctx context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	deadline, ok := ctx.Deadline()
	c.hadDeadline = ok
	if ok {
		c.timeUntil = time.Until(deadline)
	}
	return nil, context.DeadlineExceeded
}

func (c *summaryDeadlineClient) ChatStream(_ context.Context, _ llm.ChatRequest, cb llm.StreamCallback) (*llm.ChatResponse, error) {
	if cb != nil {
		cb(llm.StreamEvent{Content: "ok"})
		cb(llm.StreamEvent{Done: true})
	}
	return endTurnResponse("ok"), nil
}

type observingStreamClient struct {
	deltas     []string
	response   *llm.ChatResponse
	afterDelta func()
}

func (c *observingStreamClient) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	panic("unexpected summary Chat call")
}

func (c *observingStreamClient) ChatStream(_ context.Context, req llm.ChatRequest, cb llm.StreamCallback) (*llm.ChatResponse, error) {
	for _, delta := range c.deltas {
		if cb != nil {
			cb(llm.StreamEvent{Content: delta})
		}
		if c.afterDelta != nil {
			c.afterDelta()
		}
	}
	if cb != nil {
		cb(llm.StreamEvent{Done: true})
	}
	if c.response != nil {
		return c.response, nil
	}
	return endTurnResponse(strings.Join(c.deltas, "")), nil
}

type reasoningStreamClient struct {
	requests []llm.ChatRequest
	events   []llm.StreamEvent
	response *llm.ChatResponse
}

func (c *reasoningStreamClient) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	panic("unexpected summary Chat call")
}

func (c *reasoningStreamClient) ChatStream(_ context.Context, req llm.ChatRequest, cb llm.StreamCallback) (*llm.ChatResponse, error) {
	c.requests = append(c.requests, req)
	for _, event := range c.events {
		if cb != nil {
			cb(event)
		}
	}
	if cb != nil {
		cb(llm.StreamEvent{Done: true})
	}
	if c.response != nil {
		return c.response, nil
	}
	return endTurnResponse("ok"), nil
}

type reactiveRetryLLMClient struct {
	requests []llm.ChatRequest
	errs     []error
	response *llm.ChatResponse
}

func (c *reactiveRetryLLMClient) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	panic("unexpected summary Chat call")
}

func (c *reactiveRetryLLMClient) ChatStream(_ context.Context, req llm.ChatRequest, cb llm.StreamCallback) (*llm.ChatResponse, error) {
	c.requests = append(c.requests, req)
	call := len(c.requests) - 1
	if call < len(c.errs) && c.errs[call] != nil {
		return nil, c.errs[call]
	}
	if cb != nil {
		cb(llm.StreamEvent{Done: true})
	}
	return c.response, nil
}

type reactiveToolLoopLLMClient struct {
	requests []llm.ChatRequest
}

func (c *reactiveToolLoopLLMClient) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	panic("unexpected summary Chat call")
}

func (c *reactiveToolLoopLLMClient) ChatStream(_ context.Context, req llm.ChatRequest, cb llm.StreamCallback) (*llm.ChatResponse, error) {
	c.requests = append(c.requests, req)
	switch len(c.requests) {
	case 1:
		return nil, &llm.Error{Kind: llm.ErrorKindContextOverflow, Provider: "openai", Operation: "chat_stream", StatusCode: 400, Message: "prompt too long"}
	case 2:
		if cb != nil {
			cb(llm.StreamEvent{Done: true})
		}
		return &llm.ChatResponse{
			ID:         "resp-tool",
			StopReason: "tool_use",
			ContentBlocks: []llm.ContentBlock{
				{Type: "tool_use", ID: "call_1", Name: "get_current_time", Input: json.RawMessage(`{}`)},
			},
		}, nil
	default:
		if cb != nil {
			cb(llm.StreamEvent{Content: "final"})
			cb(llm.StreamEvent{Done: true})
		}
		return &llm.ChatResponse{
			ID:         "resp-final",
			Content:    "final",
			StopReason: "end_turn",
			ContentBlocks: []llm.ContentBlock{
				{Type: "text", Text: "final"},
			},
		}, nil
	}
}

type scriptedEngineClient struct {
	responses []*llm.ChatResponse
	requests  []llm.ChatRequest
	index     int
}

func (c *scriptedEngineClient) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	// Summary call
	c.requests = append(c.requests, req)
	return &llm.ChatResponse{
		ID:      "summary-1",
		Model:   req.Model,
		Content: engineSummaryContent("summary"),
	}, nil
}

func (c *scriptedEngineClient) ChatStream(_ context.Context, req llm.ChatRequest, cb llm.StreamCallback) (*llm.ChatResponse, error) {
	c.requests = append(c.requests, req)
	if c.index >= len(c.responses) {
		return nil, errors.New("scriptedEngineClient: no scripted response")
	}
	resp := c.responses[c.index]
	c.index++
	if cb != nil {
		if resp.Content != "" {
			cb(llm.StreamEvent{Content: resp.Content})
		}
		cb(llm.StreamEvent{Done: true})
	}
	return resp, nil
}

func toolUseResponse(callID, name, input string) *llm.ChatResponse {
	return &llm.ChatResponse{
		ID:         "resp-tool-" + callID,
		StopReason: "tool_use",
		ContentBlocks: []llm.ContentBlock{
			{
				Type:  "tool_use",
				ID:    callID,
				Name:  name,
				Input: json.RawMessage(input),
			},
		},
	}
}

func endTurnResponse(text string) *llm.ChatResponse {
	return &llm.ChatResponse{
		ID:         "resp-end",
		Content:    text,
		StopReason: "end_turn",
		ContentBlocks: []llm.ContentBlock{
			{Type: "text", Text: text},
		},
	}
}

func TestEngineStartSessionPersistsSession(t *testing.T) {
	engine, db, _ := newTestEngine(t, &fakeLLMClient{})

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if sessionID == "" {
		t.Fatal("StartSession returned empty session id")
	}

	session, err := db.GetSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session == nil {
		t.Fatal("session not persisted")
	}
	if session.Persona != "default" {
		t.Fatalf("session.Persona = %q, want default", session.Persona)
	}
}

func TestEngineStartSessionEnsuresMemorySegment(t *testing.T) {
	engine, _, _ := newTestEngine(t, &fakeLLMClient{})
	bridge := &fakeMemoryBridge{
		ensureResult: MemorySegmentRef{SegmentID: "segment-1", MemorySessionID: "memory-1"},
	}
	engine.memory = bridge

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if len(bridge.ensureCalls) != 1 {
		t.Fatalf("ensure calls = %#v, want one call", bridge.ensureCalls)
	}
	if bridge.ensureCalls[0].ChatSessionID != sessionID || bridge.ensureCalls[0].PersonaID != "default" {
		t.Fatalf("ensure call = %#v, want session/default", bridge.ensureCalls[0])
	}
}

func TestEngineSendMessageStreamsAndPersistsConversation(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		deltas: []string{"Hi", " there"},
		response: &llm.ChatResponse{
			ID:      "resp-1",
			Content: "Hi there",
			Model:   "test-model",
		},
	}
	engine, db, _ := newTestEngine(t, fakeLLM)

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := db.AddMessage(context.Background(), "earlier-user", sessionID, "user", "Earlier question"); err != nil {
		t.Fatalf("AddMessage(user): %v", err)
	}
	if err := db.AddMessage(context.Background(), "earlier-assistant", sessionID, "assistant", "Earlier answer"); err != nil {
		t.Fatalf("AddMessage(assistant): %v", err)
	}

	persona := &config.Persona{
		Name:         "default",
		SystemPrompt: "You are warm.",
	}

	var streamed []string
	reply, err := engine.SendMessage(context.Background(), sessionID, persona, "How are you?", func(delta string) {
		streamed = append(streamed, delta)
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if reply != "Hi there" {
		t.Fatalf("reply = %q, want %q", reply, "Hi there")
	}
	if len(streamed) != 2 || streamed[0] != "Hi" || streamed[1] != " there" {
		t.Fatalf("streamed = %#v, want [Hi \" there\"]", streamed)
	}

	if !strings.Contains(fakeLLM.lastRequest.System, "<persona>\nYou are warm.\n</persona>") {
		t.Fatalf("System = %q, want persona section", fakeLLM.lastRequest.System)
	}
	if !strings.Contains(fakeLLM.lastRequest.System, "Emotion Work Delegation Contract") {
		t.Fatalf("System = %q, want Emotion Work Delegation Contract section", fakeLLM.lastRequest.System)
	}
	if fakeLLM.lastRequest.Model != "test-model" {
		t.Fatalf("Model = %q, want test-model", fakeLLM.lastRequest.Model)
	}
	if len(fakeLLM.lastRequest.Messages) != 3 {
		t.Fatalf("len(Messages) = %d, want 3", len(fakeLLM.lastRequest.Messages))
	}
	if fakeLLM.lastRequest.Messages[0].Content != "Earlier question" {
		t.Fatalf("Messages[0] = %#v, want Earlier question first", fakeLLM.lastRequest.Messages[0])
	}
	if fakeLLM.lastRequest.Messages[2].Content != "How are you?" {
		t.Fatalf("Messages[2] = %#v, want current user message last", fakeLLM.lastRequest.Messages[2])
	}

	messages, err := db.GetRecentMessages(context.Background(), sessionID, 10)
	if err != nil {
		t.Fatalf("GetRecentMessages: %v", err)
	}
	if len(messages) != 4 {
		t.Fatalf("len(messages) = %d, want 4", len(messages))
	}
	if messages[2].Role != "user" || messages[2].Content != "How are you?" {
		t.Fatalf("messages[2] = %#v, want persisted user message", messages[2])
	}
	if messages[3].Role != "assistant" || messages[3].Content != "Hi there" {
		t.Fatalf("messages[3] = %#v, want persisted assistant message", messages[3])
	}
}

func TestEngineRecordsEmotionPromptRenderSnapshot(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		deltas: []string{"ok"},
		response: &llm.ChatResponse{
			ID:      "resp-1",
			Content: "ok",
			Model:   "test-model",
		},
	}
	engine, db, _ := newTestEngine(t, fakeLLM)
	engine.agentID = "agent-a"
	engine.personaKey = "default"

	ctx := context.Background()
	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if _, err := engine.SendMessage(ctx, sessionID, &config.Persona{Name: "default", SystemPrompt: "system"}, "hello", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	items, err := db.ListRenderSnapshots(ctx, promptcenter.SnapshotFilter{AgentID: "agent-a", Purpose: "emotion_chat", Limit: 5})
	if err != nil {
		t.Fatalf("ListRenderSnapshots: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("snapshots = %#v, want one emotion_chat snapshot", items)
	}
	snapshot, err := db.GetRenderSnapshot(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("GetRenderSnapshot: %v", err)
	}
	if snapshot == nil {
		t.Fatal("snapshot is nil")
	}
	if snapshot.AgentID != "agent-a" || snapshot.PersonaKey != "default" || snapshot.SessionID != sessionID || snapshot.Purpose != "emotion_chat" {
		t.Fatalf("snapshot scope = %#v", snapshot)
	}
	if snapshot.RenderedText != fakeLLM.lastRequest.System {
		t.Fatalf("snapshot.RenderedText != request system\nsnapshot=%s\nrequest=%s", snapshot.RenderedText, fakeLLM.lastRequest.System)
	}
	if snapshot.FinalHash != promptcenter.HashText(fakeLLM.lastRequest.System) {
		t.Fatalf("snapshot.FinalHash = %q, want hash of rendered system", snapshot.FinalHash)
	}
	if len(snapshot.Components) != 2 {
		t.Fatalf("snapshot.Components = %#v, want two prompt components", snapshot.Components)
	}
	if snapshot.Components[0].ComponentID != promptcenter.ComponentEmotionOperatingContract || snapshot.Components[1].ComponentID != promptcenter.ComponentEmotionInternalContextDataPolicy {
		t.Fatalf("snapshot.Components = %#v, want emotion prompt components", snapshot.Components)
	}
	if !strings.Contains(snapshot.ComponentsJSON, promptcenter.ComponentEmotionOperatingContract) {
		t.Fatalf("ComponentsJSON = %q, want serialized component IDs", snapshot.ComponentsJSON)
	}
}

func TestEngineSendMessageAppendsMemoryEpisodes(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		response: &llm.ChatResponse{
			ID:         "resp-1",
			Content:    "Hi there",
			StopReason: "end_turn",
		},
	}
	engine, _, _ := newTestEngine(t, fakeLLM)
	bridge := &fakeMemoryBridge{
		ensureResult: MemorySegmentRef{SegmentID: "segment-current", MemorySessionID: "memory-current"},
	}
	engine.memory = bridge

	ctx := context.Background()
	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	bridge.ensureCalls = nil

	reply, err := engine.SendMessage(ctx, sessionID, &config.Persona{Name: "default", SystemPrompt: "system"}, "How are you?", nil)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if reply != "Hi there" {
		t.Fatalf("reply = %q, want Hi there", reply)
	}
	if len(bridge.ensureCalls) != 1 || bridge.ensureCalls[0].ChatSessionID != sessionID {
		t.Fatalf("ensure calls = %#v, want current session", bridge.ensureCalls)
	}
	if len(bridge.userEpisodes) != 1 {
		t.Fatalf("user episodes = %#v, want one", bridge.userEpisodes)
	}
	if bridge.userEpisodes[0].SegmentID != "segment-current" || bridge.userEpisodes[0].Content != "How are you?" || bridge.userEpisodes[0].MessageID == "" {
		t.Fatalf("user episode = %#v, want current segment/content/message id", bridge.userEpisodes[0])
	}
	if len(bridge.assistEpisodes) != 1 {
		t.Fatalf("assistant episodes = %#v, want one", bridge.assistEpisodes)
	}
	if bridge.assistEpisodes[0].SegmentID != "segment-current" || bridge.assistEpisodes[0].Content != "Hi there" || bridge.assistEpisodes[0].MessageID == "" {
		t.Fatalf("assistant episode = %#v, want current segment/content/message id", bridge.assistEpisodes[0])
	}
}

func TestEnginePrepareInputAndMemoryAnchorReturnsCurrentUserEpisode(t *testing.T) {
	engine, _, _ := newTestEngine(t, &fakeLLMClient{})
	bridge := &fakeMemoryBridge{
		ensureResult: MemorySegmentRef{SegmentID: "segment-current", MemorySessionID: "memory-current"},
	}
	engine.memory = bridge

	ctx := context.Background()
	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	bridge.ensureCalls = nil

	anchor, err := engine.prepareInputAndMemoryAnchor(ctx, sessionID, turnOptions{
		persistUser: true,
		userContent: "咖啡",
	})
	if err != nil {
		t.Fatalf("prepareInputAndMemoryAnchor: %v", err)
	}
	if !anchor.hasMemorySegment || anchor.memorySegment.SegmentID != "segment-current" {
		t.Fatalf("anchor segment = %#v has=%v, want current segment", anchor.memorySegment, anchor.hasMemorySegment)
	}
	if anchor.userEpisodeID != "episode-user" {
		t.Fatalf("userEpisodeID = %q, want episode-user", anchor.userEpisodeID)
	}
	if len(bridge.userEpisodes) != 1 || bridge.userEpisodes[0].Content != "咖啡" {
		t.Fatalf("user episodes = %#v, want one current episode", bridge.userEpisodes)
	}
}

func TestEngineCommitTurnOutputPersistsAssistantAndEpisode(t *testing.T) {
	engine, db, _ := newTestEngine(t, &fakeLLMClient{})
	bridge := &fakeMemoryBridge{
		ensureResult: MemorySegmentRef{SegmentID: "segment-current", MemorySessionID: "memory-current"},
	}
	engine.memory = bridge

	ctx := context.Background()
	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	bridge.ensureCalls = nil

	if err := engine.commitTurnOutput(ctx, sessionID, "Hi there", nil, nil, MemorySegmentRef{}, false); err != nil {
		t.Fatalf("commitTurnOutput: %v", err)
	}
	messages, err := db.GetAllMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	if len(messages) != 1 || messages[0].Role != "assistant" || messages[0].Content != "Hi there" {
		t.Fatalf("messages = %#v, want final assistant message", messages)
	}
	if len(bridge.assistEpisodes) != 1 || bridge.assistEpisodes[0].Content != "Hi there" {
		t.Fatalf("assistant episodes = %#v, want final assistant episode", bridge.assistEpisodes)
	}
}

func TestEngineRetrieveMemoryPromptExcludesCurrentEpisode(t *testing.T) {
	engine, _, _ := newTestEngine(t, &fakeLLMClient{})
	bridge := &fakeMemoryBridge{
		retrieveBlock: "memory block",
	}
	engine.memory = bridge

	snapshot, err := engine.retrieveMemoryPrompt(context.Background(), "session-1", "咖啡", "episode-user", config.MemoryRetrievalConfig{
		Enabled:      true,
		InjectPrompt: true,
		FailOpen:     true,
	})
	if err != nil {
		t.Fatalf("retrieveMemoryPrompt: %v", err)
	}
	if snapshot == nil || snapshot.PromptBlock != "memory block" {
		t.Fatalf("snapshot = %#v, want memory block", snapshot)
	}
	if len(bridge.retrieveCalls) != 1 {
		t.Fatalf("retrieve calls = %#v, want one", bridge.retrieveCalls)
	}
	if got := bridge.retrieveCalls[0].ExcludedEpisodeIDs; len(got) != 1 || got[0] != "episode-user" {
		t.Fatalf("excluded episodes = %#v, want current user episode", got)
	}
}

func TestEngineApprovalContinuationSkipsMemoryRetrieve(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		response: &llm.ChatResponse{
			ID:         "resp-approval",
			Content:    "resumed",
			StopReason: "end_turn",
		},
	}
	engine, _, _ := newTestEngine(t, fakeLLM)
	bridge := &fakeMemoryBridge{
		ensureResult:  MemorySegmentRef{SegmentID: "segment-current", MemorySessionID: "memory-current"},
		retrieveBlock: "memory block",
	}
	engine.memory = bridge
	engine.memoryRetrieval = config.MemoryRetrievalConfig{
		Enabled:      true,
		InjectPrompt: true,
		FailOpen:     false,
	}
	ctx := context.Background()
	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	bridge.retrieveCalls = nil

	reply, err := engine.sendTurn(ctx, sessionID, &config.Persona{Name: "default", SystemPrompt: "system"}, nil, turnOptions{
		persistUser: false,
		extraSystem: "approval continuation",
	})
	if err != nil {
		t.Fatalf("sendTurn approval continuation: %v", err)
	}
	if reply != "resumed" {
		t.Fatalf("reply = %q, want resumed", reply)
	}
	if len(bridge.retrieveCalls) != 0 {
		t.Fatalf("retrieve calls = %#v, want none for approval continuation", bridge.retrieveCalls)
	}
}

func TestEngineSendMessageReturnsManualMemoryNoticeWithoutLLM(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		response: &llm.ChatResponse{ID: "resp-1", Content: "should not call"},
	}
	engine, db, _ := newTestEngine(t, fakeLLM)
	bridge := &fakeMemoryBridge{
		ensureResult:   MemorySegmentRef{SegmentID: "segment-1", MemorySessionID: "memory-1"},
		manualNotice:   "我准备执行一次长期记忆删除，尚未执行。\n\n候选：\n- 用户喜欢手冲咖啡。\n\n影响：确认后只会删除上面列出的 exact-node 目标。\n确认删除请回复“确认删除”；取消请回复“取消”。",
		manualNoticeOK: true,
	}
	engine.memory = bridge

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	reply, err := engine.SendMessage(context.Background(), sessionID, &config.Persona{Name: "default", SystemPrompt: "system"}, "忘记我喜欢手冲咖啡", nil)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if !strings.Contains(reply, "尚未执行") || !strings.Contains(reply, "用户喜欢手冲咖啡。") || !strings.Contains(reply, "确认删除") || !strings.Contains(reply, "取消") {
		t.Fatalf("reply = %q, want manual forget confirmation notice", reply)
	}
	if len(fakeLLM.chatRequests) != 0 || fakeLLM.lastRequest.Model != "" {
		t.Fatalf("LLM was called for manual memory notice: chat=%d stream_model=%q", len(fakeLLM.chatRequests), fakeLLM.lastRequest.Model)
	}
	if len(bridge.assistEpisodes) != 0 {
		t.Fatalf("assistant memory episodes = %d, want 0 for pending forget notice", len(bridge.assistEpisodes))
	}
	messages, err := db.GetAllMessages(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	if len(messages) != 2 || messages[1].Role != "assistant" || messages[1].Content != reply {
		t.Fatalf("messages = %#v, want persisted user and confirmation notice", messages)
	}
}

func TestEngineSendMessageSkipsMemoryRetrieveWhenPromptInjectionDisabled(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		response: endTurnResponse("ok"),
	}
	engine, _, _ := newTestEngine(t, fakeLLM)
	bridge := &fakeMemoryBridge{
		ensureResult:  MemorySegmentRef{SegmentID: "segment-current", MemorySessionID: "memory-current"},
		retrieveBlock: "[长期记忆上下文：使用约束]\n\n- 用户喜欢手冲咖啡。",
	}
	engine.memory = bridge
	engine.memoryRetrieval = config.MemoryRetrievalConfig{
		Enabled:             true,
		InjectPrompt:        false,
		UseFTS:              true,
		FinalMemoryCount:    4,
		ContextBudgetTokens: 700,
		FailOpen:            true,
	}

	ctx := context.Background()
	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	if _, err := engine.SendMessage(ctx, sessionID, &config.Persona{Name: "default", SystemPrompt: "system"}, "咖啡", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if len(bridge.retrieveCalls) != 0 {
		t.Fatalf("retrieve calls = %#v, want none", bridge.retrieveCalls)
	}
	if strings.Contains(fakeLLM.lastRequest.System, "[长期记忆上下文：使用约束]") {
		t.Fatalf("system = %q, want no long-term memory block", fakeLLM.lastRequest.System)
	}
}

func TestEngineSendMessageInjectsRetrievedMemoryPromptBlock(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		response: endTurnResponse("ok"),
	}
	engine, _, _ := newTestEngine(t, fakeLLM)
	bridge := &fakeMemoryBridge{
		ensureResult:  MemorySegmentRef{SegmentID: "segment-current", MemorySessionID: "memory-current"},
		retrieveBlock: "[长期记忆上下文：使用约束]\n\n- 用户喜欢手冲咖啡。",
	}
	engine.memory = bridge
	engine.memoryRetrieval = config.MemoryRetrievalConfig{
		Enabled:             true,
		InjectPrompt:        true,
		UseFTS:              true,
		FinalMemoryCount:    4,
		ContextBudgetTokens: 700,
		FailOpen:            true,
	}

	ctx := context.Background()
	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	bridge.ensureCalls = nil

	if _, err := engine.SendMessage(ctx, sessionID, &config.Persona{Name: "default", SystemPrompt: "system"}, "咖啡", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if len(bridge.retrieveCalls) != 1 {
		t.Fatalf("retrieve calls = %#v, want one", bridge.retrieveCalls)
	}
	call := bridge.retrieveCalls[0]
	if call.ChatSessionID != sessionID || call.Query != "咖啡" {
		t.Fatalf("retrieve call = %#v, want session/query", call)
	}
	if len(call.ExcludedEpisodeIDs) != 1 || call.ExcludedEpisodeIDs[0] != "episode-user" {
		t.Fatalf("excluded episode ids = %#v, want current user episode", call.ExcludedEpisodeIDs)
	}
	if !strings.Contains(fakeLLM.lastRequest.System, "[长期记忆上下文：使用约束]") || !strings.Contains(fakeLLM.lastRequest.System, "- 用户喜欢手冲咖啡。") {
		t.Fatalf("system = %q, want long-term memory block", fakeLLM.lastRequest.System)
	}
}

func TestEngineSendMessageStoresMemoryPipelineMetadataWhenDebugEnabled(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		response: endTurnResponse("ok"),
	}
	engine, db, _ := newTestEngine(t, fakeLLM)
	bridge := &fakeMemoryBridge{
		ensureResult:  MemorySegmentRef{SegmentID: "segment-current", MemorySessionID: "memory-current"},
		retrieveBlock: "[长期记忆上下文：使用约束]\n\n- 用户喜欢手冲咖啡。",
		retrieveTrace: map[string]any{
			"query_analysis": map[string]any{
				"normalized": "咖啡",
				"scores": map[string]any{
					"rule_fit": 0.8,
				},
			},
			"stages": map[string]any{
				"final_selection_mmr": []map[string]any{{
					"content_summary": "用户喜欢手冲咖啡。",
					"score":           0.91,
				}},
			},
		},
	}
	engine.memory = bridge
	engine.memoryRetrieval = config.MemoryRetrievalConfig{
		Enabled:             true,
		InjectPrompt:        true,
		UseFTS:              true,
		FinalMemoryCount:    4,
		ContextBudgetTokens: 700,
		FailOpen:            true,
		PipelineDebug:       true,
	}

	ctx := context.Background()
	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	bridge.ensureCalls = nil

	if _, err := engine.SendMessage(ctx, sessionID, &config.Persona{Name: "default", SystemPrompt: "system"}, "咖啡", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if len(bridge.retrieveCalls) != 1 || !bridge.retrieveCalls[0].IncludePipeline {
		t.Fatalf("retrieve calls = %#v, want one pipeline snapshot call", bridge.retrieveCalls)
	}
	if !strings.Contains(fakeLLM.lastRequest.System, "[长期记忆上下文：使用约束]") {
		t.Fatalf("system = %q, want memory prompt block", fakeLLM.lastRequest.System)
	}
	messages, err := db.GetAllMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(messages[1].Metadata), &metadata); err != nil {
		t.Fatalf("Unmarshal(metadata): %v; raw=%s", err, messages[1].Metadata)
	}
	pipeline, ok := metadata["memory_pipeline"].(map[string]any)
	if !ok {
		t.Fatalf("memory_pipeline = %#v, want object", metadata["memory_pipeline"])
	}
	if pipeline["prompt_block"] != "[长期记忆上下文：使用约束]\n\n- 用户喜欢手冲咖啡。" {
		t.Fatalf("prompt_block = %#v", pipeline["prompt_block"])
	}
	queryAnalysis, ok := pipeline["query_analysis"].(map[string]any)
	if !ok || queryAnalysis["normalized"] != "咖啡" {
		t.Fatalf("query_analysis = %#v, want normalized query", pipeline["query_analysis"])
	}
	stages, ok := pipeline["stages"].(map[string]any)
	if !ok || stages["final_selection_mmr"] == nil {
		t.Fatalf("stages = %#v, want final_selection_mmr", pipeline["stages"])
	}
}

func TestEngineSendMessageDoesNotStoreMemoryPipelineAfterFailOpenRetrieveError(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		response: endTurnResponse("ok"),
	}
	engine, db, _ := newTestEngine(t, fakeLLM)
	bridge := &fakeMemoryBridge{
		ensureResult: MemorySegmentRef{SegmentID: "segment-current", MemorySessionID: "memory-current"},
		retrieveErr:  errors.New("retrieve failed"),
	}
	engine.memory = bridge
	engine.memoryRetrieval = config.MemoryRetrievalConfig{
		Enabled:             true,
		InjectPrompt:        true,
		UseFTS:              true,
		FinalMemoryCount:    4,
		ContextBudgetTokens: 700,
		FailOpen:            true,
		PipelineDebug:       true,
	}

	ctx := context.Background()
	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if _, err := engine.SendMessage(ctx, sessionID, &config.Persona{Name: "default", SystemPrompt: "system"}, "咖啡", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	messages, err := db.GetAllMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(messages[1].Metadata), &metadata); err != nil {
		t.Fatalf("Unmarshal(metadata): %v; raw=%s", err, messages[1].Metadata)
	}
	if _, ok := metadata["memory_pipeline"]; ok {
		t.Fatalf("memory_pipeline = %#v, want absent after retrieve error", metadata["memory_pipeline"])
	}
}

func TestEngineMemoryRetrieveFailureFollowsFailOpenConfig(t *testing.T) {
	tests := []struct {
		name     string
		failOpen bool
		wantErr  bool
	}{
		{name: "fail open", failOpen: true, wantErr: false},
		{name: "fail closed", failOpen: false, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeLLM := &fakeLLMClient{
				response: endTurnResponse("ok"),
			}
			engine, _, _ := newTestEngine(t, fakeLLM)
			bridge := &fakeMemoryBridge{
				ensureResult: MemorySegmentRef{SegmentID: "segment-current", MemorySessionID: "memory-current"},
				retrieveErr:  errors.New("retrieve failed"),
			}
			engine.memory = bridge
			engine.memoryRetrieval = config.MemoryRetrievalConfig{
				Enabled:             true,
				InjectPrompt:        true,
				UseFTS:              true,
				FinalMemoryCount:    4,
				ContextBudgetTokens: 700,
				FailOpen:            tt.failOpen,
			}

			ctx := context.Background()
			sessionID, err := engine.StartSession(ctx, "default")
			if err != nil {
				t.Fatalf("StartSession: %v", err)
			}
			_, err = engine.SendMessage(ctx, sessionID, &config.Persona{Name: "default", SystemPrompt: "system"}, "咖啡", nil)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "retrieve memory prompt block") {
					t.Fatalf("SendMessage error = %v, want retrieve memory prompt block", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("SendMessage: %v", err)
			}
			if fakeLLM.lastRequest.System == "" {
				t.Fatal("LLM was not called after fail-open retrieve error")
			}
		})
	}
}

func TestEngineMemoryAppendFailureDoesNotBlockChat(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		response: endTurnResponse("ok"),
	}
	engine, db, _ := newTestEngine(t, fakeLLM)
	bridge := &fakeMemoryBridge{
		ensureResult:    MemorySegmentRef{SegmentID: "segment-current", MemorySessionID: "memory-current"},
		appendUserErr:   errors.New("memory user append failed"),
		appendAssistErr: errors.New("memory assistant append failed"),
	}
	engine.memory = bridge

	ctx := context.Background()
	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	reply, err := engine.SendMessage(ctx, sessionID, &config.Persona{Name: "default", SystemPrompt: "system"}, "hello", nil)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if reply != "ok" {
		t.Fatalf("reply = %q, want ok", reply)
	}

	messages, err := db.GetAllMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(messages))
	}
}

func TestEngineBuffersDeltasWhenRealtimeStreamingDisabled(t *testing.T) {
	var streamed []string
	client := &observingStreamClient{
		deltas: []string{"Hi"},
		afterDelta: func() {
			if len(streamed) != 0 {
				t.Fatalf("streamed during ChatStream = %#v, want no callback until response is complete", streamed)
			}
		},
	}
	engine, _, _ := newTestEngine(t, client)
	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	reply, err := engine.SendMessage(context.Background(), sessionID, &config.Persona{Name: "default", SystemPrompt: "You are warm."}, "hello", func(delta string) {
		streamed = append(streamed, delta)
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if reply != "Hi" {
		t.Fatalf("reply = %q, want Hi", reply)
	}
	if len(streamed) != 1 || streamed[0] != "Hi" {
		t.Fatalf("streamed = %#v, want [Hi]", streamed)
	}
}

func TestEngineRealtimeStreamingEmitsDeltaBeforeChatStreamReturns(t *testing.T) {
	var streamed []string
	client := &observingStreamClient{
		deltas: []string{"Hi"},
		afterDelta: func() {
			if len(streamed) != 1 || streamed[0] != "Hi" {
				t.Fatalf("streamed during ChatStream = %#v, want [Hi]", streamed)
			}
		},
	}
	engine, _, _ := newTestEngine(t, client)
	engine.UpdateRealtimeStreaming(true)
	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	reply, err := engine.SendMessage(context.Background(), sessionID, &config.Persona{Name: "default", SystemPrompt: "You are warm."}, "hello", func(delta string) {
		streamed = append(streamed, delta)
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if reply != "Hi" {
		t.Fatalf("reply = %q, want Hi", reply)
	}
	if len(streamed) != 1 || streamed[0] != "Hi" {
		t.Fatalf("streamed = %#v, want [Hi]", streamed)
	}
}

func TestEngineStreamsReasoningEventsAndPersistsMetadata(t *testing.T) {
	client := &reasoningStreamClient{
		events: []llm.StreamEvent{
			{Type: "reasoning", ReasoningContent: "think "},
			{Type: "reasoning", ReasoningContent: "first"},
			{Type: "text", Content: "Answer"},
		},
		response: &llm.ChatResponse{
			ID:               "resp-reasoning",
			Model:            "kimi-test",
			Content:          "Answer",
			StopReason:       "end_turn",
			ReasoningContent: "think first",
			ContentBlocks: []llm.ContentBlock{
				{Type: "text", Text: "Answer"},
			},
		},
	}
	engine, db, _ := newTestEngine(t, client)
	engine.providerName = "Moonshot"
	engine.UpdateRealtimeStreaming(true)
	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	var timeline []string
	ctx := withWSWriter(context.Background(), func(msg WSMessage) {
		timeline = append(timeline, msg.Type)
		if msg.Reasoning != nil && msg.Reasoning.Content != "" {
			timeline = append(timeline, "reasoning:"+msg.Reasoning.Content)
		}
		if msg.Reasoning != nil && msg.Reasoning.Provider != "" {
			timeline = append(timeline, "provider:"+msg.Reasoning.Provider)
		}
	})
	reply, err := engine.SendMessage(ctx, sessionID, &config.Persona{Name: "default", SystemPrompt: "system"}, "hello", func(delta string) {
		if delta != "" {
			timeline = append(timeline, "stream_delta:"+delta)
		}
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if reply != "Answer" {
		t.Fatalf("reply = %q, want Answer", reply)
	}

	wantTimeline := []string{
		"reasoning_start", "provider:Moonshot",
		"reasoning_delta", "reasoning:think ", "provider:Moonshot",
		"reasoning_delta", "reasoning:first", "provider:Moonshot",
		"reasoning_end", "reasoning:think first", "provider:Moonshot",
		"stream_delta:Answer",
	}
	if strings.Join(timeline, "|") != strings.Join(wantTimeline, "|") {
		t.Fatalf("timeline = %#v, want %#v", timeline, wantTimeline)
	}

	messages, err := db.GetAllMessages(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want user+assistant", len(messages))
	}
	if messages[1].Content != "Answer" {
		t.Fatalf("assistant content = %q, want Answer", messages[1].Content)
	}
	if strings.Contains(messages[1].Content, "think first") {
		t.Fatalf("assistant visible content leaked reasoning: %q", messages[1].Content)
	}
	blocks := decodeThinkingBlocks(t, messages[1].Metadata)
	if len(blocks) != 1 {
		t.Fatalf("thinking_blocks = %#v, want one block", blocks)
	}
	if blocks[0]["content"] != "think first" {
		t.Fatalf("thinking content = %#v, want think first", blocks[0]["content"])
	}
	if blocks[0]["kind"] != "reasoning_content" {
		t.Fatalf("thinking kind = %#v, want reasoning_content", blocks[0]["kind"])
	}
	if blocks[0]["provider"] != "Moonshot" {
		t.Fatalf("thinking provider = %#v, want provider display name Moonshot", blocks[0]["provider"])
	}
	if _, ok := blocks[0]["duration_ms"].(float64); !ok {
		t.Fatalf("duration_ms = %#v, want numeric", blocks[0]["duration_ms"])
	}
}

func TestEngineEmitsOneShotReasoningWhenOnlyFinalResponseHasReasoning(t *testing.T) {
	client := &reasoningStreamClient{
		events: []llm.StreamEvent{
			{Type: "text", Content: "Answer"},
		},
		response: &llm.ChatResponse{
			ID:               "resp-reasoning-final",
			Model:            "deepseek-test",
			Content:          "Answer",
			StopReason:       "end_turn",
			ReasoningContent: "final thinking",
			ContentBlocks: []llm.ContentBlock{
				{Type: "text", Text: "Answer"},
			},
		},
	}
	engine, _, _ := newTestEngine(t, client)
	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	var timeline []string
	ctx := withWSWriter(context.Background(), func(msg WSMessage) {
		timeline = append(timeline, msg.Type)
		if msg.Reasoning != nil && msg.Reasoning.Content != "" {
			timeline = append(timeline, "reasoning:"+msg.Reasoning.Content)
		}
	})
	reply, err := engine.SendMessage(ctx, sessionID, &config.Persona{Name: "default", SystemPrompt: "system"}, "hello", func(delta string) {
		if delta != "" {
			timeline = append(timeline, "stream_delta:"+delta)
		}
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if reply != "Answer" {
		t.Fatalf("reply = %q, want Answer", reply)
	}

	wantTimeline := []string{
		"reasoning_start",
		"reasoning_delta", "reasoning:final thinking",
		"reasoning_end", "reasoning:final thinking",
		"stream_delta:Answer",
	}
	if strings.Join(timeline, "|") != strings.Join(wantTimeline, "|") {
		t.Fatalf("timeline = %#v, want %#v", timeline, wantTimeline)
	}
}

func decodeThinkingBlocks(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var metadata map[string]any
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		t.Fatalf("Unmarshal(metadata): %v; raw=%s", err, raw)
	}
	values, ok := metadata["thinking_blocks"].([]any)
	if !ok {
		t.Fatalf("thinking_blocks = %#v, want array", metadata["thinking_blocks"])
	}
	blocks := make([]map[string]any, 0, len(values))
	for _, value := range values {
		block, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("thinking block = %#v, want object", value)
		}
		blocks = append(blocks, block)
	}
	return blocks
}

func TestEngineUpdateConfigAffectsSubsequentMessages(t *testing.T) {
	firstClient := &fakeLLMClient{
		response: &llm.ChatResponse{ID: "resp-1", Content: "first", Model: "model-a"},
	}
	secondClient := &fakeLLMClient{
		response: &llm.ChatResponse{ID: "resp-2", Content: "second", Model: "model-b"},
	}
	engine, _, _ := newTestEngine(t, firstClient)

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	persona := &config.Persona{Name: "default", SystemPrompt: "system"}
	if _, err := engine.SendMessage(context.Background(), sessionID, persona, "before update", nil); err != nil {
		t.Fatalf("SendMessage(before): %v", err)
	}

	engine.UpdateConfig(secondClient, "openai", "model-b", "summary-b", nil, 3072, 1024, 0.9, config.ContextConfig{
		InputBudgetTokens:    12000,
		SoftCompactRatio:     0.70,
		HardCompactRatio:     0.90,
		ReserveOutputTokens:  2048,
		KeepRecentUserTurns:  2,
		ToolResultSoftTokens: 50,
		ToolResultHardTokens: 100,
	})

	if _, err := engine.SendMessage(context.Background(), sessionID, persona, "after update", nil); err != nil {
		t.Fatalf("SendMessage(after): %v", err)
	}

	if secondClient.lastRequest.Model != "model-b" {
		t.Fatalf("lastRequest.Model = %q, want model-b", secondClient.lastRequest.Model)
	}
	if secondClient.lastRequest.MaxTokens != 1024 {
		t.Fatalf("lastRequest.MaxTokens = %d, want 1024", secondClient.lastRequest.MaxTokens)
	}
	if secondClient.lastRequest.Temperature != 0.9 {
		t.Fatalf("lastRequest.Temperature = %v, want 0.9", secondClient.lastRequest.Temperature)
	}
	if engine.summaryModel != "summary-b" {
		t.Fatalf("summaryModel = %q, want summary-b", engine.summaryModel)
	}
	if engine.summaryMaxTokens != 3072 {
		t.Fatalf("summaryMaxTokens = %d, want 3072", engine.summaryMaxTokens)
	}
	if engine.contextCfg.KeepRecentUserTurns != 2 {
		t.Fatalf("contextCfg.KeepRecentUserTurns = %d, want 2", engine.contextCfg.KeepRecentUserTurns)
	}
}

func TestEngineResumeSessionRequiresMatchingPersona(t *testing.T) {
	engine, db, _ := newTestEngine(t, &fakeLLMClient{})
	ctx := context.Background()

	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	resumedID, ok, err := engine.ResumeSession(ctx, sessionID, "default")
	if err != nil {
		t.Fatalf("ResumeSession(match): %v", err)
	}
	if !ok || resumedID != sessionID {
		t.Fatalf("ResumeSession(match) = (%q, %v), want (%q, true)", resumedID, ok, sessionID)
	}

	resumedID, ok, err = engine.ResumeSession(ctx, sessionID, "neko")
	if err != nil {
		t.Fatalf("ResumeSession(mismatch): %v", err)
	}
	if ok || resumedID != "" {
		t.Fatalf("ResumeSession(mismatch) = (%q, %v), want ('', false)", resumedID, ok)
	}

	if _, err := db.GetSession(ctx, sessionID); err != nil {
		t.Fatalf("GetSession: %v", err)
	}
}

func TestEngineResumeSessionRollsOverMemorySegment(t *testing.T) {
	engine, _, _ := newTestEngine(t, &fakeLLMClient{})
	bridge := &fakeMemoryBridge{
		rolloverResult: MemorySegmentRef{SegmentID: "segment-2", MemorySessionID: "memory-2"},
	}
	engine.memory = bridge

	ctx := context.Background()
	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	bridge.rolloverCalls = nil

	resumedID, ok, err := engine.ResumeSession(ctx, sessionID, "default")
	if err != nil {
		t.Fatalf("ResumeSession: %v", err)
	}
	if !ok || resumedID != sessionID {
		t.Fatalf("ResumeSession = (%q, %v), want (%q, true)", resumedID, ok, sessionID)
	}
	if len(bridge.rolloverCalls) != 1 {
		t.Fatalf("rollover calls = %#v, want one", bridge.rolloverCalls)
	}
	if bridge.rolloverCalls[0].ChatSessionID != sessionID || bridge.rolloverCalls[0].PersonaID != "default" || bridge.rolloverCalls[0].Reason != "session_resume" {
		t.Fatalf("rollover call = %#v, want session/default/session_resume", bridge.rolloverCalls[0])
	}
}

func TestEngineGetHistoryReturnsRecentMessages(t *testing.T) {
	engine, db, _ := newTestEngine(t, &fakeLLMClient{})
	ctx := context.Background()

	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	for i, msg := range []struct {
		id      string
		role    string
		content string
	}{
		{id: "msg-1", role: "user", content: "hello"},
		{id: "msg-2", role: "assistant", content: "hi"},
		{id: "msg-3", role: "user", content: "again"},
	} {
		if err := db.AddMessage(ctx, msg.id, sessionID, msg.role, msg.content); err != nil {
			t.Fatalf("AddMessage(%d): %v", i, err)
		}
	}

	history, err := engine.GetHistory(ctx, sessionID, 2)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("len(history) = %d, want 2", len(history))
	}
	if history[0].Content != "hi" || history[1].Content != "again" {
		t.Fatalf("history = %#v, want [hi again]", history)
	}
}

func TestEngineSendMessageUsesAssemblerInsteadOfRecentMessagesOnly(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		response: &llm.ChatResponse{ID: "resp-asm", Content: "ok", StopReason: "end_turn"},
	}
	engine, db, _ := newTestEngine(t, fakeLLM)
	ctx := context.Background()

	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	for _, msg := range []struct {
		id      string
		role    string
		content string
	}{
		{id: "m1", role: "user", content: "first user"},
		{id: "m2", role: "assistant", content: "first answer"},
		{id: "m3", role: "user", content: "second user"},
		{id: "m4", role: "assistant", content: "second answer"},
	} {
		if err := db.AddMessage(ctx, msg.id, sessionID, msg.role, msg.content); err != nil {
			t.Fatalf("AddMessage(%s): %v", msg.id, err)
		}
	}

	engine.contextCfg.KeepRecentUserTurns = 1
	persona := &config.Persona{Name: "default", SystemPrompt: "You are warm."}
	if _, err := engine.SendMessage(ctx, sessionID, persona, "latest user", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if len(fakeLLM.lastRequest.Messages) != 2 {
		t.Fatalf("len(Messages) = %d, want 2 (running summary + latest user)", len(fakeLLM.lastRequest.Messages))
	}
	if !strings.Contains(fakeLLM.lastRequest.Messages[0].Content, `"running_summary"`) {
		t.Fatalf("Messages[0] = %#v, want running summary envelope first", fakeLLM.lastRequest.Messages[0])
	}
	if fakeLLM.lastRequest.Messages[1].Content != "latest user" {
		t.Fatalf("Messages[1] = %#v, want latest user last", fakeLLM.lastRequest.Messages[1])
	}
}

func newTestEngine(t *testing.T, client llm.Client) (*Engine, *storage.DB, *slog.Logger) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "chat.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	db, err := storage.Open(path, logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	engine := NewEngine(EngineConfig{
		LLM:          client,
		DB:           db,
		Logger:       logger,
		Model:        "test-model",
		SummaryModel: "summary-model",
		MaxTokens:    256,
		Temperature:  0.2,
		ContextConfig: config.ContextConfig{
			InputBudgetTokens:    24000,
			SoftCompactRatio:     0.75,
			HardCompactRatio:     0.92,
			ReserveOutputTokens:  4096,
			KeepRecentUserTurns:  6,
			ToolResultSoftTokens: 1000,
			ToolResultHardTokens: 3000,
		},
	})

	return engine, db, logger
}

// --- Tool loop tests ---

// toolLoopLLMClient simulates an LLM that returns tool_use on the first call
// and then end_turn with the final reply on the second call.
type toolLoopLLMClient struct {
	callCount int
	requests  []llm.ChatRequest
}

func (c *toolLoopLLMClient) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	panic("unexpected Chat call")
}

func (c *toolLoopLLMClient) ChatStream(_ context.Context, req llm.ChatRequest, cb llm.StreamCallback) (*llm.ChatResponse, error) {
	c.callCount++
	c.requests = append(c.requests, req)

	if c.callCount == 1 {
		// First call: return tool_use.
		if cb != nil {
			cb(llm.StreamEvent{Content: "Let me check the current time."})
			cb(llm.StreamEvent{Done: true})
		}
		return &llm.ChatResponse{
			ID:         "resp-tool",
			Content:    "",
			StopReason: "tool_use",
			ContentBlocks: []llm.ContentBlock{
				{Type: "tool_use", ID: "call_1", Name: "get_current_time", Input: json.RawMessage(`{}`)},
			},
		}, nil
	}

	// Second call: return final text response.
	finalText := "It's 17:00 now!"
	if cb != nil {
		cb(llm.StreamEvent{Content: finalText})
		cb(llm.StreamEvent{Done: true})
	}
	return &llm.ChatResponse{
		ID:         "resp-final",
		Content:    finalText,
		StopReason: "end_turn",
		ContentBlocks: []llm.ContentBlock{
			{Type: "text", Text: finalText},
		},
	}, nil
}

type reasoningToolLoopLLMClient struct {
	requests []llm.ChatRequest
}

func (c *reasoningToolLoopLLMClient) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	panic("unexpected Chat call")
}

func (c *reasoningToolLoopLLMClient) ChatStream(_ context.Context, req llm.ChatRequest, cb llm.StreamCallback) (*llm.ChatResponse, error) {
	c.requests = append(c.requests, req)
	switch len(c.requests) {
	case 1:
		if cb != nil {
			cb(llm.StreamEvent{Type: "reasoning", ReasoningContent: "need a tool"})
			cb(llm.StreamEvent{Done: true})
		}
		return &llm.ChatResponse{
			ID:               "resp-tool-reasoning",
			StopReason:       "tool_use",
			ReasoningContent: "need a tool",
			ContentBlocks: []llm.ContentBlock{
				{Type: "tool_use", ID: "call_1", Name: "get_current_time", Input: json.RawMessage(`{}`)},
			},
		}, nil
	default:
		if cb != nil {
			cb(llm.StreamEvent{Type: "text", Content: "done"})
			cb(llm.StreamEvent{Done: true})
		}
		return &llm.ChatResponse{
			ID:         "resp-final",
			Content:    "done",
			StopReason: "end_turn",
			ContentBlocks: []llm.ContentBlock{
				{Type: "text", Text: "done"},
			},
		}, nil
	}
}

func TestEngineToolLoopExecutesToolAndReturnsResponse(t *testing.T) {
	mockLLM := &toolLoopLLMClient{}

	// Set up registry with a simple get_current_time tool.
	registry := tool.NewRegistry()
	registry.Register(tool.Spec{
		Name:        "get_current_time",
		Description: "Get current time",
		Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Scope:       tool.ScopeBoth,
		Permission:  tool.PermReadOnly,
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"current_time":"17:00:00","timezone":"CST"}`), nil
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "chat.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	db, err := storage.Open(path, logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	dispatcher := tool.NewDispatcher(registry, tool.MinimalSchemaValidator{}, logger)

	engine := NewEngine(EngineConfig{
		LLM:         mockLLM,
		DB:          db,
		Logger:      logger,
		Model:       "test-model",
		MaxTokens:   256,
		Temperature: 0.2,
		ContextConfig: config.ContextConfig{
			InputBudgetTokens:    24000,
			SoftCompactRatio:     0.75,
			HardCompactRatio:     0.92,
			ReserveOutputTokens:  4096,
			KeepRecentUserTurns:  6,
			ToolResultSoftTokens: 1000,
			ToolResultHardTokens: 3000,
		},
		Provider:   "openai",
		Registry:   registry,
		Dispatcher: dispatcher,
	})

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	persona := &config.Persona{Name: "default", SystemPrompt: "You are warm."}

	var streamed []string
	reply, err := engine.SendMessage(context.Background(), sessionID, persona, "What time is it?", func(delta string) {
		streamed = append(streamed, delta)
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	// Verify final reply.
	if reply != "It's 17:00 now!" {
		t.Fatalf("reply = %q, want %q", reply, "It's 17:00 now!")
	}

	// Verify LLM was called twice (tool_use → end_turn).
	if mockLLM.callCount != 2 {
		t.Fatalf("LLM call count = %d, want 2", mockLLM.callCount)
	}

	// Verify streaming delivered only the final text.
	if len(streamed) != 1 || streamed[0] != "It's 17:00 now!" {
		t.Fatalf("streamed = %v, want [\"It's 17:00 now!\"]", streamed)
	}

	// Verify DB has only user message + final assistant text (not intermediate tool messages).
	messages, err := db.GetRecentMessages(context.Background(), sessionID, 10)
	if err != nil {
		t.Fatalf("GetRecentMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2 (user + assistant)", len(messages))
	}
	if messages[0].Role != "user" || messages[0].Content != "What time is it?" {
		t.Fatalf("messages[0] = %+v, want user message", messages[0])
	}
	if messages[1].Role != "assistant" || messages[1].Content != "It's 17:00 now!" {
		t.Fatalf("messages[1] = %+v, want final assistant message", messages[1])
	}
}

func TestEngineForcedOutboundEmitsToolEventsWhenRealtimeStreamingDisabled(t *testing.T) {
	mockLLM := &toolLoopLLMClient{}
	registry := tool.NewRegistry()
	registry.Register(tool.Spec{
		Name:        "get_current_time",
		Description: "Get current time",
		Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Scope:       tool.ScopeBoth,
		Permission:  tool.PermReadOnly,
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"current_time":"17:00:00","timezone":"CST"}`), nil
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := storage.Open(filepath.Join(t.TempDir(), "chat.db"), logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	dispatcher := tool.NewDispatcher(registry, tool.MinimalSchemaValidator{}, logger)
	engine := NewEngine(EngineConfig{
		LLM:         mockLLM,
		DB:          db,
		Logger:      logger,
		Model:       "test-model",
		MaxTokens:   256,
		Temperature: 0.2,
		ContextConfig: config.ContextConfig{
			InputBudgetTokens:    24000,
			SoftCompactRatio:     0.75,
			HardCompactRatio:     0.92,
			ReserveOutputTokens:  4096,
			KeepRecentUserTurns:  6,
			ToolResultSoftTokens: 1000,
			ToolResultHardTokens: 3000,
		},
		Provider:   "openai",
		Registry:   registry,
		Dispatcher: dispatcher,
	})
	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	var events []turn.OutboundEvent
	ctx := turn.WithOutboundSink(context.Background(), turn.SinkFunc(func(_ context.Context, event turn.OutboundEvent) error {
		events = append(events, event)
		return nil
	}))
	ctx = withForcedOutboundEvents(ctx)
	if _, err := engine.SendMessage(ctx, sessionID, &config.Persona{Name: "default", SystemPrompt: "You are warm."}, "What time is it?", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	var types []string
	for _, event := range events {
		types = append(types, event.Type)
	}
	want := []string{turn.EventToolCallStart, turn.EventToolCallEnd}
	if len(types) != len(want) {
		t.Fatalf("events = %#v, want %#v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("events[%d] = %q, want %q (all=%#v)", i, types[i], want[i], types)
		}
	}
}

func TestEngineRealtimeToolLoopKeepsVisibleTextAndEmitsToolEvents(t *testing.T) {
	mockLLM := &toolLoopLLMClient{}
	registry := tool.NewRegistry()
	registry.Register(tool.Spec{
		Name:        "get_current_time",
		Description: "Get current time",
		Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Scope:       tool.ScopeBoth,
		Permission:  tool.PermReadOnly,
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"current_time":"17:00:00","timezone":"CST"}`), nil
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := storage.Open(filepath.Join(t.TempDir(), "chat.db"), logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	dispatcher := tool.NewDispatcher(registry, tool.MinimalSchemaValidator{}, logger)
	engine := NewEngine(EngineConfig{
		LLM:               mockLLM,
		DB:                db,
		Logger:            logger,
		Model:             "test-model",
		MaxTokens:         256,
		Temperature:       0.2,
		RealtimeStreaming: true,
		ContextConfig: config.ContextConfig{
			InputBudgetTokens:    24000,
			SoftCompactRatio:     0.75,
			HardCompactRatio:     0.92,
			ReserveOutputTokens:  4096,
			KeepRecentUserTurns:  6,
			ToolResultSoftTokens: 1000,
			ToolResultHardTokens: 3000,
		},
		Provider:   "openai",
		Registry:   registry,
		Dispatcher: dispatcher,
	})

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	var streamed []string
	var toolEvents []WSMessage
	ctx := withWSWriter(context.Background(), func(msg WSMessage) {
		if strings.HasPrefix(msg.Type, "tool_call_") {
			toolEvents = append(toolEvents, msg)
		}
	})
	reply, err := engine.SendMessage(ctx, sessionID, &config.Persona{Name: "default", SystemPrompt: "You are warm."}, "What time is it?", func(delta string) {
		streamed = append(streamed, delta)
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if reply != "Let me check the current time.It's 17:00 now!" {
		t.Fatalf("reply = %q, want visible pre-tool text plus final reply", reply)
	}
	if len(streamed) != 2 || streamed[0] != "Let me check the current time." || streamed[1] != "It's 17:00 now!" {
		t.Fatalf("streamed = %#v, want pre-tool and final deltas", streamed)
	}
	if len(toolEvents) != 2 {
		t.Fatalf("toolEvents = %#v, want start and end", toolEvents)
	}
	if toolEvents[0].Type != "tool_call_start" || toolEvents[0].Tool == nil || toolEvents[0].Tool.Status != "running" {
		t.Fatalf("tool start event = %#v, want running tool_call_start", toolEvents[0])
	}
	if toolEvents[1].Type != "tool_call_end" || toolEvents[1].Tool == nil || toolEvents[1].Tool.Status != "success" || !strings.Contains(toolEvents[1].Tool.Preview, "17:00") {
		t.Fatalf("tool end event = %#v, want successful preview", toolEvents[1])
	}

	messages, err := db.GetRecentMessages(context.Background(), sessionID, 10)
	if err != nil {
		t.Fatalf("GetRecentMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2 visible messages", len(messages))
	}
	if messages[1].Role != "assistant" || messages[1].Content != reply {
		t.Fatalf("assistant message = %#v, want persisted visible reply", messages[1])
	}
	if strings.Contains(messages[1].Content, "current_time") {
		t.Fatalf("assistant message contains tool result JSON: %q", messages[1].Content)
	}
}

func TestEngineToolLoopEndsReasoningBeforeToolCallAndKeepsReasoningForNextRequest(t *testing.T) {
	mockLLM := &reasoningToolLoopLLMClient{}
	registry := tool.NewRegistry()
	registry.Register(tool.Spec{
		Name:        "get_current_time",
		Description: "Get current time",
		Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Scope:       tool.ScopeBoth,
		Permission:  tool.PermReadOnly,
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"current_time":"17:00:00","timezone":"CST"}`), nil
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := storage.Open(filepath.Join(t.TempDir(), "chat.db"), logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	dispatcher := tool.NewDispatcher(registry, tool.MinimalSchemaValidator{}, logger)
	engine := NewEngine(EngineConfig{
		LLM:               mockLLM,
		DB:                db,
		Logger:            logger,
		Model:             "test-model",
		MaxTokens:         256,
		Temperature:       0.2,
		RealtimeStreaming: true,
		ContextConfig: config.ContextConfig{
			InputBudgetTokens:    24000,
			SoftCompactRatio:     0.75,
			HardCompactRatio:     0.92,
			ReserveOutputTokens:  4096,
			KeepRecentUserTurns:  6,
			ToolResultSoftTokens: 1000,
			ToolResultHardTokens: 3000,
		},
		Provider:   "openai",
		Registry:   registry,
		Dispatcher: dispatcher,
	})

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	var eventTypes []string
	ctx := withWSWriter(context.Background(), func(msg WSMessage) {
		switch msg.Type {
		case "reasoning_start", "reasoning_delta", "reasoning_end", "tool_call_start", "tool_call_end":
			eventTypes = append(eventTypes, msg.Type)
		}
	})
	if _, err := engine.SendMessage(ctx, sessionID, &config.Persona{Name: "default", SystemPrompt: "system"}, "time?", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	wantPrefix := []string{"reasoning_start", "reasoning_delta", "reasoning_end", "tool_call_start"}
	if len(eventTypes) < len(wantPrefix) {
		t.Fatalf("eventTypes = %#v, want prefix %#v", eventTypes, wantPrefix)
	}
	for i, want := range wantPrefix {
		if eventTypes[i] != want {
			t.Fatalf("eventTypes = %#v, want prefix %#v", eventTypes, wantPrefix)
		}
	}
	if len(mockLLM.requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(mockLLM.requests))
	}
	var assistantWithReasoning *llm.Message
	for i := range mockLLM.requests[1].Messages {
		msg := &mockLLM.requests[1].Messages[i]
		if msg.Role == llm.RoleAssistant && msg.ReasoningContent != "" {
			assistantWithReasoning = msg
			break
		}
	}
	if assistantWithReasoning == nil {
		t.Fatalf("second request messages = %#v, want assistant message with ReasoningContent", mockLLM.requests[1].Messages)
	}
	if assistantWithReasoning.ReasoningContent != "need a tool" {
		t.Fatalf("assistant ReasoningContent = %q, want need a tool", assistantWithReasoning.ReasoningContent)
	}
}

func TestEngineSendMessageDoesNotAdvertiseToolsWithoutDispatcher(t *testing.T) {
	mockLLM := &fakeLLMClient{
		response: &llm.ChatResponse{ID: "resp-plain", Content: "No tools", StopReason: "end_turn"},
	}
	registry := tool.NewRegistry()
	registry.Register(tool.Spec{
		Name:        "get_current_time",
		Description: "Get current time",
		Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Scope:       tool.ScopeBoth,
		Permission:  tool.PermReadOnly,
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"current_time":"17:00:00","timezone":"CST"}`), nil
	})

	engine, _, _ := newTestEngine(t, mockLLM)
	engine.registry = registry
	engine.dispatcher = nil

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	persona := &config.Persona{Name: "default", SystemPrompt: "You are warm."}

	if _, err := engine.SendMessage(context.Background(), sessionID, persona, "What time is it?", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if len(mockLLM.lastRequest.Tools) != 0 {
		t.Fatalf("Tools = %#v, want none when dispatcher is nil", mockLLM.lastRequest.Tools)
	}
}

func TestEngineToolLoopSnipsLargeToolResult(t *testing.T) {
	mockLLM := &toolLoopLLMClient{}

	registry := tool.NewRegistry()
	registry.Register(tool.Spec{
		Name:        "get_current_time",
		Description: "Get current time",
		Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Scope:       tool.ScopeBoth,
		Permission:  tool.PermReadOnly,
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"body":"` + strings.Repeat("x", 20000) + `"}`), nil
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "chat.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	db, err := storage.Open(path, logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	dispatcher := tool.NewDispatcher(registry, tool.MinimalSchemaValidator{}, logger)

	engine := NewEngine(EngineConfig{
		LLM:         mockLLM,
		DB:          db,
		Logger:      logger,
		Model:       "test-model",
		MaxTokens:   256,
		Temperature: 0.2,
		ContextConfig: config.ContextConfig{
			InputBudgetTokens:    24000,
			SoftCompactRatio:     0.75,
			HardCompactRatio:     0.92,
			ReserveOutputTokens:  4096,
			KeepRecentUserTurns:  6,
			ToolResultSoftTokens: 10,
			ToolResultHardTokens: 20,
		},
		Provider:   "openai",
		Registry:   registry,
		Dispatcher: dispatcher,
	})

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	persona := &config.Persona{Name: "default", SystemPrompt: "You are warm."}
	if _, err := engine.SendMessage(context.Background(), sessionID, persona, "What time is it?", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if len(mockLLM.requests) != 2 {
		t.Fatalf("len(requests) = %d, want 2", len(mockLLM.requests))
	}
	second := mockLLM.requests[1]
	last := second.Messages[len(second.Messages)-1]
	if !strings.Contains(last.Content, `"is_truncated":true`) {
		t.Fatalf("tool result content = %q, want truncated digest JSON", last.Content)
	}
	if strings.Contains(last.Content, strings.Repeat("x", 1000)) {
		t.Fatal("tool result content still contains raw payload")
	}
}

func TestSummaryModelFallsBackToPrimaryModelWhenEmpty(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		response: &llm.ChatResponse{ID: "resp-1", Content: "ok", StopReason: "end_turn"},
	}
	engine, db, _ := newTestEngine(t, fakeLLM)
	engine.contextCfg.KeepRecentUserTurns = 1
	engine.summaryModel = ""

	ctx := context.Background()
	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	for _, msg := range []struct {
		id      string
		role    string
		content string
	}{
		{id: "m1", role: "user", content: "old user"},
		{id: "m2", role: "assistant", content: "old assistant"},
	} {
		if err := db.AddMessage(ctx, msg.id, sessionID, msg.role, msg.content); err != nil {
			t.Fatalf("AddMessage(%s): %v", msg.id, err)
		}
	}

	persona := &config.Persona{Name: "default", SystemPrompt: "system"}
	if _, err := engine.SendMessage(ctx, sessionID, persona, "latest user", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if len(fakeLLM.chatRequests) == 0 {
		t.Fatal("summary Chat was not invoked")
	}
	if fakeLLM.chatRequests[0].Model != "test-model" {
		t.Fatalf("summary request model = %q, want fallback test-model", fakeLLM.chatRequests[0].Model)
	}
}

func TestSummaryFailureFallsBackWithoutBlockingChat(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		chatErr:  errors.New("summary unavailable"),
		response: &llm.ChatResponse{ID: "resp-1", Content: "ok", StopReason: "end_turn"},
	}
	engine, db, _ := newTestEngine(t, fakeLLM)
	engine.contextCfg.KeepRecentUserTurns = 1

	ctx := context.Background()
	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := db.AddMessage(ctx, "old-user", sessionID, "user", "old user"); err != nil {
		t.Fatalf("AddMessage(old-user): %v", err)
	}
	if err := db.AddMessage(ctx, "old-assistant", sessionID, "assistant", "old assistant"); err != nil {
		t.Fatalf("AddMessage(old-assistant): %v", err)
	}
	if _, err := db.SqlDB().ExecContext(ctx, `UPDATE sessions SET metadata = ? WHERE id = ?`, "{bad json", sessionID); err != nil {
		t.Fatalf("corrupt session metadata: %v", err)
	}

	persona := &config.Persona{Name: "default", SystemPrompt: "system"}
	reply, err := engine.SendMessage(ctx, sessionID, persona, "latest user", nil)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if reply != "ok" {
		t.Fatalf("reply = %q, want ok", reply)
	}
	if len(fakeLLM.chatRequests) == 0 {
		t.Fatal("summary Chat was not attempted")
	}
	if len(fakeLLM.lastRequest.Messages) != 1 || fakeLLM.lastRequest.Messages[0].Content != "latest user" {
		t.Fatalf("Messages = %#v, want chat to continue with recent turn only", fakeLLM.lastRequest.Messages)
	}
}

func TestSummaryFailureCooldownSkipsNextTurn(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		chatErr:  errors.New("summary unavailable"),
		response: endTurnResponse("ok"),
	}
	engine, db, _ := newTestEngine(t, fakeLLM)
	engine.contextCfg.KeepRecentUserTurns = 1

	ctx := context.Background()
	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := db.AddMessage(ctx, "old-user", sessionID, "user", "old user"); err != nil {
		t.Fatalf("AddMessage(old-user): %v", err)
	}
	if err := db.AddMessage(ctx, "old-assistant", sessionID, "assistant", "old assistant"); err != nil {
		t.Fatalf("AddMessage(old-assistant): %v", err)
	}

	persona := &config.Persona{Name: "default", SystemPrompt: "system"}
	if _, err := engine.SendMessage(ctx, sessionID, persona, "latest user", nil); err != nil {
		t.Fatalf("SendMessage(first): %v", err)
	}
	if len(fakeLLM.chatRequests) != 1 {
		t.Fatalf("summary calls after first turn = %d, want 1", len(fakeLLM.chatRequests))
	}
	if _, err := engine.SendMessage(ctx, sessionID, persona, "second latest user", nil); err != nil {
		t.Fatalf("SendMessage(second): %v", err)
	}
	if len(fakeLLM.chatRequests) != 1 {
		t.Fatalf("summary calls after second turn = %d, want still 1 during cooldown", len(fakeLLM.chatRequests))
	}
}

func TestSummaryUpdateUsesShortDeadline(t *testing.T) {
	client := &summaryDeadlineClient{}
	engine, db, _ := newTestEngine(t, client)
	engine.contextCfg.KeepRecentUserTurns = 1

	ctx := context.Background()
	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := db.AddMessage(ctx, "old-user", sessionID, "user", "old user"); err != nil {
		t.Fatalf("AddMessage(old-user): %v", err)
	}
	if err := db.AddMessage(ctx, "old-assistant", sessionID, "assistant", "old assistant"); err != nil {
		t.Fatalf("AddMessage(old-assistant): %v", err)
	}

	persona := &config.Persona{Name: "default", SystemPrompt: "system"}
	reply, err := engine.SendMessage(ctx, sessionID, persona, "latest user", nil)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if reply != "ok" {
		t.Fatalf("reply = %q, want ok", reply)
	}
	if !client.hadDeadline {
		t.Fatal("summary Chat context had no deadline")
	}
	if client.timeUntil > 8*time.Second || client.timeUntil < 7*time.Second {
		t.Fatalf("summary deadline = %v from now, want about 8s", client.timeUntil)
	}
}

func TestSummaryDoesNotPolluteVisibleHistory(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		chatResponse: &llm.ChatResponse{
			ID:      "summary-1",
			Model:   "summary-model",
			Content: engineSummaryContent("summarized old history"),
		},
		response: &llm.ChatResponse{ID: "resp-1", Content: "ok", StopReason: "end_turn"},
	}
	engine, db, _ := newTestEngine(t, fakeLLM)
	engine.contextCfg.KeepRecentUserTurns = 1
	engine.summaryModel = "summary-model"

	ctx := context.Background()
	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	for _, msg := range []struct {
		id      string
		role    string
		content string
	}{
		{id: "m1", role: "user", content: "old user"},
		{id: "m2", role: "assistant", content: "old assistant"},
	} {
		if err := db.AddMessage(ctx, msg.id, sessionID, msg.role, msg.content); err != nil {
			t.Fatalf("AddMessage(%s): %v", msg.id, err)
		}
	}

	persona := &config.Persona{Name: "default", SystemPrompt: "system"}
	if _, err := engine.SendMessage(ctx, sessionID, persona, "latest user", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	history, err := db.GetAllMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	if len(history) != 4 {
		t.Fatalf("len(history) = %d, want 4 visible user/assistant messages only", len(history))
	}
	for _, msg := range history {
		if msg.Role != "user" && msg.Role != "assistant" {
			t.Fatalf("unexpected visible role %q in history", msg.Role)
		}
		if strings.Contains(msg.Content, "running_summary") {
			t.Fatalf("visible history polluted by summary message: %#v", msg)
		}
	}
}

func TestReactiveCompactRetriesOnceOnOverflow(t *testing.T) {
	client := &reactiveRetryLLMClient{
		errs: []error{
			&llm.Error{Kind: llm.ErrorKindContextOverflow, Provider: "openai", Operation: "chat_stream", StatusCode: 400, Message: "prompt too long"},
		},
		response: &llm.ChatResponse{ID: "resp-1", Content: "ok", StopReason: "end_turn"},
	}
	engine, _, _ := newTestEngine(t, client)

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	persona := &config.Persona{Name: "default", SystemPrompt: "system"}
	reply, err := engine.SendMessage(context.Background(), sessionID, persona, "latest user", nil)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if reply != "ok" {
		t.Fatalf("reply = %q, want ok", reply)
	}
	if len(client.requests) != 2 {
		t.Fatalf("len(requests) = %d, want 2", len(client.requests))
	}
}

func TestReactiveCompactDoesNotRetryOnTransportError(t *testing.T) {
	client := &reactiveRetryLLMClient{
		errs: []error{
			&llm.Error{Kind: llm.ErrorKindTransport, Provider: "openai", Operation: "chat_stream", Message: "timeout"},
		},
		response: &llm.ChatResponse{ID: "resp-1", Content: "ok", StopReason: "end_turn"},
	}
	engine, _, _ := newTestEngine(t, client)

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	persona := &config.Persona{Name: "default", SystemPrompt: "system"}
	if _, err := engine.SendMessage(context.Background(), sessionID, persona, "latest user", nil); err == nil {
		t.Fatal("SendMessage should fail on transport error")
	}
	if len(client.requests) != 1 {
		t.Fatalf("len(requests) = %d, want 1", len(client.requests))
	}
}

func TestReactiveCompactStopsAfterSingleRetry(t *testing.T) {
	client := &reactiveRetryLLMClient{
		errs: []error{
			&llm.Error{Kind: llm.ErrorKindContextOverflow, Provider: "openai", Operation: "chat_stream", StatusCode: 400, Message: "prompt too long"},
			&llm.Error{Kind: llm.ErrorKindContextOverflow, Provider: "openai", Operation: "chat_stream", StatusCode: 400, Message: "prompt still too long"},
		},
	}
	engine, _, _ := newTestEngine(t, client)

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	persona := &config.Persona{Name: "default", SystemPrompt: "system"}
	if _, err := engine.SendMessage(context.Background(), sessionID, persona, "latest user", nil); err == nil {
		t.Fatal("SendMessage should fail after the single reactive retry is exhausted")
	}
	if len(client.requests) != 2 {
		t.Fatalf("len(requests) = %d, want 2", len(client.requests))
	}
}

func TestReactiveCompactToolLoopUsesCompactedWorkingSetAfterRetry(t *testing.T) {
	client := &reactiveToolLoopLLMClient{}
	registry := tool.NewRegistry()
	registry.Register(tool.Spec{
		Name:        "get_current_time",
		Description: "Get current time",
		Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Scope:       tool.ScopeBoth,
		Permission:  tool.PermReadOnly,
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"current_time":"17:00:00","timezone":"CST"}`), nil
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "chat.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	db, err := storage.Open(path, logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	dispatcher := tool.NewDispatcher(registry, tool.MinimalSchemaValidator{}, logger)
	engine := NewEngine(EngineConfig{
		LLM:          client,
		DB:           db,
		Logger:       logger,
		Model:        "test-model",
		SummaryModel: "summary-model",
		MaxTokens:    256,
		Temperature:  0.2,
		ContextConfig: config.ContextConfig{
			InputBudgetTokens:    24000,
			SoftCompactRatio:     0.75,
			HardCompactRatio:     0.92,
			ReserveOutputTokens:  4096,
			KeepRecentUserTurns:  2,
			ToolResultSoftTokens: 1000,
			ToolResultHardTokens: 3000,
		},
		Provider:   "openai",
		Registry:   registry,
		Dispatcher: dispatcher,
	})

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := db.AddMessage(context.Background(), "old-user", sessionID, "user", "old user"); err != nil {
		t.Fatalf("AddMessage(old-user): %v", err)
	}
	if err := db.AddMessage(context.Background(), "old-assistant", sessionID, "assistant", "old assistant"); err != nil {
		t.Fatalf("AddMessage(old-assistant): %v", err)
	}

	persona := &config.Persona{Name: "default", SystemPrompt: "system"}
	reply, err := engine.SendMessage(context.Background(), sessionID, persona, "latest user", nil)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if reply != "final" {
		t.Fatalf("reply = %q, want final", reply)
	}
	if len(client.requests) != 3 {
		t.Fatalf("len(requests) = %d, want 3", len(client.requests))
	}
	if len(client.requests[1].Messages) != 1 {
		t.Fatalf("retry messages len = %d, want 1 compacted latest user only", len(client.requests[1].Messages))
	}
	if len(client.requests[2].Messages) != 3 {
		t.Fatalf("post-tool-loop messages len = %d, want 3 compacted working-set messages", len(client.requests[2].Messages))
	}
	if client.requests[2].Messages[0].Content != "latest user" {
		t.Fatalf("post-tool-loop first message = %#v, want latest user", client.requests[2].Messages[0])
	}
}

func TestReactiveCompactRetryFailureLogsCompactContext(t *testing.T) {
	client := &reactiveRetryLLMClient{
		errs: []error{
			&llm.Error{Kind: llm.ErrorKindContextOverflow, Provider: "openai", Operation: "chat_stream", StatusCode: 400, Message: "prompt too long"},
			&llm.Error{Kind: llm.ErrorKindContextOverflow, Provider: "openai", Operation: "chat_stream", StatusCode: 400, Message: "prompt still too long"},
		},
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	db, err := storage.Open(filepath.Join(t.TempDir(), "chat.db"), logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	engine := NewEngine(EngineConfig{
		LLM:          client,
		DB:           db,
		Logger:       logger,
		Model:        "test-model",
		SummaryModel: "summary-model",
		MaxTokens:    256,
		Temperature:  0.2,
		ContextConfig: config.ContextConfig{
			InputBudgetTokens:    24000,
			SoftCompactRatio:     0.75,
			HardCompactRatio:     0.92,
			ReserveOutputTokens:  4096,
			KeepRecentUserTurns:  1,
			ToolResultSoftTokens: 1000,
			ToolResultHardTokens: 3000,
		},
	})

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	persona := &config.Persona{Name: "default", SystemPrompt: "system"}
	if _, err := engine.SendMessage(context.Background(), sessionID, persona, "latest user", nil); err == nil {
		t.Fatal("SendMessage should fail after overflow retry also fails")
	}

	logOutput := logs.String()
	if !strings.Contains(logOutput, "reactive compact retry failed") {
		t.Fatalf("logs = %q, want retry failure log message", logOutput)
	}
	if !strings.Contains(logOutput, "retry_attempt=1") {
		t.Fatalf("logs = %q, want retry_attempt field", logOutput)
	}
	if !strings.Contains(logOutput, "error_kind=context_overflow") {
		t.Fatalf("logs = %q, want error_kind=context_overflow", logOutput)
	}
	if !strings.Contains(logOutput, "compact_reason=reactive_overflow") {
		t.Fatalf("logs = %q, want compact_reason=reactive_overflow", logOutput)
	}
}

func TestEnginePendingDecisionChainAcrossTurns(t *testing.T) {
	llmClient := &scriptedEngineClient{
		responses: []*llm.ChatResponse{
			// Turn 1: delegate_to_work -> needs_emotion_decision, then ask user
			toolUseResponse("call_delegate", "delegate_to_work", `{"goal":"delete finish files","permission_scope":"workspace-write"}`),
			endTurnResponse("我需要你确认是否继续执行删除操作。"),
			// Turn 2: resume_work -> task report, then final assistant text
			toolUseResponse("call_resume", "resume_work", `{"task_id":"task-decision-1","decision":"confirm_delete","reason":"用户已确认"}`),
			endTurnResponse("已完成处理，目标文件已删除。"),
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "chat.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := storage.Open(path, logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	registry := tool.NewRegistry()
	dispatcher := tool.NewDispatcher(registry, tool.MinimalSchemaValidator{}, logger)
	approvals := work.NewApprovalService(db.SqlDB(), logger)
	pending := work.NewPendingRegistry(db.SqlDB(), approvals, logger, work.PendingRegistryConfig{
		SoftTTL:        time.Hour,
		HardTTL:        2 * time.Hour,
		ArchiveTTL:     24 * time.Hour,
		ResumeClaimTTL: 10 * time.Minute,
	})

	var delegateSessionID string
	var resumeSessionID string
	const pausedTaskID = "task-decision-1"

	registry.Register(tool.Spec{
		Name:        "delegate_to_work",
		Description: "test delegate",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"goal":{"type":"string"},"permission_scope":{"type":"string"}},"required":["goal","permission_scope"],"additionalProperties":false}`),
		Scope:       tool.ScopeEmotion,
		Permission:  tool.PermReadOnly,
	}, func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		delegateSessionID = work.SessionIDFromContext(ctx)
		packet := protocol.DecisionPacket{
			TaskID:               pausedTaskID,
			Category:             protocol.CatHumanConfirmation,
			RiskLevel:            "medium",
			GoalSummary:          "删除 docs/todo 下 [finish] 文件",
			Question:             "是否确认执行删除？",
			WhyBlocked:           "这是高风险不可逆操作",
			Options:              []protocol.DecisionOption{{ID: "confirm_delete", Summary: "确认删除"}, {ID: "cancel", Summary: "取消"}},
			RelevantFindings:     []protocol.DecisionEvidence{{Finding: "已定位到 4 个待删除文件", Source: "list_dir"}},
			KeyTradeoffs:         []protocol.DecisionTradeoff{{Dimension: "风险", Note: "删除后不可恢复"}},
			RecommendedOption:    "confirm_delete",
			RecommendationReason: "用户请求清理已完成文件",
			RejectOptionID:       "cancel",
			SuggestsUserInput:    true,
			CreatedAt:            time.Now().UTC(),
		}
		if err := pending.Put(delegateSessionID, pausedTaskID, &work.PausedWork{
			TaskID:    pausedTaskID,
			Packet:    packet,
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			return nil, err
		}
		return json.Marshal(work.NeedsEmotionDecision{
			Status:         "needs_emotion_decision",
			TaskID:         pausedTaskID,
			DecisionPacket: packet,
		})
	})

	registry.Register(tool.Spec{
		Name:        "resume_work",
		Description: "test resume",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string"},"decision":{"type":"string"},"reason":{"type":"string"}},"required":["task_id","decision"],"additionalProperties":false}`),
		Scope:       tool.ScopeEmotion,
		Permission:  tool.PermReadOnly,
	}, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		resumeSessionID = work.SessionIDFromContext(ctx)
		var req struct {
			TaskID   string `json:"task_id"`
			Decision string `json:"decision"`
		}
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, err
		}
		if req.TaskID != pausedTaskID {
			t.Fatalf("resume_work task_id = %q, want %q", req.TaskID, pausedTaskID)
		}
		if req.Decision != "confirm_delete" {
			t.Fatalf("resume_work decision = %q, want confirm_delete", req.Decision)
		}
		claim := pending.ClaimForResume(resumeSessionID, req.TaskID)
		report := protocol.TaskReport{
			TaskID:    req.TaskID,
			Status:    "completed",
			Goal:      "删除 docs/todo 下 [finish] 文件",
			Summary:   "删除完成",
			CreatedAt: time.Now().UTC(),
		}
		if err := pending.FinalizeResolved(resumeSessionID, req.TaskID, claim.ClaimID, protocol.DecisionResponse{
			TaskID:   req.TaskID,
			Decision: req.Decision,
		}, &report); err != nil {
			return nil, err
		}
		return json.Marshal(report)
	})

	engine := NewEngine(EngineConfig{
		LLM:          llmClient,
		DB:           db,
		Logger:       logger,
		Model:        "test-model",
		SummaryModel: "summary-model",
		MaxTokens:    512,
		Temperature:  0.2,
		ContextConfig: config.ContextConfig{
			InputBudgetTokens:    24000,
			SoftCompactRatio:     0.75,
			HardCompactRatio:     0.92,
			ReserveOutputTokens:  4096,
			KeepRecentUserTurns:  6,
			ToolResultSoftTokens: 1000,
			ToolResultHardTokens: 3000,
		},
		Provider:   "openai",
		Registry:   registry,
		Dispatcher: dispatcher,
		Pending:    pending,
	})

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	persona := &config.Persona{Name: "default", SystemPrompt: "You are warm."}

	reply1, err := engine.SendMessage(context.Background(), sessionID, persona, "帮我清理 [finish] 文件", nil)
	if err != nil {
		t.Fatalf("SendMessage turn1: %v", err)
	}
	if !strings.Contains(reply1, "确认") {
		t.Fatalf("turn1 reply = %q, want confirmation question", reply1)
	}

	reply2, err := engine.SendMessage(context.Background(), sessionID, persona, "确认删除", nil)
	if err != nil {
		t.Fatalf("SendMessage turn2: %v", err)
	}
	if !strings.Contains(reply2, "已完成") {
		t.Fatalf("turn2 reply = %q, want completion text", reply2)
	}

	if delegateSessionID != sessionID {
		t.Fatalf("delegate session id = %q, want %q", delegateSessionID, sessionID)
	}
	if resumeSessionID != sessionID {
		t.Fatalf("resume session id = %q, want %q", resumeSessionID, sessionID)
	}

	// The first ChatStream request of turn2 should include Resume Note.
	chatStreamReqCount := 0
	var turn2First llm.ChatRequest
	for _, req := range llmClient.requests {
		if req.Stream {
			chatStreamReqCount++
			if chatStreamReqCount == 3 {
				turn2First = req
				break
			}
		}
	}
	if turn2First.System == "" {
		t.Fatal("failed to capture turn2 first ChatStream request")
	}
	if !strings.Contains(turn2First.System, "Pending Decision(s) Resume Note") {
		t.Fatalf("turn2 system missing Resume Note: %s", turn2First.System)
	}
	if !strings.Contains(turn2First.System, pausedTaskID) {
		t.Fatalf("turn2 system missing pending task id %q: %s", pausedTaskID, turn2First.System)
	}

	history, err := db.GetAllMessages(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	if len(history) != 4 {
		t.Fatalf("visible history len = %d, want 4 (user/assistant/user/assistant)", len(history))
	}
	for _, msg := range history {
		if msg.Role != "user" && msg.Role != "assistant" {
			t.Fatalf("unexpected persisted role %q", msg.Role)
		}
		if strings.Contains(msg.Content, "needs_emotion_decision") || strings.Contains(msg.Content, "decision_packet") || strings.Contains(msg.Content, `"task_report"`) {
			t.Fatalf("persisted message leaks internal work traces: %#v", msg)
		}
	}

	if got := pending.ListInjectable(sessionID); len(got) != 0 {
		t.Fatalf("pending decisions should be consumed after resume, got %d", len(got))
	}
}

func TestEngineSendMessage_StopsTurnImmediatelyWhenToolApprovalIsRaised(t *testing.T) {
	llmClient := &scriptedEngineClient{
		responses: []*llm.ChatResponse{
			toolUseResponse("call_delegate", "delegate_to_work", `{"goal":"delete finish files","permission_scope":"workspace-write"}`),
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "chat.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := storage.Open(path, logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	registry := tool.NewRegistry()
	dispatcher := tool.NewDispatcher(registry, tool.MinimalSchemaValidator{}, logger)
	approvals := work.NewApprovalService(db.SqlDB(), logger)
	pending := work.NewPendingRegistry(db.SqlDB(), approvals, logger, work.PendingRegistryConfig{
		SoftTTL:        time.Hour,
		HardTTL:        2 * time.Hour,
		ArchiveTTL:     24 * time.Hour,
		ResumeClaimTTL: 10 * time.Minute,
	})

	const pausedTaskID = "task-approval-1"
	registry.Register(tool.Spec{
		Name:        "delegate_to_work",
		Description: "test delegate",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"goal":{"type":"string"},"permission_scope":{"type":"string"}},"required":["goal","permission_scope"],"additionalProperties":false}`),
		Scope:       tool.ScopeEmotion,
		Permission:  tool.PermReadOnly,
	}, func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		packet := protocol.DecisionPacket{
			TaskID:               pausedTaskID,
			Category:             protocol.CatToolApproval,
			RiskLevel:            "medium",
			GoalSummary:          "删除 docs/todo 下 [finish] 文件",
			Question:             "是否确认执行删除？",
			WhyBlocked:           "这是高风险不可逆操作",
			Options:              []protocol.DecisionOption{{ID: "confirm_delete", Summary: "确认删除"}, {ID: "cancel", Summary: "取消"}},
			RecommendedOption:    "confirm_delete",
			RecommendationReason: "用户请求清理已完成文件",
			RejectOptionID:       "cancel",
			SuggestsUserInput:    false,
			CreatedAt:            time.Now().UTC(),
		}
		if err := pending.Put(work.SessionIDFromContext(ctx), pausedTaskID, &work.PausedWork{
			TaskID:    pausedTaskID,
			Packet:    packet,
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			return nil, err
		}
		return json.Marshal(work.NeedsEmotionDecision{
			Status:         "needs_emotion_decision",
			TaskID:         pausedTaskID,
			DecisionPacket: packet,
		})
	})

	engine := NewEngine(EngineConfig{
		LLM:          llmClient,
		DB:           db,
		Logger:       logger,
		Model:        "test-model",
		SummaryModel: "summary-model",
		MaxTokens:    512,
		Temperature:  0.2,
		ContextConfig: config.ContextConfig{
			InputBudgetTokens:    24000,
			SoftCompactRatio:     0.75,
			HardCompactRatio:     0.92,
			ReserveOutputTokens:  4096,
			KeepRecentUserTurns:  6,
			ToolResultSoftTokens: 1000,
			ToolResultHardTokens: 3000,
		},
		Provider:   "openai",
		Registry:   registry,
		Dispatcher: dispatcher,
		Pending:    pending,
		Approvals:  approvals,
	})

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	persona := &config.Persona{Name: "default", SystemPrompt: "You are warm."}

	var timeline []string
	ctx := withWSWriter(context.Background(), func(msg WSMessage) {
		timeline = append(timeline, msg.Type)
	})
	reply, err := engine.SendMessage(ctx, sessionID, persona, "帮我清理 [finish] 文件", func(delta string) {
		if delta != "" {
			timeline = append(timeline, "delta:"+delta)
		}
	})
	if !errors.Is(err, errApprovalPending) {
		t.Fatalf("SendMessage err = %v, want errApprovalPending", err)
	}
	if reply != "" {
		t.Fatalf("reply = %q, want empty reply when approval interrupts the turn", reply)
	}

	var approvalIndex int = -1
	for i, item := range timeline {
		if item == "approval_required" && approvalIndex == -1 {
			approvalIndex = i
		}
		if strings.HasPrefix(item, "delta:") {
			t.Fatalf("timeline = %#v, want no assistant deltas after approval_required", timeline)
		}
	}
	if approvalIndex == -1 {
		t.Fatalf("timeline = %#v, want approval_required event", timeline)
	}
	if got := len(llmClient.requests); got != 1 {
		t.Fatalf("ChatStream requests = %d, want 1 when approval interrupts the turn", got)
	}
}

func TestEngineContinueAfterApproval_ResumesWorkDirectlyBeforeNarrating(t *testing.T) {
	llmClient := &scriptedEngineClient{
		responses: []*llm.ChatResponse{
			endTurnResponse("已经处理好了。"),
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "chat.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := storage.Open(path, logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	registry := tool.NewRegistry()
	dispatcher := tool.NewDispatcher(registry, tool.MinimalSchemaValidator{}, logger)

	var resumeCalls []struct {
		TaskID            string `json:"task_id"`
		ApprovalRequestID string `json:"approval_request_id"`
	}
	registry.Register(tool.Spec{
		Name:        "resume_work",
		Description: "test resume",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string"},"approval_request_id":{"type":"string"}},"required":["task_id","approval_request_id"],"additionalProperties":false}`),
		Scope:       tool.ScopeEmotion,
		Permission:  tool.PermReadOnly,
	}, func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var req struct {
			TaskID            string `json:"task_id"`
			ApprovalRequestID string `json:"approval_request_id"`
		}
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, err
		}
		resumeCalls = append(resumeCalls, req)
		return json.Marshal(protocol.TaskReport{
			TaskID:    req.TaskID,
			Status:    "completed",
			Goal:      "删除 finish 文件",
			Summary:   "已完成删除。",
			Findings:  []string{"删除了 1 个文件"},
			CreatedAt: time.Now().UTC(),
		})
	})

	engine := NewEngine(EngineConfig{
		LLM:          llmClient,
		DB:           db,
		Logger:       logger,
		Model:        "test-model",
		SummaryModel: "summary-model",
		MaxTokens:    512,
		Temperature:  0.2,
		ContextConfig: config.ContextConfig{
			InputBudgetTokens:    24000,
			SoftCompactRatio:     0.75,
			HardCompactRatio:     0.92,
			ReserveOutputTokens:  4096,
			KeepRecentUserTurns:  6,
			ToolResultSoftTokens: 1000,
			ToolResultHardTokens: 3000,
		},
		Provider:   "openai",
		Registry:   registry,
		Dispatcher: dispatcher,
	})

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	persona := &config.Persona{Name: "default", SystemPrompt: "You are warm."}

	var streamed []string
	reply, err := engine.ContinueAfterApproval(context.Background(), sessionID, persona, &protocol.ApprovalRequest{
		ID:               "approval-1",
		SessionID:        sessionID,
		TaskID:           "task-1",
		Category:         string(protocol.CatToolApproval),
		Status:           string(protocol.ApprovalStatusApproved),
		SelectedOptionID: "confirm_delete",
	}, func(delta string) {
		if delta != "" {
			streamed = append(streamed, delta)
		}
	})
	if err != nil {
		t.Fatalf("ContinueAfterApproval: %v", err)
	}
	if reply != "已经处理好了。" {
		t.Fatalf("reply = %q, want %q", reply, "已经处理好了。")
	}
	if len(resumeCalls) != 1 {
		t.Fatalf("resume_work calls = %d, want 1", len(resumeCalls))
	}
	if resumeCalls[0].TaskID != "task-1" || resumeCalls[0].ApprovalRequestID != "approval-1" {
		t.Fatalf("resume_work call = %#v, want task_id=task-1 approval_request_id=approval-1", resumeCalls[0])
	}
	if len(streamed) != 1 || streamed[0] != "已经处理好了。" {
		t.Fatalf("streamed = %#v, want [已经处理好了。]", streamed)
	}
	if len(llmClient.requests) != 1 {
		t.Fatalf("ChatStream requests = %d, want 1 narration request", len(llmClient.requests))
	}
	if !strings.Contains(llmClient.requests[0].System, "already been resumed internally") {
		t.Fatalf("system = %q, want direct-resume note", llmClient.requests[0].System)
	}
	if !strings.Contains(llmClient.requests[0].System, "已完成删除。") {
		t.Fatalf("system = %q, want task report summary in narration prompt", llmClient.requests[0].System)
	}
	if got := len(llmClient.requests[0].Tools); got != 0 {
		t.Fatalf("narration tools = %d, want 0 after final Work report", got)
	}
}

func TestApprovalNotesSeparateContinuationFromAlreadyResumedOutcome(t *testing.T) {
	approval := &protocol.ApprovalRequest{
		ID:               "approval-1",
		TaskID:           "task-1",
		Status:           string(protocol.ApprovalStatusApproved),
		SelectedOptionID: "allow",
	}

	continuation := buildApprovalContinuationNote(approval)
	for _, snippet := range []string{
		"Internal Approval Continuation",
		"has not been consumed",
		"calling resume_work",
		"If the approval is rejected, resume with rejection",
	} {
		if !strings.Contains(continuation, snippet) {
			t.Fatalf("continuation note missing %q: %s", snippet, continuation)
		}
	}
	if strings.Contains(continuation, "already been resumed internally") {
		t.Fatalf("continuation note should not describe direct internal resume: %s", continuation)
	}

	outcome := buildApprovalOutcomeNote(approval, json.RawMessage(`{"status":"completed","summary":"done"}`))
	for _, snippet := range []string{
		"Internal Approval Outcome",
		"already been resumed internally",
		"Do not call resume_work again",
		"do not reuse the consumed approval_request_id",
	} {
		if !strings.Contains(outcome, snippet) {
			t.Fatalf("outcome note missing %q: %s", snippet, outcome)
		}
	}
}

func TestEngineRoutesProgressEventsToWSWriter(t *testing.T) {
	llmClient := &scriptedEngineClient{
		responses: []*llm.ChatResponse{
			toolUseResponse("call_probe", "progress_probe", `{}`),
			endTurnResponse("done"),
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "chat.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := storage.Open(path, logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	registry := tool.NewRegistry()
	dispatcher := tool.NewDispatcher(registry, tool.MinimalSchemaValidator{}, logger)

	registry.Register(tool.Spec{
		Name:        "progress_probe",
		Description: "test progress callback",
		Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Scope:       tool.ScopeEmotion,
		Permission:  tool.PermReadOnly,
	}, func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		cb := progress.CallbackFromContext(ctx)
		if cb != nil {
			cb(progress.Event{Kind: progress.KindTool, ToolName: "read_file", Round: 0, TaskID: "task-1"})
			cb(progress.Event{Kind: progress.KindEnd, Round: 0, TaskID: "task-1"})
		}
		return json.Marshal(map[string]bool{"ok": true})
	})

	engine := NewEngine(EngineConfig{
		LLM:          llmClient,
		DB:           db,
		Logger:       logger,
		Model:        "test-model",
		SummaryModel: "summary-model",
		MaxTokens:    512,
		Temperature:  0.2,
		ContextConfig: config.ContextConfig{
			InputBudgetTokens:    24000,
			SoftCompactRatio:     0.75,
			HardCompactRatio:     0.92,
			ReserveOutputTokens:  4096,
			KeepRecentUserTurns:  6,
			ToolResultSoftTokens: 1000,
			ToolResultHardTokens: 3000,
		},
		Provider:   "openai",
		Registry:   registry,
		Dispatcher: dispatcher,
	})

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	persona := &config.Persona{
		Name: "default",
		WorkProgressPhrases: map[string][]string{
			"read_file": {"override progress"},
		},
	}

	var wsMessages []WSMessage
	ctx := withWSWriter(context.Background(), func(message WSMessage) {
		wsMessages = append(wsMessages, message)
	})

	if _, err := engine.SendMessage(ctx, sessionID, persona, "please run progress probe", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	var sawProgress bool
	var sawEnd bool
	for _, message := range wsMessages {
		switch message.Type {
		case "work_progress":
			sawProgress = sawProgress || message.Content == "override progress"
		case "work_progress_end":
			sawEnd = true
		}
	}
	if !sawProgress {
		t.Fatalf("ws messages = %#v, want work_progress with override content", wsMessages)
	}
	if !sawEnd {
		t.Fatalf("ws messages = %#v, want work_progress_end", wsMessages)
	}
}
