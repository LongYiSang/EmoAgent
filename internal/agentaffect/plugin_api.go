package agentaffect

import (
	"context"

	"github.com/google/uuid"
)

type PluginAPI struct {
	service Service
	store   Store
}

func NewPluginAPI(service Service, store Store) PluginAPI {
	return PluginAPI{service: service, store: store}
}

func (api PluginAPI) GetCurrentMood(ctx context.Context, pluginID string, req GetCurrentMoodRequest) (GetCurrentMoodResponse, error) {
	if api.service == nil {
		return GetCurrentMoodResponse{}, nil
	}
	req.View = "plugin_safe"
	return api.service.GetCurrentMood(ctx, req)
}

func (api PluginAPI) EvaluateMoodImpact(ctx context.Context, pluginID string, req EvaluateMoodImpactRequest) (EvaluateMoodImpactResponse, error) {
	if api.service == nil {
		return EvaluateMoodImpactResponse{}, nil
	}
	req.Trigger.PluginID = pluginID
	req.Trigger.SourceKind = "plugin"
	return api.service.EvaluateMoodImpact(ctx, req)
}

func (api PluginAPI) SubmitMoodImpact(ctx context.Context, pluginID string, req SubmitMoodImpactRequest) (SubmitMoodImpactResponse, error) {
	if api.service == nil {
		return SubmitMoodImpactResponse{}, nil
	}
	req.Trigger.PluginID = pluginID
	req.Trigger.SourceKind = "plugin"
	resp, err := api.service.SubmitMoodImpact(ctx, req)
	api.audit(ctx, PluginWriteRecord{
		PersonaID:       req.PersonaID,
		SessionID:       req.SessionID,
		TurnID:          req.TurnID,
		PluginID:        pluginID,
		Capability:      "agent_affect.submit",
		RequestKind:     "submit",
		RequestJSON:     mustJSON(req),
		Accepted:        err == nil,
		EvaluationID:    resp.EvaluationID,
		AffectEventID:   resp.EventID,
		RejectionReason: errorString(err),
	})
	return resp, err
}

func (api PluginAPI) ApplyMoodDelta(ctx context.Context, pluginID string, req ApplyMoodDeltaRequest) (ApplyMoodDeltaResponse, error) {
	if api.service == nil {
		return ApplyMoodDeltaResponse{}, nil
	}
	req.Trigger.PluginID = pluginID
	req.Trigger.SourceKind = "plugin"
	req.CommittedBy = "plugin"
	resp, err := api.service.ApplyMoodDelta(ctx, req)
	api.audit(ctx, PluginWriteRecord{
		PersonaID:       req.PersonaID,
		SessionID:       req.SessionID,
		TurnID:          req.TurnID,
		PluginID:        pluginID,
		Capability:      "agent_affect.write_delta",
		RequestKind:     "write_delta",
		RequestJSON:     mustJSON(req),
		Accepted:        err == nil,
		AffectEventID:   resp.EventID,
		RejectionReason: errorString(err),
	})
	return resp, err
}

func (api PluginAPI) audit(ctx context.Context, write PluginWriteRecord) {
	if api.store == nil {
		return
	}
	write.ID = uuid.NewString()
	if write.RequestJSON == "" {
		write.RequestJSON = "{}"
	}
	_ = api.store.InsertPluginWrite(ctx, write)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
