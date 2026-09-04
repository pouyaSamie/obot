package client

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/obot-platform/obot/pkg/gateway/types"
	"github.com/obot-platform/obot/pkg/hash"
	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/storage/value"
)

var ldapAuthSessionResource = schema.GroupResource{Group: "obot.obot.ai", Resource: "ldapauthsessions"}

// CreateLDAPAuthSession stores a token-hash keyed LDAP browser session.
func (c *Client) CreateLDAPAuthSession(ctx context.Context, id, namespace, name, providerUserID, revision, profile string, expiresAt time.Time) error {
	session := types.LDAPAuthSession{
		ID: id, AuthProviderNamespace: namespace, AuthProviderName: name,
		ProviderUserID: providerUserID, HashedProviderUserID: hash.String(providerUserID),
		ConfigurationRevision: revision, Profile: profile, ExpiresAt: expiresAt,
	}
	if err := c.encryptLDAPAuthSession(ctx, &session); err != nil {
		return err
	}
	return c.db.WithContext(ctx).Create(&session).Error
}

// LDAPAuthSession returns an active session only if it was issued for the
// current LDAP configuration and its identity has not been disabled by sync.
func (c *Client) LDAPAuthSession(ctx context.Context, id, revision string) (*types.LDAPAuthSession, error) {
	var session types.LDAPAuthSession
	if err := c.db.WithContext(ctx).Where("id = ?", id).First(&session).Error; err != nil {
		return nil, err
	}
	if !session.ExpiresAt.After(time.Now()) || session.ConfigurationRevision != revision {
		_ = c.DeleteLDAPAuthSession(ctx, id)
		return nil, gorm.ErrRecordNotFound
	}
	var identity types.Identity
	if err := c.db.WithContext(ctx).Where("auth_provider_namespace = ? AND auth_provider_name = ? AND hashed_provider_user_id = ?", session.AuthProviderNamespace, session.AuthProviderName, session.HashedProviderUserID).First(&identity).Error; err == nil && identity.DisabledAt != nil {
		_ = c.DeleteLDAPAuthSession(ctx, id)
		return nil, gorm.ErrRecordNotFound
	} else if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	if err := c.decryptLDAPAuthSession(ctx, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (c *Client) LDAPIdentityActive(ctx context.Context, namespace, name, providerUserID string) (bool, error) {
	var identity types.Identity
	err := c.db.WithContext(ctx).Where("auth_provider_namespace = ? AND auth_provider_name = ? AND hashed_provider_user_id = ?", namespace, name, hash.String(providerUserID)).First(&identity).Error
	if err == gorm.ErrRecordNotFound {
		// First interactive LDAP login creates the identity after this provider
		// has returned its state, so absence is not a disabled identity.
		return true, nil
	}
	return err == nil && identity.DisabledAt == nil, err
}

func (c *Client) DeleteLDAPAuthSession(ctx context.Context, id string) error {
	return c.db.WithContext(ctx).Where("id = ?", id).Delete(new(types.LDAPAuthSession)).Error
}

func (c *Client) DeleteLDAPAuthSessionsForIdentity(ctx context.Context, namespace, name, providerUserID string) error {
	return c.db.WithContext(ctx).Where("auth_provider_namespace = ? AND auth_provider_name = ? AND hashed_provider_user_id = ?", namespace, name, hash.String(providerUserID)).Delete(new(types.LDAPAuthSession)).Error
}

func (c *Client) DeleteLDAPAuthSessionsForHashedIdentity(ctx context.Context, namespace, name, hashedProviderUserID string) error {
	return c.db.WithContext(ctx).Where("auth_provider_namespace = ? AND auth_provider_name = ? AND hashed_provider_user_id = ?", namespace, name, hashedProviderUserID).Delete(new(types.LDAPAuthSession)).Error
}

func (c *Client) DeleteAllLDAPAuthSessions(ctx context.Context) error {
	return c.db.WithContext(ctx).Where("1 = 1").Delete(new(types.LDAPAuthSession)).Error
}

func (c *Client) DeleteExpiredLDAPAuthSessions(ctx context.Context) error {
	return c.db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(new(types.LDAPAuthSession)).Error
}

func (c *Client) CreateLDAPSyncPreview(ctx context.Context, token, namespace, name, revision, snapshotHash string) error {
	preview := types.LDAPSyncPreview{ID: hash.String(token), AuthProviderNamespace: namespace, AuthProviderName: name, ConfigurationRevision: revision, SnapshotHash: snapshotHash, ExpiresAt: time.Now().Add(10 * time.Minute)}
	return c.db.WithContext(ctx).Create(&preview).Error
}

func (c *Client) LDAPSyncPreviewValid(ctx context.Context, token, namespace, name, revision, snapshotHash string) (bool, error) {
	var preview types.LDAPSyncPreview
	err := c.db.WithContext(ctx).Where("id = ? AND auth_provider_namespace = ? AND auth_provider_name = ?", hash.String(token), namespace, name).First(&preview).Error
	if err == gorm.ErrRecordNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return preview.ExpiresAt.After(time.Now()) && preview.ConfigurationRevision == revision && preview.SnapshotHash == snapshotHash, nil
}

func (c *Client) DeleteLDAPSyncPreview(ctx context.Context, token string) error {
	return c.db.WithContext(ctx).Where("id = ?", hash.String(token)).Delete(new(types.LDAPSyncPreview)).Error
}

func ldapAuthSessionDataCtx(session *types.LDAPAuthSession) value.Context {
	return value.DefaultContext(fmt.Sprintf("%s/%s", ldapAuthSessionResource.String(), session.ID))
}

func (c *Client) encryptLDAPAuthSession(ctx context.Context, session *types.LDAPAuthSession) error {
	if c.encryptionConfig == nil || c.encryptionConfig.Transformers[ldapAuthSessionResource] == nil {
		return nil
	}
	b, err := c.encryptionConfig.Transformers[ldapAuthSessionResource].TransformToStorage(ctx, []byte(session.Profile), ldapAuthSessionDataCtx(session))
	if err != nil {
		return err
	}
	session.Profile, session.Encrypted = base64.StdEncoding.EncodeToString(b), true
	return nil
}

func (c *Client) decryptLDAPAuthSession(ctx context.Context, session *types.LDAPAuthSession) error {
	if !session.Encrypted || c.encryptionConfig == nil || c.encryptionConfig.Transformers[ldapAuthSessionResource] == nil {
		return nil
	}
	b, err := base64.StdEncoding.DecodeString(session.Profile)
	if err != nil {
		return err
	}
	plain, _, err := c.encryptionConfig.Transformers[ldapAuthSessionResource].TransformFromStorage(ctx, b, ldapAuthSessionDataCtx(session))
	if err != nil {
		return err
	}
	session.Profile = string(plain)
	session.Encrypted = false
	return nil
}
