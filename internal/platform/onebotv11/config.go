package onebotv11

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	appconfig "github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/platform"
)

const (
	KindOneBotV11 = "onebot_v11"

	TransportModeWSClient  = "ws_client"
	TransportModeWSReverse = "ws_reverse"

	MessageFormatArray  = "array"
	MessageFormatString = "string"
	MessageFormatAuto   = "auto"

	UnsupportedSegmentPlaceholder = "placeholder"
)

type Config struct {
	AdapterID      string `json:"-"`
	InstanceID     string `json:"-"`
	PlatformID     string `json:"-"`
	Implementation string `json:"implementation"`
	SourceType     string `json:"source_type"`

	Transport TransportConfig `json:"transport"`
	Routing   RoutingConfig   `json:"routing"`
	Message   MessageConfig   `json:"message"`
	Outbound  OutboundConfig  `json:"outbound"`
}

type TransportConfig struct {
	Mode                string `json:"mode"`
	URL                 string `json:"url"`
	ReversePath         string `json:"reverse_path"`
	AccessToken         string `json:"access_token"`
	AccessTokenEnv      string `json:"access_token_env"`
	RequestTimeoutMS    int    `json:"request_timeout_ms"`
	ConnectTimeoutMS    int    `json:"connect_timeout_ms"`
	ReconnectIntervalMS int    `json:"reconnect_interval_ms"`
}

type RoutingConfig struct {
	PrivateEnabled     bool                 `json:"private_enabled"`
	GroupEnabled       bool                 `json:"group_enabled"`
	IgnoreSelfMessages bool                 `json:"ignore_self_messages"`
	PrivateScope       platform.OriginScope `json:"private_scope"`
	GroupScope         platform.OriginScope `json:"group_scope"`
}

type MessageConfig struct {
	InputFormat              string `json:"input_format"`
	OutputFormat             string `json:"output_format"`
	UnsupportedSegmentPolicy string `json:"unsupported_segment_policy"`
	MaxTextChars             int    `json:"max_text_chars"`
}

type OutboundConfig struct {
	AutoEscape            bool   `json:"auto_escape"`
	CoalesceCommandEvents bool   `json:"coalesce_command_events"`
	SplitLongMessages     bool   `json:"split_long_messages"`
	MaxMessageChars       int    `json:"max_message_chars"`
	OutputFormat          string `json:"-"`
	GroupEnabled          bool   `json:"-"`
}

func ParseConfig(adapterID string, adapter appconfig.PlatformAdapterConfig) (Config, error) {
	if strings.TrimSpace(adapter.Kind) != KindOneBotV11 {
		return Config{}, fmt.Errorf("onebot adapter kind must be %s", KindOneBotV11)
	}
	cfg := Config{}
	if adapter.ConfigJSON != nil {
		data, err := json.Marshal(adapter.ConfigJSON)
		if err != nil {
			return Config{}, fmt.Errorf("encode onebot config: %w", err)
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("decode onebot config: %w", err)
		}
	}
	decoded := cfg
	cfg.AdapterID = strings.TrimSpace(adapterID)
	if cfg.AdapterID == "" {
		cfg.AdapterID = strings.TrimSpace(adapter.InstanceID)
	}
	if cfg.AdapterID == "" {
		return Config{}, fmt.Errorf("adapter id is required")
	}
	cfg.InstanceID = strings.TrimSpace(adapter.InstanceID)
	if cfg.InstanceID == "" {
		cfg.InstanceID = cfg.AdapterID
	}
	cfg.PlatformID = strings.TrimSpace(adapter.PlatformID)
	cfg.applyDefaults()
	cfg.restoreExplicitZeroValues(decoded, adapter.ConfigJSON)
	cfg.Outbound.GroupEnabled = cfg.Routing.GroupEnabled
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Implementation == "" {
		c.Implementation = "generic"
	}
	c.Implementation = strings.ToLower(strings.TrimSpace(c.Implementation))
	if c.SourceType == "" {
		c.SourceType = "onebot"
	}
	if c.PlatformID == "" {
		c.PlatformID = "qq"
	}
	if c.Transport.RequestTimeoutMS == 0 {
		c.Transport.RequestTimeoutMS = 15000
	}
	if c.Transport.ConnectTimeoutMS == 0 {
		c.Transport.ConnectTimeoutMS = 5000
	}
	if c.Transport.ReconnectIntervalMS == 0 {
		c.Transport.ReconnectIntervalMS = 3000
	}
	if c.Transport.Mode == "" {
		if strings.TrimSpace(c.Transport.ReversePath) != "" {
			c.Transport.Mode = TransportModeWSReverse
		} else {
			c.Transport.Mode = TransportModeWSClient
		}
	}
	if c.Transport.Mode == TransportModeWSReverse && strings.TrimSpace(c.Transport.ReversePath) == "" {
		c.Transport.ReversePath = "/api/platforms/onebot/v11/" + c.AdapterID + "/ws"
	}
	c.Routing.PrivateEnabled = true
	if c.Routing.PrivateScope == "" {
		c.Routing.PrivateScope = platform.OriginScopePrivate
	}
	if c.Routing.GroupScope == "" {
		c.Routing.GroupScope = platform.OriginScopeGroupShared
	}
	c.Routing.IgnoreSelfMessages = true
	if c.Message.InputFormat == "" {
		c.Message.InputFormat = MessageFormatArray
	}
	if c.Message.OutputFormat == "" {
		c.Message.OutputFormat = MessageFormatArray
	}
	if c.Message.UnsupportedSegmentPolicy == "" {
		c.Message.UnsupportedSegmentPolicy = UnsupportedSegmentPlaceholder
	}
	if c.Message.MaxTextChars == 0 {
		c.Message.MaxTextChars = 8000
	}
	c.Outbound.CoalesceCommandEvents = true
	c.Outbound.SplitLongMessages = true
	if c.Outbound.MaxMessageChars == 0 {
		c.Outbound.MaxMessageChars = 1800
	}
	if c.Outbound.OutputFormat == "" {
		c.Outbound.OutputFormat = c.Message.OutputFormat
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.AdapterID) == "" {
		return fmt.Errorf("adapter id is required")
	}
	if strings.TrimSpace(c.InstanceID) == "" {
		return fmt.Errorf("instance_id is required")
	}
	if strings.TrimSpace(c.SourceType) == "" {
		return fmt.Errorf("source_type is required")
	}
	if _, err := SelectProfile(c.Implementation); err != nil {
		return err
	}
	switch c.Transport.Mode {
	case TransportModeWSClient:
		if strings.TrimSpace(c.Transport.URL) == "" {
			return fmt.Errorf("transport.url is required for ws_client")
		}
	case TransportModeWSReverse:
		if strings.TrimSpace(c.Transport.ReversePath) == "" {
			return fmt.Errorf("transport.reverse_path is required for ws_reverse")
		}
	default:
		return fmt.Errorf("transport.mode must be ws_client or ws_reverse")
	}
	if c.Transport.RequestTimeoutMS <= 0 {
		return fmt.Errorf("transport.request_timeout_ms must be > 0")
	}
	if c.Transport.ConnectTimeoutMS <= 0 {
		return fmt.Errorf("transport.connect_timeout_ms must be > 0")
	}
	if c.Transport.ReconnectIntervalMS <= 0 {
		return fmt.Errorf("transport.reconnect_interval_ms must be > 0")
	}
	switch c.Message.InputFormat {
	case MessageFormatArray, MessageFormatString, MessageFormatAuto:
	default:
		return fmt.Errorf("message.input_format must be array, string, or auto")
	}
	switch c.Message.OutputFormat {
	case MessageFormatArray, MessageFormatString:
	default:
		return fmt.Errorf("message.output_format must be array or string")
	}
	if c.Message.MaxTextChars <= 0 {
		return fmt.Errorf("message.max_text_chars must be > 0")
	}
	if c.Outbound.MaxMessageChars <= 0 {
		return fmt.Errorf("outbound.max_message_chars must be > 0")
	}
	return nil
}

func (c TransportConfig) BearerToken() string {
	if env := strings.TrimSpace(c.AccessTokenEnv); env != "" {
		if value := strings.TrimSpace(os.Getenv(env)); value != "" {
			return value
		}
	}
	return strings.TrimSpace(c.AccessToken)
}

func (c *Config) restoreExplicitZeroValues(decoded Config, raw map[string]any) {
	if hasNestedKey(raw, "routing", "private_enabled") {
		c.Routing.PrivateEnabled = decoded.Routing.PrivateEnabled
	}
	if hasNestedKey(raw, "routing", "ignore_self_messages") {
		c.Routing.IgnoreSelfMessages = decoded.Routing.IgnoreSelfMessages
	}
	if hasNestedKey(raw, "outbound", "coalesce_command_events") {
		c.Outbound.CoalesceCommandEvents = decoded.Outbound.CoalesceCommandEvents
	}
	if hasNestedKey(raw, "outbound", "split_long_messages") {
		c.Outbound.SplitLongMessages = decoded.Outbound.SplitLongMessages
	}
}

func hasNestedKey(raw map[string]any, section string, key string) bool {
	if raw == nil {
		return false
	}
	value, ok := raw[section]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case map[string]any:
		_, ok := typed[key]
		return ok
	default:
		return false
	}
}
