//nolint:revive
package types

import (
	"strings"
	"time"
)

const (
	// GroupSourceProvider means the listing came from the auth provider and is complete.
	GroupSourceProvider GroupSource = "provider"

	// GroupSourceCache means the listing came from the groups table, which holds the groups
	// observed during a user sign-in plus any resolved by ID for a policy, and is therefore
	// partial.
	GroupSourceCache GroupSource = "cache"

	// GroupSourceCustom identifies a group created and maintained in Obot.
	GroupSourceCustom GroupSource = "custom"

	// GroupSourceSystem identifies a protected computed group.
	GroupSourceSystem GroupSource = "system"

	// CustomGroupIDPrefix keeps administrator-managed groups isolated from groups
	// owned by identity providers.
	CustomGroupIDPrefix = "custom/"
	// LDAPUsersGroupID is a computed group containing active LDAP users.
	LDAPUsersGroupID = "system/ldap-users"
)

// Group represents a group that users can belong to in an auth provider or Obot.
type Group struct {
	// ID is the globally unique identifier for the group.
	ID string `json:"id" gorm:"primaryKey;unique;index:idx_group_auth_provider_name,priority:4"`

	// AuthProviderName and AuthProviderNamespace identify provider-owned groups.
	// Obot-managed groups use the internal "obot" provider identity.
	AuthProviderName string `json:"authProviderName" gorm:"primaryKey;index:idx_group_auth_provider;index:idx_group_auth_provider_name,priority:1"`
	AuthProviderNamespace string `json:"authProviderNamespace" gorm:"primaryKey;index:idx_group_auth_provider;index:idx_group_auth_provider_name,priority:2"`

	Name string `json:"name" gorm:"index:idx_group_auth_provider_name,priority:3"`
	IconURL *string `json:"iconURL"`

	// Existing rows without a source are provider-owned.
	Source GroupSource `json:"source,omitempty" gorm:"default:provider;index"`
	MemberCount int64 `json:"memberCount,omitempty" gorm:"-"`
}

func (g Group) IsCustom() bool {
	return g.Source == GroupSourceCustom || strings.HasPrefix(g.ID, CustomGroupIDPrefix)
}

// GroupSource names where a group listing came from.
type GroupSource string

// GroupListResponse is the paginated response for a group listing.
type GroupListResponse struct {
	Items []Group `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
	Source GroupSource `json:"source"`
	Degraded bool `json:"degraded"`
	Reset bool `json:"reset,omitempty"`
}

// GroupMemberships represents a user's membership in a group.
type GroupMemberships struct {
	UserID uint `json:"userID" gorm:"primaryKey"`
	GroupID string `json:"groupID" gorm:"primaryKey"`
	CreatedAt time.Time `json:"createdAt" gorm:"autoCreateTime"`
}