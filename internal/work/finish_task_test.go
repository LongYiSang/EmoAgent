package work

import (
	"strings"
	"testing"
)

func TestFinishTaskDescriptionMentionsStringArraysOnly(t *testing.T) {
	spec := NewFinishTaskTool()

	for _, snippet := range []string{
		"findings and open_questions must be arrays of strings",
		"never arrays of objects",
	} {
		if !strings.Contains(spec.Description, snippet) {
			t.Fatalf("description missing %q: %s", snippet, spec.Description)
		}
	}
}
