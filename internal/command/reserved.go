package command

import (
	"strings"
)

var reservedRootCommands = []string{
	"help",
	"sid",
	"new",
	"switch",
	"reset",
	"clear",
	"compact",
	"forget",
	"stop",
	"set",
	"unset",
	"plugin",
	"plugins",
	"provider",
	"model",
	"memory",
	"config",
	"admin",
}

var reservedRootSet = func() map[string]struct{} {
	out := make(map[string]struct{}, len(reservedRootCommands))
	for _, name := range reservedRootCommands {
		out[name] = struct{}{}
	}
	return out
}()

func IsReservedRoot(name string) bool {
	_, ok := reservedRootSet[normalizeRoot(name)]
	return ok
}

func normalizeRoot(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "/")
	return strings.ToLower(name)
}
