package app

import (
	"context"
	"strings"

	"github.com/longyisang/emoagent/internal/logcenter"
	"github.com/longyisang/emoagent/internal/plugin"
	sidecarruntime "github.com/longyisang/emoagent/internal/sidecar"
)

const logCenterTailBytes = 262144

type sidecarLogSource struct {
	service *SidecarService
}

func (s sidecarLogSource) LogCenterSources(ctx context.Context) []logcenter.SourceTail {
	source := logcenter.Source{
		Type:   logcenter.SourceTypeSidecar,
		ID:     "memorycore",
		Label:  "MemoryCore Sidecar",
		Status: logcenter.SourceStatusUnavailable,
	}
	if s.service == nil {
		return []logcenter.SourceTail{{Source: source}}
	}
	status, err := s.service.Status(ctx)
	if err != nil {
		source.Status = logcenter.SourceStatusDegraded
		source.LastError = err.Error()
		return []logcenter.SourceTail{{Source: source}}
	}
	source.Status = sidecarLogStatus(status.State)
	if status.Error != "" {
		source.Status = logcenter.SourceStatusDegraded
		source.LastError = status.Error
	}
	tail, err := s.service.Logs(ctx, logCenterTailBytes)
	if err != nil {
		source.Status = logcenter.SourceStatusDegraded
		source.LastError = err.Error()
	}
	return []logcenter.SourceTail{{Source: source, Tail: tail}}
}

func sidecarLogStatus(state sidecarruntime.State) logcenter.SourceStatus {
	switch state {
	case sidecarruntime.StateHealthy, sidecarruntime.StateStarting:
		return logcenter.SourceStatusActive
	case sidecarruntime.StateDegraded:
		return logcenter.SourceStatusDegraded
	default:
		return logcenter.SourceStatusUnavailable
	}
}

type pluginLogSource struct {
	service *PluginService
}

func (s pluginLogSource) LogCenterSources(ctx context.Context) []logcenter.SourceTail {
	if s.service == nil {
		return nil
	}
	summaries, err := s.service.ListPlugins(ctx)
	if err != nil {
		return nil
	}
	out := make([]logcenter.SourceTail, 0, len(summaries))
	for _, summary := range summaries {
		label := strings.TrimSpace(summary.Name)
		if label == "" {
			label = summary.PluginID
		}
		source := logcenter.Source{
			Type:   logcenter.SourceTypePlugin,
			ID:     summary.PluginID,
			Label:  label,
			Status: pluginLogStatus(summary.Enabled, summary.RuntimeStatus),
		}
		if summary.RuntimeStatus.LastError != "" {
			source.LastError = summary.RuntimeStatus.LastError
		}
		logs, err := s.service.PluginLogs(ctx, summary.PluginID)
		if err != nil {
			source.Status = logcenter.SourceStatusDegraded
			source.LastError = err.Error()
		}
		out = append(out, logcenter.SourceTail{Source: source, Tail: logs.StderrTail})
	}
	return out
}

func pluginLogStatus(enabled bool, status plugin.RuntimeStatus) logcenter.SourceStatus {
	if !enabled {
		return logcenter.SourceStatusUnavailable
	}
	switch strings.TrimSpace(status.Status) {
	case "running", "starting":
		return logcenter.SourceStatusActive
	case "failed", "backoff", "quarantined":
		return logcenter.SourceStatusDegraded
	default:
		return logcenter.SourceStatusUnavailable
	}
}
