package promptcenter

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

type MemoryStore struct {
	mu        sync.Mutex
	overrides map[string]OverrideRecord
	snapshots map[string]RenderSnapshot
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		overrides: make(map[string]OverrideRecord),
		snapshots: make(map[string]RenderSnapshot),
	}
}

func (s *MemoryStore) GetOverride(_ context.Context, componentID, scopeType, scopeID string) (*OverrideRecord, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.overrides[overrideKey(componentID, scopeType, scopeID)]
	if !ok {
		return nil, nil
	}
	copy := record
	return &copy, nil
}

func (s *MemoryStore) ListOverrides(context.Context) ([]OverrideRecord, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records := make([]OverrideRecord, 0, len(s.overrides))
	for _, record := range s.overrides {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].ComponentID == records[j].ComponentID {
			if records[i].ScopeType == records[j].ScopeType {
				return records[i].ScopeID < records[j].ScopeID
			}
			return records[i].ScopeType < records[j].ScopeType
		}
		return records[i].ComponentID < records[j].ComponentID
	})
	return records, nil
}

func (s *MemoryStore) UpsertOverride(_ context.Context, req UpsertOverrideRequest) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	key := overrideKey(req.ComponentID, req.ScopeType, req.ScopeID)
	record := s.overrides[key]
	if record.ID == "" {
		record.ID = fmt.Sprintf("%s:%s:%s", req.ComponentID, req.ScopeType, req.ScopeID)
		record.CreatedAt = now
	}
	record.ComponentID = req.ComponentID
	record.ScopeType = req.ScopeType
	record.ScopeID = req.ScopeID
	record.Mode = req.Mode
	record.OverrideText = req.OverrideText
	record.Enabled = req.EnabledOrDefault()
	record.Note = req.Note
	if req.TrustDefaultHashAtEdit || req.DefaultHashAtEdit != "" {
		record.DefaultHashAtEdit = req.DefaultHashAtEdit
	}
	record.UpdatedAt = now
	s.overrides[key] = record
	return nil
}

func (s *MemoryStore) DeleteOverride(_ context.Context, componentID, scopeType, scopeID string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.overrides, overrideKey(componentID, scopeType, scopeID))
	return nil
}

func (s *MemoryStore) SaveRenderSnapshot(_ context.Context, snapshot RenderSnapshot) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if snapshot.ID == "" {
		snapshot.ID = fmt.Sprintf("snapshot:%d", len(s.snapshots)+1)
	}
	if snapshot.CreatedAt == "" {
		snapshot.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	s.snapshots[snapshot.ID] = snapshot
	return nil
}

func (s *MemoryStore) ListRenderSnapshots(_ context.Context, filter SnapshotFilter) ([]RenderSnapshotSummary, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []RenderSnapshotSummary
	for _, snapshot := range s.snapshots {
		if filter.AgentID != "" && snapshot.AgentID != filter.AgentID {
			continue
		}
		if filter.SessionID != "" && snapshot.SessionID != filter.SessionID {
			continue
		}
		if filter.Purpose != "" && snapshot.Purpose != filter.Purpose {
			continue
		}
		items = append(items, RenderSnapshotSummary{
			ID:         snapshot.ID,
			SessionID:  snapshot.SessionID,
			AgentID:    snapshot.AgentID,
			PersonaKey: snapshot.PersonaKey,
			Purpose:    snapshot.Purpose,
			Model:      snapshot.Model,
			FinalHash:  snapshot.FinalHash,
			Truncated:  snapshot.Truncated,
			CreatedAt:  snapshot.CreatedAt,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
	if filter.Limit > 0 && len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items, nil
}

func (s *MemoryStore) GetRenderSnapshot(_ context.Context, id string) (*RenderSnapshot, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.snapshots[id]
	if !ok {
		return nil, nil
	}
	copy := snapshot
	return &copy, nil
}

func overrideKey(componentID, scopeType, scopeID string) string {
	return componentID + "\x00" + scopeType + "\x00" + scopeID
}
