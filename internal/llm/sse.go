package llm

import (
	"bufio"
	"io"
	"strings"
)

const maxSSETokenSize = 4 * 1024 * 1024

// SSEEvent represents a single Server-Sent Events event.
type SSEEvent struct {
	Event string
	Data  string
}

// SSEDecoder reads SSE events from a stream.
type SSEDecoder struct {
	scanner *bufio.Scanner
}

// NewSSEDecoder creates an SSE decoder from an io.Reader.
func NewSSEDecoder(r io.Reader) *SSEDecoder {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, bufio.MaxScanTokenSize), maxSSETokenSize)
	return &SSEDecoder{scanner: scanner}
}

// Next reads the next SSE event. Returns io.EOF when the stream ends.
func (d *SSEDecoder) Next() (SSEEvent, error) {
	var event SSEEvent
	var dataLines []string

	for d.scanner.Scan() {
		line := d.scanner.Text()

		// Empty line signals end of event.
		if line == "" {
			if len(dataLines) > 0 {
				event.Data = strings.Join(dataLines, "\n")
				return event, nil
			}
			continue
		}

		if strings.HasPrefix(line, "event:") {
			event.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			// OpenAI uses [DONE] to signal end of stream.
			if data == "[DONE]" {
				return SSEEvent{}, io.EOF
			}
			dataLines = append(dataLines, data)
		}
		// Ignore id:, retry:, and comment lines (starting with :)
	}

	if err := d.scanner.Err(); err != nil {
		return SSEEvent{}, err
	}

	// If we collected data but stream ended without trailing empty line.
	if len(dataLines) > 0 {
		event.Data = strings.Join(dataLines, "\n")
		return event, nil
	}

	return SSEEvent{}, io.EOF
}
