package platform

type PlatformStatus struct {
	Enabled  bool            `json:"enabled"`
	Adapters []AdapterStatus `json:"adapters"`
}

type AdapterStatus struct {
	ID             string          `json:"id"`
	Kind           string          `json:"kind"`
	Enabled        bool            `json:"enabled"`
	Implementation string          `json:"implementation,omitempty"`
	SourceType     string          `json:"source_type,omitempty"`
	PlatformID     string          `json:"platform_id,omitempty"`
	InstanceID     string          `json:"instance_id,omitempty"`
	Transport      TransportStatus `json:"transport"`
	Routing        RoutingStatus   `json:"routing"`
	Auth           AuthStatus      `json:"auth"`
}

type TransportStatus struct {
	Mode      string `json:"mode"`
	State     string `json:"state"`
	URL       string `json:"url"`
	SelfID    string `json:"self_id,omitempty"`
	Connected bool   `json:"connected"`
}

type RoutingStatus struct {
	PrivateEnabled     bool `json:"private_enabled"`
	GroupEnabled       bool `json:"group_enabled"`
	IgnoreSelfMessages bool `json:"ignore_self_messages"`
}

type AuthStatus struct {
	AccessTokenConfigured bool `json:"access_token_configured"`
}
