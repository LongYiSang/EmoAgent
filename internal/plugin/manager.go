package plugin

type Manager struct {
	Store           *PluginStore
	Supervisor      *RuntimeSupervisor
	FacadeBroker    *FacadeBroker
	ProviderGateway *ProviderGateway
}

func NewManager(store *PluginStore, supervisor *RuntimeSupervisor, broker *FacadeBroker, gateway *ProviderGateway) *Manager {
	return &Manager{
		Store:           store,
		Supervisor:      supervisor,
		FacadeBroker:    broker,
		ProviderGateway: gateway,
	}
}
