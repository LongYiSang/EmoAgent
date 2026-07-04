package platform

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
