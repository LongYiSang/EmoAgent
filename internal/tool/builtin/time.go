package builtin

import (
	"context"
	"encoding/json"
	"time"

	"github.com/longyisang/emoagent/internal/tool"
)

// GetCurrentTimeSpec defines the tool specification for get_current_time.
var GetCurrentTimeSpec = tool.Spec{
	Name:        "get_current_time",
	Description: "Get the current date and time. Returns the current local time.",
	Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	Scope:       tool.ScopeBoth,
	Permission:  tool.PermReadOnly,
}

// currentTimeResponse is the JSON structure returned by the handler.
type currentTimeResponse struct {
	CurrentTime string `json:"current_time"`
	Timezone    string `json:"timezone"`
}

// GetCurrentTimeHandler returns the current local time.
func GetCurrentTimeHandler(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
	now := time.Now()
	zone, _ := now.Zone()

	resp := currentTimeResponse{
		CurrentTime: now.Format("2006-01-02 15:04:05"),
		Timezone:    zone,
	}
	return json.Marshal(resp)
}
