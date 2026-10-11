package model

import "time"

type Area struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Usage       *AreaUsage `json:"usage,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// AreaUsage counts what is in an area, so admins can see what a delete would affect.
type AreaUsage struct {
	Crews  int `json:"crews"`
	Agents int `json:"agents"`
	Tasks  int `json:"tasks"`
	Users  int `json:"users"`
}

type PermissionDef struct {
	Key         string `json:"key"`
	Feature     string `json:"feature"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type Role struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Builtin     bool      `json:"builtin"`
	Permissions []string  `json:"permissions"`
	UserCount   int       `json:"user_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

const (
	UserActive   = "active"
	UserDisabled = "disabled"
)

type LinkedIdentity struct {
	Provider    string    `json:"provider"`
	Email       string    `json:"email"`
	LastLoginAt time.Time `json:"last_login_at"`
}

type User struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Email    *string  `json:"email"`
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	AllAreas bool     `json:"all_areas"`
	AreaIDs  []string `json:"area_ids"`
	RoleIDs  []string `json:"role_ids"`
	// Permissions granted directly, on top of the roles
	Permissions []string `json:"permissions"`
	// EffectivePermissions is roles plus direct grants (read-only)
	EffectivePermissions []string         `json:"effective_permissions"`
	HasPassword          bool             `json:"has_password"`
	Identities           []LinkedIdentity `json:"identities"`
	LastLoginAt          *time.Time       `json:"last_login_at"`
	CreatedAt            time.Time        `json:"created_at"`
	UpdatedAt            time.Time        `json:"updated_at"`
}

// UserInput is what admins send to create or change a person or service account.
type UserInput struct {
	Kind        string   `json:"kind"`
	Email       string   `json:"email"`
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	AllAreas    bool     `json:"all_areas"`
	AreaIDs     []string `json:"area_ids"`
	RoleIDs     []string `json:"role_ids"`
	Permissions []string `json:"permissions"`
}

type APIKey struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

// AccountAPIKey is a key with the account it belongs to, as key lists return it.
type AccountAPIKey struct {
	APIKey
	UserName      string  `json:"user_name"`
	UserKind      string  `json:"user_kind"`
	CreatedByName *string `json:"created_by_name"`
	// CanRevoke says whether the caller may revoke the key (admins need everything its owner holds)
	CanRevoke bool `json:"can_revoke"`
}

type AccessSettings struct {
	SSOAutoProvision     bool     `json:"sso_auto_provision"`
	AutoProvisionRoleID  *string  `json:"auto_provision_role_id"`
	CallbackAllowedHosts []string `json:"callback_allowed_hosts"`
}

type AuditEvent struct {
	ID        int64          `json:"id"`
	Action    string         `json:"action"`
	ActorID   *string        `json:"actor_id"`
	ActorName string         `json:"actor_name"`
	IP        string         `json:"ip"`
	Details   map[string]any `json:"details"`
	CreatedAt time.Time      `json:"created_at"`
}
