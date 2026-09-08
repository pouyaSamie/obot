package server

import (
	"errors"
	"fmt"
	"strings"

	types2 "github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/api"
	"github.com/obot-platform/obot/pkg/gateway/client"
	"github.com/obot-platform/obot/pkg/gateway/types"
	"gorm.io/gorm"
)

type customGroupResponse struct {
	types.Group
	MemberUserIDs []uint `json:"memberUserIDs,omitempty"`
}

func customGroupOutput(group *types.Group, memberIDs []uint) customGroupResponse {
	group.Source = types.GroupSourceCustom
	return customGroupResponse{Group: *group, MemberUserIDs: memberIDs}
}

func (s *Server) listCustomGroups(apiContext api.Context) error {
	groups, err := apiContext.GatewayClient.ListCustomGroups(apiContext.Context(), apiContext.URL.Query().Get("name"))
	if err != nil {
		return fmt.Errorf("list custom groups: %w", err)
	}
	return apiContext.Write(struct {
		Items []types.Group `json:"items"`
	}{Items: groups})
}

func (s *Server) createCustomGroup(apiContext api.Context) error {
	var request client.CustomGroupRequest
	if err := apiContext.Read(&request); err != nil {
		return types2.NewErrBadRequest("invalid custom group: %v", err)
	}
	group, err := apiContext.GatewayClient.CreateCustomGroup(apiContext.Context(), request.Name)
	if err != nil {
		return types2.NewErrBadRequest("create custom group: %v", err)
	}
	return apiContext.Write(customGroupOutput(group, nil))
}

func (s *Server) getCustomGroup(apiContext api.Context) error {
	group, err := apiContext.GatewayClient.CustomGroup(apiContext.Context(), apiContext.PathValue("id"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return types2.NewErrNotFound("custom group %s not found", apiContext.PathValue("id"))
	}
	if err != nil {
		return fmt.Errorf("get custom group: %w", err)
	}
	memberIDs, err := apiContext.GatewayClient.ListCustomGroupMemberIDs(apiContext.Context(), group.ID)
	if err != nil {
		return fmt.Errorf("list custom group members: %w", err)
	}
	return apiContext.Write(customGroupOutput(group, memberIDs))
}

func (s *Server) updateCustomGroup(apiContext api.Context) error {
	var request client.CustomGroupRequest
	if err := apiContext.Read(&request); err != nil {
		return types2.NewErrBadRequest("invalid custom group: %v", err)
	}
	group, err := apiContext.GatewayClient.UpdateCustomGroup(apiContext.Context(), apiContext.PathValue("id"), request.Name)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return types2.NewErrNotFound("custom group %s not found", apiContext.PathValue("id"))
	}
	if err != nil {
		return types2.NewErrBadRequest("update custom group: %v", err)
	}
	return apiContext.Write(customGroupOutput(group, nil))
}

func (s *Server) deleteCustomGroup(apiContext api.Context) error {
	id := apiContext.PathValue("id")
	if id == types.LDAPUsersGroupID || !strings.HasPrefix(id, types.CustomGroupIDPrefix) {
		return types2.NewErrBadRequest("only Obot-managed custom groups can be deleted")
	}
	if err := apiContext.GatewayClient.DeleteCustomGroup(apiContext.Context(), id); errors.Is(err, gorm.ErrRecordNotFound) {
		return types2.NewErrNotFound("custom group %s not found", id)
	} else if err != nil {
		return fmt.Errorf("delete custom group: %w", err)
	}
	return apiContext.Write(struct{}{})
}

func (s *Server) setCustomGroupMembers(apiContext api.Context) error {
	var request client.CustomGroupMembersRequest
	if err := apiContext.Read(&request); err != nil {
		return types2.NewErrBadRequest("invalid custom group members: %v", err)
	}
	group, err := apiContext.GatewayClient.SetCustomGroupMembers(apiContext.Context(), apiContext.PathValue("id"), request.UserIDs)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return types2.NewErrNotFound("custom group %s not found", apiContext.PathValue("id"))
	}
	if err != nil {
		return types2.NewErrBadRequest("update custom group members: %v", err)
	}
	members, err := apiContext.GatewayClient.ListCustomGroupMemberIDs(apiContext.Context(), group.ID)
	if err != nil {
		return fmt.Errorf("list custom group members: %w", err)
	}
	return apiContext.Write(customGroupOutput(group, members))
}