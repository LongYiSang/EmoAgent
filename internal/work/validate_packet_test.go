package work

import (
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/protocol"
)

func validDecisionPacket(taskID string) protocol.DecisionPacket {
	return protocol.DecisionPacket{
		TaskID:      taskID,
		Category:    protocol.CatExecutionOnly,
		RiskLevel:   "low",
		GoalSummary: "Summarize the README and identify missing sections.",
		Question:    "Should I keep the current heading structure or flatten it?",
		WhyBlocked:  "Both options are valid and I need a decision to continue.",
		Options: []protocol.DecisionOption{
			{ID: "keep", Summary: "Keep the current heading hierarchy."},
			{ID: "flat", Summary: "Flatten headings into one level."},
		},
		RecommendedOption: "keep",
	}
}

func TestValidateDecisionPacket_Valid(t *testing.T) {
	brief := protocol.TaskBrief{TaskID: "task-1"}
	packet := validDecisionPacket(brief.TaskID)
	if err := ValidateDecisionPacket(&packet, brief); err != nil {
		t.Fatalf("ValidateDecisionPacket returned error: %v", err)
	}
}

func TestValidateDecisionPacket_MissingRequiredFields(t *testing.T) {
	brief := protocol.TaskBrief{TaskID: "task-1"}
	packet := validDecisionPacket(brief.TaskID)
	packet.Question = ""

	if err := ValidateDecisionPacket(&packet, brief); err == nil {
		t.Fatal("expected missing question to fail validation")
	}
}

func TestValidateDecisionPacket_InvalidCategory(t *testing.T) {
	brief := protocol.TaskBrief{TaskID: "task-1"}
	packet := validDecisionPacket(brief.TaskID)
	packet.Category = protocol.EscalationCategory("not_valid")

	if err := ValidateDecisionPacket(&packet, brief); err == nil {
		t.Fatal("expected invalid category to fail validation")
	}
}

func TestValidateDecisionPacket_InvalidRiskLevel(t *testing.T) {
	brief := protocol.TaskBrief{TaskID: "task-1"}
	packet := validDecisionPacket(brief.TaskID)
	packet.RiskLevel = "critical"

	if err := ValidateDecisionPacket(&packet, brief); err == nil {
		t.Fatal("expected invalid risk level to fail validation")
	}
}

func TestValidateDecisionPacket_OptionsCountRange(t *testing.T) {
	brief := protocol.TaskBrief{TaskID: "task-1"}
	packet := validDecisionPacket(brief.TaskID)
	packet.Options = nil
	if err := ValidateDecisionPacket(&packet, brief); err == nil {
		t.Fatal("expected empty options to fail validation")
	}

	packet = validDecisionPacket(brief.TaskID)
	packet.Options = make([]protocol.DecisionOption, 9)
	for i := range packet.Options {
		packet.Options[i] = protocol.DecisionOption{
			ID:      string(rune('a' + i)),
			Summary: "option",
		}
	}
	if err := ValidateDecisionPacket(&packet, brief); err == nil {
		t.Fatal("expected options > 8 to fail validation")
	}
}

func TestValidateDecisionPacket_DuplicateOptionIDs(t *testing.T) {
	brief := protocol.TaskBrief{TaskID: "task-1"}
	packet := validDecisionPacket(brief.TaskID)
	packet.Options[1].ID = packet.Options[0].ID

	if err := ValidateDecisionPacket(&packet, brief); err == nil {
		t.Fatal("expected duplicate option IDs to fail validation")
	}
}

func TestValidateDecisionPacket_RecommendedOptionMustExist(t *testing.T) {
	brief := protocol.TaskBrief{TaskID: "task-1"}
	packet := validDecisionPacket(brief.TaskID)
	packet.RecommendedOption = "unknown"

	if err := ValidateDecisionPacket(&packet, brief); err == nil {
		t.Fatal("expected unknown recommended option to fail validation")
	}
}

func TestValidateDecisionPacket_FieldLengthLimit(t *testing.T) {
	brief := protocol.TaskBrief{TaskID: "task-1"}
	packet := validDecisionPacket(brief.TaskID)
	packet.Question = strings.Repeat("q", maxPacketQuestionRunes+1)

	if err := ValidateDecisionPacket(&packet, brief); err == nil {
		t.Fatal("expected overlong question to fail validation")
	}
}

func TestValidateDecisionPacket_TaskIDMustMatchBrief(t *testing.T) {
	brief := protocol.TaskBrief{TaskID: "task-brief"}
	packet := validDecisionPacket("task-other")

	if err := ValidateDecisionPacket(&packet, brief); err == nil {
		t.Fatal("expected task_id mismatch to fail validation")
	}
}

func TestValidateDecisionPacket_CategoryAwareMinimumContext(t *testing.T) {
	brief := protocol.TaskBrief{TaskID: "task-1"}
	packet := validDecisionPacket(brief.TaskID)
	packet.Category = protocol.CatPreferenceSensitive
	packet.RelevantFindings = nil
	packet.KeyTradeoffs = nil

	if err := ValidateDecisionPacket(&packet, brief); err == nil {
		t.Fatal("expected preference_sensitive without context to fail validation")
	}
}

func TestValidateDecisionPacket_HighRiskRequiresRecommendationReason(t *testing.T) {
	brief := protocol.TaskBrief{TaskID: "task-1"}
	packet := validDecisionPacket(brief.TaskID)
	packet.Category = protocol.CatHighRisk
	packet.RelevantFindings = []protocol.DecisionEvidence{{Finding: "This may overwrite many files."}}
	packet.RecommendationReason = ""

	if err := ValidateDecisionPacket(&packet, brief); err == nil {
		t.Fatal("expected high_risk without recommendation_reason to fail validation")
	}
}

func TestValidateDecisionPacket_ExecutionOnlyAllowsEmptyContext(t *testing.T) {
	brief := protocol.TaskBrief{TaskID: "task-1"}
	packet := validDecisionPacket(brief.TaskID)
	packet.Category = protocol.CatExecutionOnly
	packet.RelevantFindings = nil
	packet.KeyTradeoffs = nil

	if err := ValidateDecisionPacket(&packet, brief); err != nil {
		t.Fatalf("execution_only should allow empty context, got: %v", err)
	}
}
