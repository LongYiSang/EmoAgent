package onebotv11

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type ActionClient interface {
	Call(context.Context, ActionRequest) (ActionResponse, error)
}

type ActionRequest struct {
	Action string         `json:"action"`
	Params map[string]any `json:"params,omitempty"`
	Echo   string         `json:"echo,omitempty"`
}

type ActionResponse struct {
	Status  string          `json:"status"`
	Retcode int             `json:"retcode"`
	Data    json.RawMessage `json:"data,omitempty"`
	Echo    any             `json:"echo,omitempty"`
	Wording string          `json:"wording,omitempty"`
}

type ActionRetcodeError struct {
	Action   string
	Status   string
	Retcode  int
	Wording  string
	Response ActionResponse
}

func (e ActionRetcodeError) Error() string {
	if e.Wording != "" {
		return fmt.Sprintf("onebot action %s failed: status=%s retcode=%d wording=%s", e.Action, e.Status, e.Retcode, e.Wording)
	}
	return fmt.Sprintf("onebot action %s failed: status=%s retcode=%d", e.Action, e.Status, e.Retcode)
}

func sendPrivateMsgRequest(userID string, message any, autoEscape bool) ActionRequest {
	return ActionRequest{
		Action: "send_private_msg",
		Params: map[string]any{
			"user_id":     onebotIDParam(userID),
			"message":     message,
			"auto_escape": autoEscape,
		},
	}
}

func sendGroupMsgRequest(groupID string, message any, autoEscape bool) ActionRequest {
	return ActionRequest{
		Action: "send_group_msg",
		Params: map[string]any{
			"group_id":    onebotIDParam(groupID),
			"message":     message,
			"auto_escape": autoEscape,
		},
	}
}

func onebotIDParam(id string) any {
	trimmed := strings.TrimSpace(id)
	if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil && n > 0 {
		return n
	}
	return id
}
