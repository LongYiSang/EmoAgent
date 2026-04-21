package work

import (
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/protocol"
)

func validDecisionPacket(taskID string) protocol.DecisionPacket {
	return protocol.DecisionPacket{
		TaskID:      taskID,
		Category:    protocol.CatAuto,
		RiskLevel:   "unexpected-but-ignored",
		GoalSummary: "Summarize the README and identify missing sections.",
		Question:    "Should I keep the current heading structure or flatten it?",
		WhyBlocked:  "Both options are valid and I need a decision to continue.",
		Options: []protocol.DecisionOption{
			{ID: "keep", Summary: "Keep the current heading hierarchy."},
			{ID: "flat", Summary: "Flatten headings into one level."},
		},
		RecommendedOption: "keep",
		RejectOptionID:    "flat",
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

func TestValidateDecisionPacket_RiskLevelIsIgnoredForLLMPackets(t *testing.T) {
	brief := protocol.TaskBrief{TaskID: "task-1"}
	packet := validDecisionPacket(brief.TaskID)
	packet.RiskLevel = "critical"

	if err := ValidateDecisionPacket(&packet, brief); err != nil {
		t.Fatalf("risk_level should be ignored for LLM packets, got: %v", err)
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
	packet.Category = protocol.CatEmotionJudgment
	packet.RelevantFindings = nil
	packet.KeyTradeoffs = nil

	if err := ValidateDecisionPacket(&packet, brief); err == nil {
		t.Fatal("expected emotion_judgment without context to fail validation")
	}
}

func TestValidateDecisionPacket_HumanConfirmationRequiresRecommendationReason(t *testing.T) {
	brief := protocol.TaskBrief{TaskID: "task-1"}
	packet := validDecisionPacket(brief.TaskID)
	packet.Category = protocol.CatHumanConfirmation
	packet.RelevantFindings = []protocol.DecisionEvidence{{Finding: "This may overwrite many files."}}
	packet.RecommendationReason = ""

	if err := ValidateDecisionPacket(&packet, brief); err == nil {
		t.Fatal("expected human_confirmation without recommendation_reason to fail validation")
	}
}

func TestValidateDecisionPacket_HumanConfirmationRequiresRejectOptionID(t *testing.T) {
	brief := protocol.TaskBrief{TaskID: "task-1"}
	packet := validDecisionPacket(brief.TaskID)
	packet.Category = protocol.CatHumanConfirmation
	packet.RelevantFindings = []protocol.DecisionEvidence{{Finding: "This may delete generated files."}}
	packet.RecommendationReason = "Deleting the generated files is the safest fix."
	packet.RejectOptionID = ""

	if err := ValidateDecisionPacket(&packet, brief); err == nil {
		t.Fatal("expected human_confirmation without reject_option_id to fail validation")
	}
}

func TestValidateDecisionPacket_RejectOptionMustExist(t *testing.T) {
	brief := protocol.TaskBrief{TaskID: "task-1"}
	packet := validDecisionPacket(brief.TaskID)
	packet.Category = protocol.CatHumanConfirmation
	packet.RelevantFindings = []protocol.DecisionEvidence{{Finding: "This may delete generated files."}}
	packet.RecommendationReason = "Deleting the generated files is the safest fix."
	packet.RejectOptionID = "nope"

	if err := ValidateDecisionPacket(&packet, brief); err == nil {
		t.Fatal("expected unknown reject_option_id to fail validation")
	}
}

func TestValidateDecisionPacket_AutoAllowsEmptyContext(t *testing.T) {
	brief := protocol.TaskBrief{TaskID: "task-1"}
	packet := validDecisionPacket(brief.TaskID)
	packet.Category = protocol.CatAuto
	packet.RelevantFindings = nil
	packet.KeyTradeoffs = nil

	if err := ValidateDecisionPacket(&packet, brief); err != nil {
		t.Fatalf("auto should allow empty context, got: %v", err)
	}
}

func TestValidateDecisionPacket_ToolApprovalIsRuntimeOnly(t *testing.T) {
	brief := protocol.TaskBrief{TaskID: "task-1"}
	packet := validDecisionPacket(brief.TaskID)
	packet.Category = protocol.CatToolApproval

	err := ValidateDecisionPacket(&packet, brief)
	if err == nil {
		t.Fatal("expected tool_approval to be rejected from LLM packets")
	}
	if !strings.Contains(err.Error(), "runtime-only") {
		t.Fatalf("error = %q, want runtime-only guidance", err)
	}
}
