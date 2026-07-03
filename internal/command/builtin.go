package command

type BuiltinProvider struct{}

func NewBuiltinProvider() BuiltinProvider {
	return BuiltinProvider{}
}

func (BuiltinProvider) Descriptors() []CommandDescriptor {
	descriptors := make([]CommandDescriptor, 0, len(reservedRootCommands))
	for _, name := range reservedRootCommands {
		descriptors = append(descriptors, CommandDescriptor{
			ID:           "builtin." + name,
			Name:         name,
			Reserved:     true,
			ProviderKind: CommandProviderBuiltin,
			Permission:   CommandPermissionMember,
			Scope:        CommandScopeOrigin,
			OutputMode:   CommandOutputDirect,
		})
	}
	return descriptors
}
