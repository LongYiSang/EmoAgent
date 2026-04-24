package apperrors

import "errors"

var (
	ErrLLMProviderExists             = errors.New("llm provider already exists")
	ErrLLMProviderNotFound           = errors.New("llm provider not found")
	ErrLLMProviderInUse              = errors.New("llm provider is referenced by an agent config")
	ErrAgentConfigExists             = errors.New("agent config already exists")
	ErrAgentConfigNotFound           = errors.New("agent config not found")
	ErrCannotDeleteActiveAgentConfig = errors.New("cannot delete the active agent config")
	ErrCannotDeleteLastAgentConfig   = errors.New("cannot delete the last agent config")
	ErrPersonaExists                 = errors.New("persona already exists")
	ErrPersonaNotFound               = errors.New("persona not found")
	ErrCannotDeleteDefault           = errors.New("cannot delete the active default persona")
	ErrSessionNotFound               = errors.New("session not found")
)
