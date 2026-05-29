package memoryhost

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"gopkg.in/yaml.v3"
)

type ManualMemoryIntentKind string

const (
	ManualMemoryIntentNone   ManualMemoryIntentKind = ""
	ManualMemoryIntentPin    ManualMemoryIntentKind = "pin"
	ManualMemoryIntentForget ManualMemoryIntentKind = "forget"
)

type ManualRules struct {
	PinRules       []ManualPinRule `yaml:"pin_rules"`
	ForgetPrefixes []string        `yaml:"forget_prefixes"`
}

type ManualPinRule struct {
	Prefix         string `yaml:"prefix"`
	Predicate      string `yaml:"predicate"`
	FactType       string `yaml:"fact_type"`
	ContentSummary string `yaml:"content_summary"`
}

type ManualMemoryIntent struct {
	Kind        ManualMemoryIntentKind
	Candidate   memorycore.ManualFactCandidate
	ForgetQuery string
}

func DefaultManualRules() *ManualRules {
	return &ManualRules{
		ForgetPrefixes: defaultManualForgetPrefixes(),
		PinRules: []ManualPinRule{
			{
				Prefix:         "请记住我喜欢",
				Predicate:      "likes",
				FactType:       memorycore.FactTypeStablePreference,
				ContentSummary: "用户喜欢{object}。",
			},
			{
				Prefix:         "请记住我不喜欢",
				Predicate:      "dislikes",
				FactType:       memorycore.FactTypeStablePreference,
				ContentSummary: "用户不喜欢{object}。",
			},
			{
				Prefix:         "以后叫我",
				Predicate:      "prefers_name",
				FactType:       memorycore.FactTypeCoreIdentity,
				ContentSummary: "用户偏好被称呼为 {object}。",
			},
			{
				Prefix:         "我的名字是",
				Predicate:      "prefers_name",
				FactType:       memorycore.FactTypeCoreIdentity,
				ContentSummary: "用户偏好被称呼为 {object}。",
			},
			{
				Prefix:         "我更喜欢",
				Predicate:      "likes",
				FactType:       memorycore.FactTypeStablePreference,
				ContentSummary: "用户更喜欢{object}。",
			},
			{
				Prefix:         "我不想再聊",
				Predicate:      "has_boundary",
				FactType:       memorycore.FactTypeRelationalState,
				ContentSummary: "用户不想再聊{object}。",
			},
		},
	}
}

func LoadManualRules(path string) (*ManualRules, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("manual rules path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	var rules ManualRules
	if err := yaml.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	rules.applyDefaults()
	if err := rules.Validate(); err != nil {
		return nil, fmt.Errorf("validate %q: %w", path, err)
	}
	return &rules, nil
}

func (r *ManualRules) Validate() error {
	if r == nil {
		return fmt.Errorf("manual rules are required")
	}
	if len(r.PinRules) == 0 {
		return fmt.Errorf("at least one pin rule is required")
	}
	for i := range r.PinRules {
		rule := r.PinRules[i]
		if strings.TrimSpace(rule.Prefix) == "" {
			return fmt.Errorf("pin_rules[%d].prefix is required", i)
		}
		if strings.TrimSpace(rule.Predicate) == "" {
			return fmt.Errorf("pin_rules[%d].predicate is required", i)
		}
		if err := validateManualFactType(rule.FactType); err != nil {
			return fmt.Errorf("pin_rules[%d].fact_type: %w", i, err)
		}
		if strings.TrimSpace(rule.ContentSummary) == "" {
			return fmt.Errorf("pin_rules[%d].content_summary is required", i)
		}
		if !strings.Contains(rule.ContentSummary, "{object}") {
			return fmt.Errorf("pin_rules[%d].content_summary must contain {object}", i)
		}
	}
	return nil
}

func (r *ManualRules) Match(text string) ManualMemoryIntent {
	if r == nil {
		return ManualMemoryIntent{}
	}
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return ManualMemoryIntent{}
	}
	for _, prefix := range sortedForgetPrefixes(r.ForgetPrefixes) {
		if !strings.HasPrefix(normalized, prefix) {
			continue
		}
		query := trimManualObject(strings.TrimPrefix(normalized, prefix))
		if query == "" {
			return ManualMemoryIntent{}
		}
		return ManualMemoryIntent{
			Kind:        ManualMemoryIntentForget,
			ForgetQuery: query,
		}
	}
	for _, rule := range sortedPinRules(r.PinRules) {
		if !strings.HasPrefix(normalized, rule.Prefix) {
			continue
		}
		object := trimManualObject(strings.TrimPrefix(normalized, rule.Prefix))
		if object == "" {
			return ManualMemoryIntent{}
		}
		objectCopy := object
		return ManualMemoryIntent{
			Kind: ManualMemoryIntentPin,
			Candidate: memorycore.ManualFactCandidate{
				Predicate:       rule.Predicate,
				ObjectLiteral:   &objectCopy,
				ContentSummary:  strings.ReplaceAll(rule.ContentSummary, "{object}", object),
				FactType:        rule.FactType,
				Confidence:      memorycore.ConfidenceExplicit,
				ConfidenceScore: 0.9,
				Importance:      0.7,
				Pinned:          true,
				UserRequested:   true,
			},
		}
	}
	return ManualMemoryIntent{}
}

func (r *ManualRules) applyDefaults() {
	if r == nil {
		return
	}
	if len(sortedForgetPrefixes(r.ForgetPrefixes)) == 0 {
		r.ForgetPrefixes = defaultManualForgetPrefixes()
	}
}

func defaultManualForgetPrefixes() []string {
	return []string{"不要再提", "别再提", "忘记", "删除"}
}

func validateManualFactType(value string) error {
	switch value {
	case memorycore.FactTypeCoreIdentity,
		memorycore.FactTypeSignificantEvent,
		memorycore.FactTypeStablePreference,
		memorycore.FactTypeRelationalState,
		memorycore.FactTypeCommitment,
		memorycore.FactTypeTransientContext,
		memorycore.FactTypeTaskRelevantContext:
		return nil
	default:
		return fmt.Errorf("unsupported fact type %q", value)
	}
}

func sortedPinRules(rules []ManualPinRule) []ManualPinRule {
	out := make([]ManualPinRule, 0, len(rules))
	for _, rule := range rules {
		rule.Prefix = strings.TrimSpace(rule.Prefix)
		rule.Predicate = strings.TrimSpace(rule.Predicate)
		rule.FactType = strings.TrimSpace(rule.FactType)
		rule.ContentSummary = strings.TrimSpace(rule.ContentSummary)
		if rule.Prefix != "" {
			out = append(out, rule)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return len([]rune(out[i].Prefix)) > len([]rune(out[j].Prefix))
	})
	return out
}

func sortedForgetPrefixes(prefixes []string) []string {
	out := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix != "" {
			out = append(out, prefix)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return len([]rune(out[i])) > len([]rune(out[j]))
	})
	return out
}

func trimManualObject(value string) string {
	return strings.Trim(strings.TrimSpace(value), " \t\r\n。！？!?.,，；;：:\"'“”‘’")
}
