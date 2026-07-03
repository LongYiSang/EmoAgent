package command

import "fmt"

type CommandActor struct {
	ID   string
	Role CommandPermission
}

type PermissionChecker struct{}

func NewPermissionChecker() PermissionChecker {
	return PermissionChecker{}
}

func (PermissionChecker) Check(actor CommandActor, required CommandPermission) error {
	if required == "" {
		required = CommandPermissionMember
	}
	if actor.Role == "" {
		actor.Role = CommandPermissionMember
	}
	if permissionRank(actor.Role) < permissionRank(required) {
		return fmt.Errorf("command requires %s permission", required)
	}
	return nil
}

func permissionRank(permission CommandPermission) int {
	switch permission {
	case CommandPermissionEveryone:
		return 0
	case CommandPermissionMember:
		return 1
	case CommandPermissionAdmin:
		return 2
	case CommandPermissionOwner:
		return 3
	default:
		return -1
	}
}
