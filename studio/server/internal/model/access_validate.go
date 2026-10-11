package model

import (
	"net/mail"
	"strings"
)

const maxHostsLen = 50

// NormalizeArea validates a functional area.
func NormalizeArea(in Area) (Area, error) {
	out := Area{ID: in.ID, Name: strings.TrimSpace(in.Name), Description: strings.TrimSpace(in.Description)}
	var c checker
	c.text("name", out.Name, 1, maxNameLen)
	c.text("description", out.Description, 0, maxShortTextLen)
	return out, c.err()
}

// NormalizeRole validates a role against the permission catalog.
func NormalizeRole(in Role, known func(string) bool) (Role, error) {
	out := Role{ID: in.ID, Name: strings.TrimSpace(in.Name), Description: strings.TrimSpace(in.Description)}
	var c checker
	c.text("name", out.Name, 1, maxNameLen)
	c.text("description", out.Description, 0, maxShortTextLen)
	out.Permissions = normalizePermissions(&c, in.Permissions, known)
	return out, c.err()
}

func normalizePermissions(c *checker, perms []string, known func(string) bool) []string {
	out := make([]string, 0, len(perms))
	for _, p := range perms {
		p = strings.TrimSpace(p)
		switch {
		case !known(p):
			c.addf("permissions", "unknown permission %q", p)
		case !contains(out, p):
			out = append(out, p)
		}
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// NormalizeUser validates a person or service account.
func NormalizeUser(in UserInput, known func(string) bool) (UserInput, error) {
	out := in
	out.Name = strings.TrimSpace(in.Name)
	out.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if out.Status == "" {
		out.Status = UserActive
	}
	var c checker
	switch out.Kind {
	case "human":
		if addr, err := mail.ParseAddress(out.Email); err != nil || addr.Address != out.Email || len(out.Email) > 254 {
			c.addf("email", "must be a valid email address")
		}
		if out.Name == "" {
			out.Name = out.Email
		}
	case "service":
		out.Email = ""
	default:
		c.addf("kind", `must be "human" or "service"`)
	}
	c.text("name", out.Name, 1, maxNameLen)
	if out.Status != UserActive && out.Status != UserDisabled {
		c.addf("status", "must be %q or %q", UserActive, UserDisabled)
	}
	out.AreaIDs = c.uuidList("area_ids", in.AreaIDs, "")
	out.RoleIDs = c.uuidList("role_ids", in.RoleIDs, "")
	out.Permissions = normalizePermissions(&c, in.Permissions, known)
	return out, c.err()
}

// NormalizeAccessSettings validates sign-in settings.
func NormalizeAccessSettings(in AccessSettings) (AccessSettings, error) {
	out := in
	out.AutoProvisionRoleID = trimmedPtr(in.AutoProvisionRoleID)
	var c checker
	c.optionalUUID("auto_provision_role_id", out.AutoProvisionRoleID)
	if len(in.CallbackAllowedHosts) > maxHostsLen {
		c.addf("callback_allowed_hosts", "at most %d hosts", maxHostsLen)
	}
	out.CallbackAllowedHosts = []string{}
	for _, h := range in.CallbackAllowedHosts {
		h = strings.ToLower(strings.TrimSpace(h))
		switch {
		case h == "":
		case len(h) > 253 || strings.ContainsAny(h, "/:@ *?#"):
			c.addf("callback_allowed_hosts", "%q must be a bare host name or IP address", h)
		case !contains(out.CallbackAllowedHosts, h):
			out.CallbackAllowedHosts = append(out.CallbackAllowedHosts, h)
		}
	}
	return out, c.err()
}
