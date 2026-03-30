package apperrors

import "errors"

var (
	ErrLLMProfileExists             = errors.New("llm profile already exists")
	ErrLLMProfileNotFound           = errors.New("llm profile not found")
	ErrCannotDeleteActiveLLMProfile = errors.New("cannot delete the active llm profile")
	ErrCannotDeleteLastLLMProfile   = errors.New("cannot delete the last llm profile")
	ErrPersonaExists                = errors.New("persona already exists")
	ErrPersonaNotFound              = errors.New("persona not found")
	ErrCannotDeleteDefault          = errors.New("cannot delete the active default persona")
)
