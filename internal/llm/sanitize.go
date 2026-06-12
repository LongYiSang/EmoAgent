package llm

import (
	"regexp"
	"strings"
)

const maxDiagnosticTextBytes = 2048

var (
	imageDataURLPattern      = regexp.MustCompile(`(?i)data:image/[a-z0-9.+-]+;base64,[a-z0-9+/=_-]+`)
	imageBase64HeaderPattern = regexp.MustCompile(`(?i)(ivborw0kggo[a-z0-9+/=_-]*|/9j/[a-z0-9+/=_-]*)`)
)

// SanitizeImageDataForDiagnostics removes inline image payloads before text reaches logs or SQLite.
func SanitizeImageDataForDiagnostics(text string) string {
	if text == "" {
		return ""
	}
	text = imageDataURLPattern.ReplaceAllString(text, "[redacted image data]")
	text = imageBase64HeaderPattern.ReplaceAllString(text, "[redacted image data]")
	text = strings.TrimSpace(text)
	if len(text) <= maxDiagnosticTextBytes {
		return text
	}
	return text[:maxDiagnosticTextBytes] + "...[truncated]"
}
