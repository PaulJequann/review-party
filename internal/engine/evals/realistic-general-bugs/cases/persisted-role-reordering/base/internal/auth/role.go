package auth

type Role uint8

const (
	RoleUnknown Role = iota
	RoleViewer
	RoleAdministrator
)

func (role Role) CanExport() bool {
	return role == RoleAdministrator
}

func (role Role) String() string {
	switch role {
	case RoleViewer:
		return "viewer"
	case RoleAdministrator:
		return "administrator"
	default:
		return "unknown"
	}
}
