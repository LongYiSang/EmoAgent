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
	ChangeSetID         string `json:"changeset_id,omitempty"`
	PlanHash            string `json:"plan_hash,omitempty"`
	ResourceID          string `json:"resource_id,omitempty"`
	CanonicalPathHash   string `json:"canonical_path_hash,omitempty"`
	BaselineHash        string `json:"baseline_hash,omitempty"`
	BaselineFileID      string `json:"baseline_file_id,omitempty"`
	DeleteMode          string `json:"delete_mode,omitempty"`
}

func BuildApprovalBinding(call Call, requestID string, kind ApprovalKind) (ApprovalBinding, error) {
	if kind == "" {
		kind = ApprovalKindDestructiveWrite
	}
	inputHash, err := NormalizedInputHash(call.Input)
	if err != nil {
		return ApprovalBinding{}, err
	}
	binding := ApprovalBinding{
		RequestID:           requestID,
		ApprovalKind:        string(kind),
		ToolName:            strings.TrimSpace(call.Name),
		NormalizedInputHash: inputHash,
		PathDigest:          PathDigestForCall(call),
		InputPreview:        InputPreviewForCall(call),
	}
	applyChangeSetBindingFields(call, &binding)
	return binding, nil
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
	if ok && strings.TrimSpace(pathValue) != "" {
		cleaned := filepath.ToSlash(filepath.Clean(strings.TrimSpace(pathValue)))
		return "sha256:" + sha256Hex([]byte(cleaned))
	}
	if changeSetID, ok := object["changeset_id"].(string); ok && strings.TrimSpace(changeSetID) != "" {
		planHash, _ := object["plan_hash"].(string)
		return "sha256:" + sha256Hex([]byte(strings.TrimSpace(changeSetID)+"\x00"+strings.TrimSpace(planHash)))
	}
	return ""
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
	case "host_apply_change", "host_restore_quarantine":
		return previewChangeSetApplyInput(name, object)
	}
	return previewGenericInput(name, object)
}

func applyChangeSetBindingFields(call Call, binding *ApprovalBinding) {
	if binding == nil {
		return
	}
	name := strings.TrimSpace(call.Name)
	if name != "host_apply_change" && name != "host_restore_quarantine" {
		return
	}
	var object map[string]any
	if err := json.Unmarshal(call.Input, &object); err != nil {
		return
	}
	binding.ChangeSetID = stringField(object, "changeset_id")
	binding.PlanHash = stringField(object, "plan_hash")
	binding.ResourceID = stringField(object, "resource_id")
	binding.CanonicalPathHash = stringField(object, "canonical_path_hash")
	binding.BaselineHash = stringField(object, "baseline_hash")
	binding.BaselineFileID = stringField(object, "baseline_file_id")
	binding.DeleteMode = stringField(object, "delete_mode")
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

func previewChangeSetApplyInput(name string, object map[string]any) string {
	parts := []string{}
	if changeSetID := stringField(object, "changeset_id"); changeSetID != "" {
		parts = append(parts, "changeset_id="+truncatePreview(changeSetID, 80))
	}
	if planHash := stringField(object, "plan_hash"); planHash != "" {
		parts = append(parts, "plan_hash="+truncatePreview(planHash, 96))
	}
	if deleteMode := stringField(object, "delete_mode"); deleteMode != "" {
		parts = append(parts, "delete_mode="+truncatePreview(deleteMode, 40))
	}
	if recursive, ok := object["recursive"].(bool); ok && recursive {
		parts = append(parts, "recursive=true")
	}
	if len(parts) == 0 {
		return name
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

func stringField(object map[string]any, key string) string {
	value, ok := object[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
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
