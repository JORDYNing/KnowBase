package types

// KBPermission is the permission granted to one knowledge-base operation.
type KBPermission string

const (
	KBPermissionRead  KBPermission = "viewer"
	KBPermissionWrite KBPermission = "editor"
	KBPermissionAdmin KBPermission = "admin"
)

func (p KBPermission) IsValid() bool {
	return p == KBPermissionRead || p == KBPermissionWrite || p == KBPermissionAdmin
}
func (p KBPermission) HasPermission(required KBPermission) bool {
	levels := map[KBPermission]int{KBPermissionRead: 1, KBPermissionWrite: 2, KBPermissionAdmin: 3}
	return p.IsValid() && required.IsValid() && levels[p] >= levels[required]
}
