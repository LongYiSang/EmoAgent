package platform

import "sort"

type RegisteredAdapter struct {
	ID      string
	Adapter Adapter
}

type Manager struct {
	adapters map[string]Adapter
}

func NewManager() *Manager {
	return &Manager{adapters: map[string]Adapter{}}
}

func (m *Manager) Register(id string, adapter Adapter) {
	if m == nil || id == "" || adapter == nil {
		return
	}
	m.adapters[id] = adapter
}

func (m *Manager) Adapter(id string) (Adapter, bool) {
	if m == nil {
		return nil, false
	}
	adapter, ok := m.adapters[id]
	return adapter, ok
}

func (m *Manager) List() []RegisteredAdapter {
	if m == nil || len(m.adapters) == 0 {
		return nil
	}
	ids := make([]string, 0, len(m.adapters))
	for id := range m.adapters {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]RegisteredAdapter, 0, len(ids))
	for _, id := range ids {
		out = append(out, RegisteredAdapter{ID: id, Adapter: m.adapters[id]})
	}
	return out
}
