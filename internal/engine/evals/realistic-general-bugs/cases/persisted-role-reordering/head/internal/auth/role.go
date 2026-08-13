package auth

type Role uint8

const (
	RoleAdministrator Role = iota
	RoleAuditor
	RoleViewer
	RoleUnknown
)

func (role Role) CanExport() bool {
	return role == RoleAdministrator
}

func (role Role) String() string {
	switch role {
	case RoleAdministrator:
		return "administrator"
	case RoleAuditor:
		return "auditor"
	case RoleViewer:
		return "viewer"
	default:
		return "unknown"
	}
}
