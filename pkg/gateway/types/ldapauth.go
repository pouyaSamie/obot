//nolint:revive
package types

import "time"

// LDAPAuthSession is a browser session issued by the built-in LDAP provider.
// ID is the SHA-256 hash of the browser token. Profile is encrypted by the
// gateway data-encryption mechanism before it is stored.
type LDAPAuthSession struct {
	ID                    string    `json:"-" gorm:"primaryKey"`
	CreatedAt             time.Time `json:"createdAt"`
	ExpiresAt             time.Time `json:"expiresAt" gorm:"index"`
	AuthProviderNamespace string    `json:"authProviderNamespace" gorm:"index"`
	AuthProviderName      string    `json:"authProviderName" gorm:"index"`
	ProviderUserID        string    `json:"providerUserID"`
	HashedProviderUserID  string    `json:"-" gorm:"index"`
	ConfigurationRevision string    `json:"-" gorm:"index"`
	Profile               string    `json:"-"`
	Encrypted             bool      `json:"-"`
}

// LDAPSyncPreview records an administrator-reviewed directory snapshot for a
// short period. Only a token hash is retained in the database.
type LDAPSyncPreview struct {
	ID                    string    `json:"-" gorm:"primaryKey"`
	CreatedAt             time.Time `json:"createdAt"`
	ExpiresAt             time.Time `json:"expiresAt" gorm:"index"`
	AuthProviderNamespace string    `json:"-" gorm:"index"`
	AuthProviderName      string    `json:"-" gorm:"index"`
	ConfigurationRevision string    `json:"-"`
	SnapshotHash          string    `json:"-"`
}
