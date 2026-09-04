package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	types2 "github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/gateway/types"
	"github.com/obot-platform/obot/pkg/hash"
)

// LDAPSyncUser is the credential-free directory record passed from the LDAP
// provider to the gateway. It intentionally mirrors only identity attributes.
type LDAPSyncUser struct {
	ID, Username, Email, DisplayName string
	Groups                           []string
}

type LDAPSyncSummary struct {
	Created, Updated, Linked, Disabled, Unchanged, Skipped, Conflicts int
	Issues                                                       []string
	SnapshotHash                                                 string
	PreviewToken                                                 string
}

func (c *Client) PreviewLDAPSync(ctx context.Context, providerNamespace, providerName string, entries []LDAPSyncUser) (LDAPSyncSummary, error) {
	valid, summary := normalizeLDAPSyncUsers(entries)
	known, err := c.ldapIdentities(ctx, providerNamespace, providerName)
	if err != nil {
		return summary, err
	}
	seen := make(map[string]struct{}, len(valid))
	for _, entry := range valid {
		key := hash.String(entry.ID)
		seen[key] = struct{}{}
		if _, ok := known[key]; ok {
			summary.Updated++
			continue
		}
		users, err := c.Users(ctx, types.UserQuery{Email: entry.Email})
		if err != nil {
			return summary, err
		}
		if len(users) > 0 {
			identities, err := c.FindIdentitiesForUser(ctx, users[0].ID)
			if err != nil {
				return summary, err
			}
			for _, identity := range identities {
				if identity.AuthProviderNamespace == providerNamespace && identity.AuthProviderName == providerName && identity.HashedProviderUserID != key {
					summary.Conflicts++
					summary.Issues = appendLDAPIssue(summary.Issues, "LDAP email is already linked to a different stable ID: "+entry.Email)
					goto nextEntry
				}
			}
			summary.Linked++
		} else {
			summary.Created++
		}
	nextEntry:
	}
	for key := range known {
		if _, ok := seen[key]; !ok {
			summary.Disabled++
		}
	}
	summary.Unchanged = summary.Updated
	summary.SnapshotHash = ldapSnapshotHash(valid)
	return summary, nil
}

// ApplyLDAPSync is idempotent. Callers must run PreviewLDAPSync first and
// compare the returned hash with a freshly enumerated directory snapshot.
func (c *Client) ApplyLDAPSync(ctx context.Context, providerNamespace, providerName string, entries []LDAPSyncUser) (LDAPSyncSummary, error) {
	summary, err := c.PreviewLDAPSync(ctx, providerNamespace, providerName, entries)
	if err != nil || summary.Conflicts > 0 {
		if summary.Conflicts > 0 {
			return summary, fmt.Errorf("LDAP synchronization has conflicts")
		}
		return summary, err
	}
	valid, _ := normalizeLDAPSyncUsers(entries)
	for _, entry := range valid {
		// Capture a matching user before EnsureIdentity so a linked local account
		// retains its username, profile, role and limits.
		existing, err := c.Users(ctx, types.UserQuery{Email: entry.Email})
		if err != nil {
			return summary, err
		}
		identity := &types.Identity{
			AuthProviderName: providerName, AuthProviderNamespace: providerNamespace,
			ProviderUsername: entry.Username, ProviderUserID: entry.ID, Email: entry.Email,
		}
		user, err := c.EnsureIdentityWithRole(ctx, identity, "", types2.RoleUnknown, UserLimit{Unlimited: true})
		if err != nil {
			return summary, err
		}
		if err := c.db.WithContext(ctx).Model(new(types.Identity)).
			Where("auth_provider_namespace = ? AND auth_provider_name = ? AND hashed_provider_user_id = ?", providerNamespace, providerName, hash.String(entry.ID)).
			Update("disabled_at", nil).Error; err != nil {
			return summary, err
		}
		if err := c.setLDAPUserEnabled(ctx, user.ID); err != nil {
			return summary, err
		}
		if len(existing) > 0 {
			if err := c.restoreLDAPLinkedUser(ctx, user.ID, existing[0]); err != nil {
				return summary, err
			}
		} else if err := c.updateLDAPDisplayName(ctx, user.ID, entry.DisplayName); err != nil {
			return summary, err
		}
		if err := c.persistLDAPGroups(ctx, identity, entry.Groups); err != nil {
			return summary, err
		}
	}

	known, err := c.ldapIdentities(ctx, providerNamespace, providerName)
	if err != nil {
		return summary, err
	}
	seen := make(map[string]struct{}, len(valid))
	for _, entry := range valid {
		seen[hash.String(entry.ID)] = struct{}{}
	}
	if len(known) > 0 {
		missing := make([]string, 0)
		for key := range known {
			if _, ok := seen[key]; !ok {
				missing = append(missing, key)
			}
		}
		if len(missing) > 0 {
			now := time.Now().UTC()
			if err := c.db.WithContext(ctx).Model(new(types.Identity)).
				Where("auth_provider_namespace = ? AND auth_provider_name = ? AND hashed_provider_user_id IN ?", providerNamespace, providerName, missing).
				Update("disabled_at", &now).Error; err != nil {
				return summary, err
			}
			for _, key := range missing {
				if err := c.DeleteLDAPAuthSessionsForHashedIdentity(ctx, providerNamespace, providerName, key); err != nil {
					return summary, err
				}
			}
			for _, key := range missing {
				if identity, ok := known[key]; ok {
					if err := c.recomputeLDAPUserDisabled(ctx, identity.UserID); err != nil {
						return summary, err
					}
				}
			}
		}
	}
	return summary, nil
}

func (c *Client) setLDAPUserEnabled(ctx context.Context, userID uint) error {
	return c.db.WithContext(ctx).Model(new(types.User)).Where("id = ?", userID).Update("disabled_at", nil).Error
}

func (c *Client) recomputeLDAPUserDisabled(ctx context.Context, userID uint) error {
	var active int64
	if err := c.db.WithContext(ctx).Model(new(types.Identity)).Where("user_id = ? AND disabled_at IS NULL", userID).Count(&active).Error; err != nil {
		return err
	}
	if active > 0 {
		return nil
	}
	now := time.Now().UTC()
	return c.db.WithContext(ctx).Model(new(types.User)).Where("id = ? AND disabled_at IS NULL", userID).Update("disabled_at", &now).Error
}

func (c *Client) persistLDAPGroups(ctx context.Context, identity *types.Identity, groupIDs []string) error {
	identity.AuthProviderGroups = make([]types.Group, 0, len(groupIDs))
	seen := make(map[string]struct{}, len(groupIDs))
	for _, id := range groupIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		identity.AuthProviderGroups = append(identity.AuthProviderGroups, types.Group{
			ID: id, Name: strings.TrimPrefix(id, "ldap/"),
			AuthProviderName: identity.AuthProviderName, AuthProviderNamespace: identity.AuthProviderNamespace,
		})
	}
	return c.persistGroups(ctx, identity)
}

func (c *Client) ldapIdentities(ctx context.Context, namespace, name string) (map[string]types.Identity, error) {
	var identities []types.Identity
	if err := c.db.WithContext(ctx).Where("auth_provider_namespace = ? AND auth_provider_name = ?", namespace, name).Find(&identities).Error; err != nil {
		return nil, err
	}
	result := make(map[string]types.Identity, len(identities))
	for _, identity := range identities {
		result[identity.HashedProviderUserID] = identity
	}
	return result, nil
}

func normalizeLDAPSyncUsers(entries []LDAPSyncUser) ([]LDAPSyncUser, LDAPSyncSummary) {
	byID, byEmail := map[string]struct{}{}, map[string]struct{}{}
	valid := make([]LDAPSyncUser, 0, len(entries))
	summary := LDAPSyncSummary{}
	for _, entry := range entries {
		entry.ID = strings.TrimSpace(entry.ID)
		entry.Email = NormalizeEmail(entry.Email)
		entry.Username = strings.TrimSpace(entry.Username)
		if entry.Username == "" {
			entry.Username = entry.ID
		}
		if entry.ID == "" || entry.Email == "" {
			summary.Skipped++
			summary.Issues = appendLDAPIssue(summary.Issues, "LDAP entry is missing its stable ID or email")
			continue
		}
		if _, ok := byID[entry.ID]; ok {
			summary.Conflicts++
			summary.Issues = appendLDAPIssue(summary.Issues, "duplicate LDAP stable ID: "+entry.ID)
			continue
		}
		if _, ok := byEmail[entry.Email]; ok {
			summary.Conflicts++
			summary.Issues = appendLDAPIssue(summary.Issues, "duplicate LDAP email: "+entry.Email)
			continue
		}
		byID[entry.ID], byEmail[entry.Email] = struct{}{}, struct{}{}
		valid = append(valid, entry)
	}
	return valid, summary
}

func appendLDAPIssue(issues []string, issue string) []string {
	const maxIssues = 100
	if len(issues) >= maxIssues {
		return issues
	}
	return append(issues, issue)
}

func ldapSnapshotHash(entries []LDAPSyncUser) string {
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		lines = append(lines, entry.ID+"\x00"+entry.Email+"\x00"+entry.Username+"\x00"+entry.DisplayName)
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

func (c *Client) updateLDAPDisplayName(ctx context.Context, userID uint, displayName string) error {
	if displayName == "" {
		return nil
	}
	user, err := c.UserByID(ctx, fmt.Sprint(userID))
	if err != nil {
		return err
	}
	user.DisplayName = displayName
	u := *user
	if err := c.encryptUser(ctx, &u); err != nil {
		return err
	}
	return c.db.WithContext(ctx).Model(new(types.User)).Where("id = ?", userID).Updates(&u).Error
}

func (c *Client) restoreLDAPLinkedUser(ctx context.Context, userID uint, original types.User) error {
	current, err := c.UserByID(ctx, fmt.Sprint(userID))
	if err != nil {
		return err
	}
	current.Username, current.Email, current.DisplayName = original.Username, original.Email, original.DisplayName
	current.HashedUsername, current.HashedEmail = hash.String(current.Username), hash.String(current.Email)
	u := *current
	if err := c.encryptUser(ctx, &u); err != nil {
		return err
	}
	return c.db.WithContext(ctx).Model(new(types.User)).Where("id = ?", userID).Updates(&u).Error
}
