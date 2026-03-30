package llm

import (
	"io"
	"strings"
	"testing"
)

func TestSSEDecoder(t *testing.T) {
	input := `event: message
data: {"text": "hello"}

event: message
data: {"text": "world"}

`
	decoder := NewSSEDecoder(strings.NewReader(input))

	// First event.
	ev, err := decoder.Next()
	if err != nil {
		t.Fatalf("event 1: %v", err)
	}
	if ev.Event != "message" {
		t.Errorf("event 1 type = %q, want message", ev.Event)
	}
	if ev.Data != `{"text": "hello"}` {
		t.Errorf("event 1 data = %q", ev.Data)
	}

	// Second event.
	ev, err = decoder.Next()
	if err != nil {
		t.Fatalf("event 2: %v", err)
	}
	if ev.Data != `{"text": "world"}` {
		t.Errorf("event 2 data = %q", ev.Data)
	}

	// EOF.
	_, err = decoder.Next()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestSSEDecoderDone(t *testing.T) {
	input := `data: {"content": "hi"}

data: [DONE]

`
	decoder := NewSSEDecoder(strings.NewReader(input))

	ev, err := decoder.Next()
	if err != nil {
		t.Fatalf("first event: %v", err)
	}
	if ev.Data != `{"content": "hi"}` {
		t.Errorf("data = %q", ev.Data)
	}

	// [DONE] should return EOF.
	_, err = decoder.Next()
	if err != io.EOF {
		t.Errorf("expected EOF after [DONE], got %v", err)
	}
}

func TestSSEDecoderMultilineData(t *testing.T) {
	input := `data: line1
data: line2

`
	decoder := NewSSEDecoder(strings.NewReader(input))
	ev, err := decoder.Next()
	if err != nil {
		t.Fatalf("multiline: %v", err)
	}
	if ev.Data != "line1\nline2" {
		t.Errorf("multiline data = %q, want line1\\nline2", ev.Data)
	}
}

func TestSSEDecoderLargeEvent(t *testing.T) {
	large := strings.Repeat("x", 70*1024)
	input := "data: " + large + "\n\n"

	decoder := NewSSEDecoder(strings.NewReader(input))
	ev, err := decoder.Next()
	if err != nil {
		t.Fatalf("large event: %v", err)
	}
	if ev.Data != large {
		t.Fatalf("large event size = %d, want %d", len(ev.Data), len(large))
	}
}
