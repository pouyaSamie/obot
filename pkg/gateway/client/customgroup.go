package client

import (
	"context"
	"fmt"
	"strings"

	"uuid"

	"github.com/obot-platform/obot/pkg/gateway/types"
	"github.com/obot-platform/obot/pkg/system"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const customGroupProviderName = "obot-custom-groups"

type CustomGroupRequest struct {
	Name string `json:"name"`
}

type CustomGroupMembersRequest struct {
	UserIDs []uint `json:"userIDs"`
}

func normalizeCustomGroupName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("group name is required")
	}
	if len([]rune(name)) > 120 {
		return "", fmt.Errorf("group name must be 120 characters or fewer")
	}
	return name, nil
}

func (c *Client) CreateCustomGroup(ctx context.Context, name string) (*types.Group, error) {
	name, err := normalizeCustomGroupName(name)
	if err != nil {
		return nil, err
	}
	group := &types.Group{
		ID:                    types.CustomGroupIDPrefix + uuid.New().String(),
		AuthProviderName:      customGroupProviderName,
		AuthProviderNamespace: system.DefaultNamespace,
		Name:                  name,
		Source:                types.GroupSourceCustom,
	}
	if err := c.db.WithContext(ctx).Create(group).Error; err != nil {
		return nil, fmt.Errorf("create custom group: %w", err)
	}
	return group, nil
}

func (c *Client) ListCustomGroups(ctx context.Context, nameFilter string) ([]types.Group, error) {
	query := c.db.WithContext(ctx).Model(&types.Group{}).
		Where("source = ? OR id LIKE ?", types.GroupSourceCustom, types.CustomGroupIDPrefix+"%")
	if nameFilter = strings.TrimSpace(nameFilter); nameFilter != "" {
		query = query.Where("LOWER(name) LIKE LOWER(?)", "%"+nameFilter+"%")
	}
	var groups []types.Group
	if err := query.Order("name, id").Find(&groups).Error; err != nil {
		return nil, fmt.Errorf("list custom groups: %w", err)
	}
	for i := range groups {
		groups[i].Source = types.GroupSourceCustom
		if err := c.db.WithContext(ctx).Model(&types.GroupMemberships{}).Where("group_id = ?", groups[i].ID).Count(&groups[i].MemberCount).Error; err != nil {
			return nil, fmt.Errorf("count custom group members: %w", err)
		}
	}
	return groups, nil
}

func (c *Client) CustomGroup(ctx context.Context, id string) (*types.Group, error) {
	var group types.Group
	if err := c.db.WithContext(ctx).Where("id = ?", id).
		Where("source = ? OR id LIKE ?", types.GroupSourceCustom, types.CustomGroupIDPrefix+"%").First(&group).Error; err != nil {
		return nil, err
	}
	group.Source = types.GroupSourceCustom
	if err := c.db.WithContext(ctx).Model(&types.GroupMemberships{}).Where("group_id = ?", group.ID).Count(&group.MemberCount).Error; err != nil {
		return nil, fmt.Errorf("count custom group members: %w", err)
	}
	return &group, nil
}

func (c *Client) UpdateCustomGroup(ctx context.Context, id, name string) (*types.Group, error) {
	name, err := normalizeCustomGroupName(name)
	if err != nil {
		return nil, err
	}
	group, err := c.CustomGroup(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := c.db.WithContext(ctx).Model(&types.Group{}).Where("id = ?", group.ID).Update("name", name).Error; err != nil {
		return nil, fmt.Errorf("rename custom group: %w", err)
	}
	group.Name = name
	return group, nil
}

func (c *Client) DeleteCustomGroup(ctx context.Context, id string) error {
	group, err := c.CustomGroup(ctx, id)
	if err != nil {
		return err
	}
	return c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", group.ID).Delete(&types.GroupMemberships{}).Error; err != nil {
			return fmt.Errorf("delete custom group memberships: %w", err)
		}
		if err := tx.Where("id = ?", group.ID).Delete(&types.Group{}).Error; err != nil {
			return fmt.Errorf("delete custom group: %w", err)
		}
		return nil
	})
}

func (c *Client) SetCustomGroupMembers(ctx context.Context, id string, userIDs []uint) (*types.Group, error) {
	group, err := c.CustomGroup(ctx, id)
	if err != nil {
		return nil, err
	}
	unique := make(map[uint]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID == 0 {
			return nil, fmt.Errorf("user ID is required")
		}
		unique[userID] = struct{}{}
	}
	ids := make([]uint, 0, len(unique))
	for userID := range unique {
		ids = append(ids, userID)
	}
	return group, c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(ids) > 0 {
			var count int64
			if err := tx.Model(&types.User{}).Where("id IN ? AND deleted_at IS NULL", ids).Count(&count).Error; err != nil {
				return fmt.Errorf("validate group users: %w", err)
			}
			if count != int64(len(ids)) {
				return fmt.Errorf("one or more selected users do not exist")
			}
		}
		if err := tx.Where("group_id = ?", group.ID).Delete(&types.GroupMemberships{}).Error; err != nil {
			return fmt.Errorf("clear custom group members: %w", err)
		}
		memberships := make([]types.GroupMemberships, 0, len(ids))
		for _, userID := range ids {
			memberships = append(memberships, types.GroupMemberships{UserID: userID, GroupID: group.ID})
		}
		if len(memberships) > 0 {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&memberships).Error; err != nil {
				return fmt.Errorf("add custom group members: %w", err)
			}
		}
		return nil
	})
}

func (c *Client) ListCustomAndSystemGroupIDs(ctx context.Context, userID uint) ([]string, error) {
	var ids []string
	if err := c.db.WithContext(ctx).Table("group_memberships").
		Joins("JOIN groups ON groups.id = group_memberships.group_id").
		Where("group_memberships.user_id = ?", userID).
		Where("groups.source = ? OR groups.id LIKE ?", types.GroupSourceCustom, types.CustomGroupIDPrefix+"%").
		Pluck("groups.id", &ids).Error; err != nil {
		return nil, fmt.Errorf("list custom group IDs: %w", err)
	}
	var activeLDAP int64
	if err := c.db.WithContext(ctx).Model(&types.Identity{}).
		Where("user_id = ? AND auth_provider_name = ? AND disabled_at IS NULL", userID, system.LDAPAuthProvider).
		Count(&activeLDAP).Error; err != nil {
		return nil, fmt.Errorf("check LDAP identity: %w", err)
	}
	if activeLDAP > 0 {
		ids = append(ids, types.LDAPUsersGroupID)
	}
	return ids, nil
}
func (c *Client) ListCustomGroupMemberIDs(ctx context.Context, groupID string) ([]uint, error) {
	var userIDs []uint
	if err := c.db.WithContext(ctx).Model(&types.GroupMemberships{}).Where("group_id = ?", groupID).Order("user_id").Pluck("user_id", &userIDs).Error; err != nil {
		return nil, fmt.Errorf("list custom group member IDs: %w", err)
	}
	return userIDs, nil
}