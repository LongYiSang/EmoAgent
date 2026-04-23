package work

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/longyisang/emoagent/internal/protocol"
)

const (
	maxPacketGoalSummaryRunes          = 400
	maxPacketQuestionRunes             = 500
	maxPacketWhyBlockedRunes           = 400
	maxPacketOptionSummaryRunes        = 300
	maxPacketFindingRunes              = 300
	maxPacketSourceRunes               = 200
	maxPacketTradeoffDimensionRunes    = 120
	maxPacketTradeoffNoteRunes         = 200
	maxPacketRecommendationReasonRunes = 400
	maxPacketOptions                   = 8
	maxPacketFindings                  = 12
	maxPacketTradeoffs                 = 8
)

var validEscalationCategories = map[protocol.EscalationCategory]struct{}{
	protocol.CatAuto:              {},
	protocol.CatEmotionJudgment:   {},
	protocol.CatHumanConfirmation: {},
}

// ValidateDecisionPacket validates a Work escalation packet before routing.
func ValidateDecisionPacket(packet *protocol.DecisionPacket, brief protocol.TaskBrief) error {
	if packet == nil {
		return fmt.Errorf("decision packet is required")
	}

	if packet.TaskID == "" {
		return fmt.Errorf("decision packet task_id is required")
	}
	if brief.TaskID != "" && packet.TaskID != brief.TaskID {
		return fmt.Errorf("decision packet task_id %q does not match brief %q", packet.TaskID, brief.TaskID)
	}
	if packet.Category == protocol.CatToolApproval || packet.Category == protocol.CatPermissionEscalationRequired {
		return fmt.Errorf("category %q is runtime-only and must not appear in LLM decision packets", packet.Category)
	}
	if _, ok := validEscalationCategories[packet.Category]; !ok {
		return fmt.Errorf("invalid decision category %q", packet.Category)
	}

	if err := validateBoundedRequired("goal_summary", packet.GoalSummary, maxPacketGoalSummaryRunes); err != nil {
		return err
	}
	if err := validateBoundedRequired("question", packet.Question, maxPacketQuestionRunes); err != nil {
		return err
	}
	if err := validateBoundedRequired("why_blocked", packet.WhyBlocked, maxPacketWhyBlockedRunes); err != nil {
		return err
	}

	if len(packet.Options) == 0 {
		return fmt.Errorf("options must contain at least 1 item")
	}
	if len(packet.Options) > maxPacketOptions {
		return fmt.Errorf("options must not exceed %d items", maxPacketOptions)
	}

	optionIDs := make(map[string]struct{}, len(packet.Options))
	for i, option := range packet.Options {
		if err := validateBoundedRequired(fmt.Sprintf("options[%d].id", i), option.ID, 80); err != nil {
			return err
		}
		if _, exists := optionIDs[option.ID]; exists {
			return fmt.Errorf("options contain duplicate id %q", option.ID)
		}
		optionIDs[option.ID] = struct{}{}
		if err := validateBoundedRequired(fmt.Sprintf("options[%d].summary", i), option.Summary, maxPacketOptionSummaryRunes); err != nil {
			return err
		}
		if err := validateListBounded(fmt.Sprintf("options[%d].pros", i), option.Pros, 200, false); err != nil {
			return err
		}
		if err := validateListBounded(fmt.Sprintf("options[%d].cons", i), option.Cons, 200, false); err != nil {
			return err
		}
		if err := validateListBounded(fmt.Sprintf("options[%d].side_effects", i), option.SideEffects, 200, false); err != nil {
			return err
		}
	}

	if packet.RecommendedOption != "" {
		if _, ok := optionIDs[packet.RecommendedOption]; !ok {
			return fmt.Errorf("recommended_option %q does not match any option id", packet.RecommendedOption)
		}
	}
	if packet.RejectOptionID != "" {
		if _, ok := optionIDs[packet.RejectOptionID]; !ok {
			return fmt.Errorf("reject_option_id %q does not match any option id", packet.RejectOptionID)
		}
	}
	if err := validateBoundedOptional("recommendation_reason", packet.RecommendationReason, maxPacketRecommendationReasonRunes); err != nil {
		return err
	}

	if len(packet.RelevantFindings) > maxPacketFindings {
		return fmt.Errorf("relevant_findings must not exceed %d items", maxPacketFindings)
	}
	for i, finding := range packet.RelevantFindings {
		if err := validateBoundedRequired(fmt.Sprintf("relevant_findings[%d].finding", i), finding.Finding, maxPacketFindingRunes); err != nil {
			return err
		}
		if err := validateBoundedOptional(fmt.Sprintf("relevant_findings[%d].source", i), finding.Source, maxPacketSourceRunes); err != nil {
			return err
		}
	}

	if len(packet.KeyTradeoffs) > maxPacketTradeoffs {
		return fmt.Errorf("key_tradeoffs must not exceed %d items", maxPacketTradeoffs)
	}
	for i, tradeoff := range packet.KeyTradeoffs {
		if err := validateBoundedRequired(fmt.Sprintf("key_tradeoffs[%d].dimension", i), tradeoff.Dimension, maxPacketTradeoffDimensionRunes); err != nil {
			return err
		}
		if err := validateBoundedRequired(fmt.Sprintf("key_tradeoffs[%d].note", i), tradeoff.Note, maxPacketTradeoffNoteRunes); err != nil {
			return err
		}
	}

	if packet.Category != protocol.CatAuto &&
		len(packet.RelevantFindings) == 0 &&
		len(packet.KeyTradeoffs) == 0 {
		return fmt.Errorf("category %q requires relevant_findings or key_tradeoffs", packet.Category)
	}

	if packet.Category == protocol.CatHumanConfirmation {
		if strings.TrimSpace(packet.RecommendationReason) == "" {
			return fmt.Errorf("category %q requires recommendation_reason", packet.Category)
		}
		if strings.TrimSpace(packet.RejectOptionID) == "" {
			return fmt.Errorf("category %q requires reject_option_id", packet.Category)
		}
	}

	return nil
}

func validateBoundedRequired(field, value string, maxRunes int) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	return validateBoundedOptional(field, value, maxRunes)
}

func validateBoundedOptional(field, value string, maxRunes int) error {
	if value == "" {
		return nil
	}
	if utf8.RuneCountInString(value) > maxRunes {
		return fmt.Errorf("%s exceeds %d runes", field, maxRunes)
	}
	return nil
}

func validateListBounded(field string, values []string, maxRunes int, required bool) error {
	if len(values) == 0 {
		if required {
			return fmt.Errorf("%s requires at least 1 item", field)
		}
		return nil
	}
	for i, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s[%d] is required", field, i)
		}
		if utf8.RuneCountInString(value) > maxRunes {
			return fmt.Errorf("%s[%d] exceeds %d runes", field, i, maxRunes)
		}
	}
	return nil
}
