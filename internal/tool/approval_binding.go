package tool

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type ApprovalBinding struct {
	RequestID           string `json:"request_id,omitempty"`
	ApprovalKind        string `json:"approval_kind"`
	ToolName            string `json:"tool_name"`
	NormalizedInputHash string `json:"normalized_input_hash"`
	PathDigest          string `json:"path_digest,omitempty"`
	InputPreview        string `json:"input_preview,omitempty"`
}

func BuildApprovalBinding(call Call, requestID string, kind ApprovalKind) (ApprovalBinding, error) {
	if kind == "" {
		kind = ApprovalKindDestructiveWrite
	}
	inputHash, err := NormalizedInputHash(call.Input)
	if err != nil {
		return ApprovalBinding{}, err
	}
	return ApprovalBinding{
		RequestID:           requestID,
		ApprovalKind:        string(kind),
		ToolName:            strings.TrimSpace(call.Name),
		NormalizedInputHash: inputHash,
		PathDigest:          PathDigestForCall(call),
		InputPreview:        InputPreviewForCall(call),
	}, nil
}

func NormalizedInputHash(input json.RawMessage) (string, error) {
	canonical, err := canonicalJSON(input)
	if err != nil {
		return "", err
	}
	return "sha256:" + sha256Hex(canonical), nil
}

func PathDigestForCall(call Call) string {
	var object map[string]any
	if err := json.Unmarshal(call.Input, &object); err != nil {
		return ""
	}
	pathValue, ok := object["path"].(string)
	if !ok || strings.TrimSpace(pathValue) == "" {
		return ""
	}
	cleaned := filepath.ToSlash(filepath.Clean(strings.TrimSpace(pathValue)))
	return "sha256:" + sha256Hex([]byte(cleaned))
}

func InputPreviewForCall(call Call) string {
	var object map[string]any
	if err := json.Unmarshal(call.Input, &object); err != nil || len(object) == 0 {
		return strings.TrimSpace(call.Name)
	}

	name := strings.TrimSpace(call.Name)
	switch name {
	case "write_file":
		return previewWriteFileInput(object)
	case "edit_file":
		return previewEditFileInput(object)
	case "bash":
		if command, ok := object["command"].(string); ok {
			return "command=" + truncatePreview(command, 160)
		}
	}
	return previewGenericInput(name, object)
}

func canonicalJSON(input json.RawMessage) ([]byte, error) {
	if len(bytes.TrimSpace(input)) == 0 {
		input = json.RawMessage(`null`)
	}
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("canonicalize input JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("canonicalize input JSON: multiple JSON values")
		}
		return nil, fmt.Errorf("canonicalize input JSON: %w", err)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical input JSON: %w", err)
	}
	return canonical, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func previewWriteFileInput(object map[string]any) string {
	parts := []string{}
	if path, ok := object["path"].(string); ok && strings.TrimSpace(path) != "" {
		parts = append(parts, "path="+cleanPreviewPath(path))
	}
	if content, ok := object["content"].(string); ok {
		parts = append(parts, "content_bytes="+strconv.Itoa(len(content)))
	}
	if createDirs, ok := object["create_dirs"].(bool); ok {
		parts = append(parts, "create_dirs="+strconv.FormatBool(createDirs))
	}
	return strings.Join(parts, ", ")
}

func previewEditFileInput(object map[string]any) string {
	parts := []string{}
	if path, ok := object["path"].(string); ok && strings.TrimSpace(path) != "" {
		parts = append(parts, "path="+cleanPreviewPath(path))
	}
	if oldString, ok := object["old_string"].(string); ok {
		parts = append(parts, "old_string_bytes="+strconv.Itoa(len(oldString)))
	}
	if newString, ok := object["new_string"].(string); ok {
		parts = append(parts, "new_string_bytes="+strconv.Itoa(len(newString)))
	}
	if replaceAll, ok := object["replace_all"].(bool); ok {
		parts = append(parts, "replace_all="+strconv.FormatBool(replaceAll))
	}
	return strings.Join(parts, ", ")
}

func previewGenericInput(name string, object map[string]any) string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, min(len(keys), 4))
	for i, key := range keys {
		if i >= 4 {
			parts = append(parts, "...")
			break
		}
		parts = append(parts, key+"="+previewValue(object[key]))
	}
	if name == "" {
		return strings.Join(parts, ", ")
	}
	return name + " (" + strings.Join(parts, ", ") + ")"
}

func previewValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case string:
		return truncatePreview(typed, 80)
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case []any:
		return fmt.Sprintf("<array:%d>", len(typed))
	case map[string]any:
		return "<object>"
	default:
		return fmt.Sprint(typed)
	}
}

func cleanPreviewPath(path string) string {
	return filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
}

func truncatePreview(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "..."
}
