package command

type CommandService struct {
	registry    *Registry
	parser      Parser
	permissions PermissionChecker
}

func NewCommandService(registry *Registry) *CommandService {
	if registry == nil {
		registry = NewRegistry()
	}
	return &CommandService{
		registry:    registry,
		parser:      NewParser(),
		permissions: NewPermissionChecker(),
	}
}

func (s *CommandService) Registry() *Registry {
	if s == nil {
		return nil
	}
	return s.registry
}

func (s *CommandService) TryParse(input string) (ParsedCommand, CommandDescriptor, bool, error) {
	if s == nil {
		return ParsedCommand{}, CommandDescriptor{}, false, nil
	}
	parsed, handled, err := s.parser.Parse(input, CommandDescriptor{})
	if err != nil || !handled {
		return parsed, CommandDescriptor{}, handled, err
	}
	descriptor, ok := s.registry.Lookup(parsed.Name)
	if !ok {
		return parsed, CommandDescriptor{}, true, nil
	}
	parsed, _, err = s.parser.Parse(input, descriptor)
	return parsed, descriptor, true, err
}

func (s *CommandService) CheckPermission(actor CommandActor, descriptor CommandDescriptor) error {
	if s == nil {
		return nil
	}
	return s.permissions.Check(actor, descriptor.Permission)
}
