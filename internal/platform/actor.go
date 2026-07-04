package platform

type ActorRole string

const (
	ActorRoleMember ActorRole = "member"
	ActorRoleAdmin  ActorRole = "admin"
	ActorRoleOwner  ActorRole = "owner"
)

type Actor struct {
	ID          string
	DisplayName string
	Role        ActorRole
	IsBot       bool
	Raw         map[string]any
}
