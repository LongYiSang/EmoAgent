package onebotv11

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type Event struct {
	Time     int64  `json:"time"`
	SelfID   int64  `json:"self_id"`
	PostType string `json:"post_type"`

	MessageType string          `json:"message_type,omitempty"`
	SubType     string          `json:"sub_type,omitempty"`
	MessageID   json.RawMessage `json:"message_id,omitempty"`
	UserID      int64           `json:"user_id,omitempty"`
	GroupID     int64           `json:"group_id,omitempty"`

	Message    RawMessageValue `json:"message,omitempty"`
	RawMessage string          `json:"raw_message,omitempty"`
	Sender     Sender          `json:"sender,omitempty"`
	Anonymous  *Anonymous      `json:"anonymous,omitempty"`

	Raw map[string]any `json:"-"`
}

type Sender struct {
	UserID   int64  `json:"user_id"`
	Nickname string `json:"nickname"`
	Card     string `json:"card,omitempty"`
	Role     string `json:"role,omitempty"`
}

type Anonymous struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Flag string `json:"flag"`
}

func (e *Event) UnmarshalJSON(data []byte) error {
	type eventAlias Event
	var alias eventAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	var raw map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return err
	}
	*e = Event(alias)
	e.Raw = raw
	return nil
}

func (e Event) messageIDString() string {
	return rawValueString(e.MessageID)
}

func rawValueString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var num json.Number
	if err := json.Unmarshal(raw, &num); err == nil {
		return strings.TrimSpace(num.String())
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		if f == float64(int64(f)) {
			return strconv.FormatInt(int64(f), 10)
		}
		return fmt.Sprintf("%v", f)
	}
	return strings.Trim(string(raw), `"`)
}
