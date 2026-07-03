package command

import "testing"

func TestParserParsesQuotesAndFlags(t *testing.T) {
	parser := NewParser()

	got, handled, err := parser.Parse(`/forget "Alice Bob" --hard --reason "privacy cleanup"`, CommandDescriptor{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !handled {
		t.Fatal("handled = false, want true")
	}
	if got.Name != "forget" {
		t.Fatalf("Name = %q, want forget", got.Name)
	}
	if len(got.Args) != 1 || got.Args[0] != "Alice Bob" {
		t.Fatalf("Args = %#v, want quoted argument", got.Args)
	}
	if got.Flags["hard"] != "true" {
		t.Fatalf("hard flag = %q, want true", got.Flags["hard"])
	}
	if got.Flags["reason"] != "privacy cleanup" {
		t.Fatalf("reason flag = %q, want quoted value", got.Flags["reason"])
	}
}

func TestParserGreedyArgKeepsTailTogether(t *testing.T) {
	parser := NewParser()
	descriptor := CommandDescriptor{
		Name: "forget",
		Args: []CommandArgSpec{
			{Name: "target", Greedy: true},
		},
	}

	got, handled, err := parser.Parse(`/forget Bob remembers "old project" --scope session`, descriptor)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !handled {
		t.Fatal("handled = false, want true")
	}
	if len(got.Args) != 1 || got.Args[0] != "Bob remembers old project" {
		t.Fatalf("Args = %#v, want greedy tail", got.Args)
	}
	if got.Flags["scope"] != "session" {
		t.Fatalf("scope flag = %q, want session", got.Flags["scope"])
	}
}

func TestParserIgnoresPlainMessages(t *testing.T) {
	parser := NewParser()

	got, handled, err := parser.Parse("hello /not-a-command", CommandDescriptor{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if handled {
		t.Fatalf("handled = true for plain message, parsed %#v", got)
	}
}
