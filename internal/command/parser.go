package command

import (
	"fmt"
	"strings"
)

type Parser struct{}

func NewParser() Parser {
	return Parser{}
}

func (Parser) Parse(input string, descriptor CommandDescriptor) (ParsedCommand, bool, error) {
	raw := input
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") {
		return ParsedCommand{}, false, nil
	}

	tokens, err := commandTokens(strings.TrimPrefix(input, "/"))
	if err != nil {
		return ParsedCommand{}, true, err
	}
	if len(tokens) == 0 || strings.TrimSpace(tokens[0]) == "" {
		return ParsedCommand{}, true, fmt.Errorf("command name is required")
	}

	parsed := ParsedCommand{
		Raw:   raw,
		Name:  normalizeRoot(tokens[0]),
		Flags: make(map[string]string),
	}
	positionals := make([]string, 0, len(tokens)-1)
	for i := 1; i < len(tokens); i++ {
		token := tokens[i]
		if strings.HasPrefix(token, "--") && len(token) > 2 {
			key, value, consumedNext := parseFlag(token[2:], tokens, i)
			if key != "" {
				parsed.Flags[key] = value
			}
			if consumedNext {
				i++
			}
			continue
		}
		positionals = append(positionals, token)
	}
	parsed.Args = applyGreedyArgs(positionals, descriptor.Args)
	return parsed, true, nil
}

func commandTokens(input string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	var quote rune
	escaped := false

	for _, r := range input {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(r)
	}
	if escaped {
		current.WriteRune('\\')
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quoted string")
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens, nil
}

func parseFlag(raw string, tokens []string, index int) (string, string, bool) {
	key, value, hasValue := strings.Cut(raw, "=")
	key = normalizeRoot(key)
	if key == "" {
		return "", "", false
	}
	if hasValue {
		return key, value, false
	}
	if index+1 < len(tokens) && !strings.HasPrefix(tokens[index+1], "--") {
		return key, tokens[index+1], true
	}
	return key, "true", false
}

func applyGreedyArgs(args []string, specs []CommandArgSpec) []string {
	out := append([]string(nil), args...)
	for i, spec := range specs {
		if !spec.Greedy {
			continue
		}
		if i >= len(out) {
			return out
		}
		greedy := strings.Join(out[i:], " ")
		out = append(out[:i], greedy)
		return out
	}
	return out
}
