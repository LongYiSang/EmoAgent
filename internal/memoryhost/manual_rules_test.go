package memoryhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func TestDefaultManualRulesMatchPinTemplates(t *testing.T) {
	rules := DefaultManualRules()
	tests := []struct {
		name      string
		input     string
		predicate string
		factType  string
		object    string
		summary   string
	}{
		{
			name:      "explicit like",
			input:     "请记住我喜欢手冲咖啡",
			predicate: "likes",
			factType:  memorycore.FactTypeStablePreference,
			object:    "手冲咖啡",
			summary:   "用户喜欢手冲咖啡。",
		},
		{
			name:      "explicit dislike",
			input:     "请记住我不喜欢早会",
			predicate: "dislikes",
			factType:  memorycore.FactTypeStablePreference,
			object:    "早会",
			summary:   "用户不喜欢早会。",
		},
		{
			name:      "call me",
			input:     "以后叫我 Long",
			predicate: "prefers_name",
			factType:  memorycore.FactTypeCoreIdentity,
			object:    "Long",
			summary:   "用户偏好被称呼为 Long。",
		},
		{
			name:      "my name is",
			input:     "我的名字是 Yi",
			predicate: "prefers_name",
			factType:  memorycore.FactTypeCoreIdentity,
			object:    "Yi",
			summary:   "用户偏好被称呼为 Yi。",
		},
		{
			name:      "prefer more",
			input:     "我更喜欢拿铁",
			predicate: "likes",
			factType:  memorycore.FactTypeStablePreference,
			object:    "拿铁",
			summary:   "用户更喜欢拿铁。",
		},
		{
			name:      "conversation boundary",
			input:     "我不想再聊上线事故",
			predicate: "has_boundary",
			factType:  memorycore.FactTypeRelationalState,
			object:    "上线事故",
			summary:   "用户不想再聊上线事故。",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := rules.Match(tt.input)
			if intent.Kind != ManualMemoryIntentPin {
				t.Fatalf("intent kind = %q, want pin", intent.Kind)
			}
			if intent.Candidate.Predicate != tt.predicate {
				t.Fatalf("predicate = %q, want %q", intent.Candidate.Predicate, tt.predicate)
			}
			if intent.Candidate.FactType != tt.factType {
				t.Fatalf("fact type = %q, want %q", intent.Candidate.FactType, tt.factType)
			}
			if intent.Candidate.ObjectLiteral == nil || *intent.Candidate.ObjectLiteral != tt.object {
				t.Fatalf("object literal = %#v, want %q", intent.Candidate.ObjectLiteral, tt.object)
			}
			if intent.Candidate.ContentSummary != tt.summary {
				t.Fatalf("summary = %q, want %q", intent.Candidate.ContentSummary, tt.summary)
			}
		})
	}
}

func TestManualRulesTrimObjectPunctuationAndRejectEmptyObject(t *testing.T) {
	rules := DefaultManualRules()

	intent := rules.Match("请记住我喜欢  手冲咖啡。 ")
	if intent.Kind != ManualMemoryIntentPin {
		t.Fatalf("intent kind = %q, want pin", intent.Kind)
	}
	if intent.Candidate.ObjectLiteral == nil || *intent.Candidate.ObjectLiteral != "手冲咖啡" {
		t.Fatalf("object literal = %#v, want 手冲咖啡", intent.Candidate.ObjectLiteral)
	}

	empty := rules.Match("请记住我喜欢 。")
	if empty.Kind != ManualMemoryIntentNone {
		t.Fatalf("empty object intent = %#v, want none", empty)
	}
}

func TestManualRulesForgetPrefixTakesPrecedenceOverPin(t *testing.T) {
	rules := DefaultManualRules()

	for _, input := range []string{
		"忘记我喜欢手冲咖啡",
		"别再提请记住我喜欢手冲咖啡",
		"删除请记住我喜欢手冲咖啡",
	} {
		intent := rules.Match(input)
		if intent.Kind != ManualMemoryIntentForget {
			t.Fatalf("input %q intent kind = %q, want forget", input, intent.Kind)
		}
	}
}

func TestLoadManualRulesValidatesRuleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.yaml")
	if err := os.WriteFile(path, []byte(`
pin_rules:
  - prefix: 记住我喜欢
    predicate: likes
    fact_type: stable_preference
    content_summary: 用户喜欢 {object}。
forget_prefixes:
  - 忘记
`), 0o644); err != nil {
		t.Fatalf("WriteFile rules: %v", err)
	}

	rules, err := LoadManualRules(path)
	if err != nil {
		t.Fatalf("LoadManualRules: %v", err)
	}
	if got := rules.Match("记住我喜欢绿茶"); got.Kind != ManualMemoryIntentPin {
		t.Fatalf("loaded rules intent = %#v, want pin", got)
	}

	badPath := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(badPath, []byte(`
pin_rules:
  - prefix: 记住
    predicate: likes
    fact_type: stable_preference
    content_summary: 用户喜欢对象。
`), 0o644); err != nil {
		t.Fatalf("WriteFile bad rules: %v", err)
	}
	_, err = LoadManualRules(badPath)
	if err == nil {
		t.Fatal("LoadManualRules succeeded with missing {object}, want error")
	}
	if !strings.Contains(err.Error(), "content_summary") {
		t.Fatalf("error = %v, want content_summary", err)
	}
}
