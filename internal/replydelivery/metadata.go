package replydelivery

const SchemaVersion = "reply_delivery.v0.1"

type Metadata struct {
	SchemaVersion  string   `json:"schema_version"`
	Mode           string   `json:"mode"`
	Strategy       string   `json:"strategy,omitempty"`
	Segments       []string `json:"segments,omitempty"`
	SegmentCount   int      `json:"segment_count"`
	Suppressed     bool     `json:"suppressed"`
	SuppressReason string   `json:"suppress_reason,omitempty"`
}

func (p Plan) Metadata() Metadata {
	metadata := Metadata{
		SchemaVersion:  SchemaVersion,
		Mode:           p.Mode,
		Strategy:       p.Strategy,
		Suppressed:     p.Suppressed,
		SuppressReason: p.SuppressReason,
	}
	if !p.Suppressed {
		metadata.Segments = append([]string(nil), p.Segments...)
		metadata.SegmentCount = len(metadata.Segments)
	}
	return metadata
}
