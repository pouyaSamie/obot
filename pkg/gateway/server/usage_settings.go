package server

import (
	"errors"
	"net/http"
	"strconv"

	types2 "github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/api"
	"github.com/obot-platform/obot/pkg/gateway/client"
	"gorm.io/gorm"
)

// usageSettings exposes the persisted organization-wide combined daily token
// limit. The environment value remains the initial value until this endpoint
// has been used at least once.
func (s *Server) usageSettings(req api.Context) error {
	limit, err := req.GatewayClient.DailyUserTotalTokenLimit(req.Context(), s.dailyUserTotalTokenLimit)
	if err != nil {
		return err
	}
	return req.Write(types2.UsageSettings{DailyUserTotalTokenLimit: limit})
}

func (s *Server) updateUsageSettings(req api.Context) error {
	var settings types2.UsageSettings
	if err := req.Read(&settings); err != nil {
		return types2.NewErrHTTP(http.StatusBadRequest, "invalid usage settings request body")
	}
	if settings.DailyUserTotalTokenLimit == 0 || settings.DailyUserTotalTokenLimit < -1 {
		return types2.NewErrHTTP(http.StatusBadRequest, "dailyUserTotalTokenLimit must be positive or -1")
	}
	if _, err := req.GatewayClient.SetProperty(req.Context(), client.DailyUserTotalTokenLimitPropertyKey, strconv.Itoa(settings.DailyUserTotalTokenLimit)); err != nil {
		return err
	}
	return req.Write(settings)
}

type dailyTotalTokenLimitRequest struct {
	DailyTotalTokensLimit int `json:"dailyTotalTokensLimit"`
}

func (s *Server) updateUserDailyTotalTokenLimit(req api.Context) error {
	userID := req.PathValue("user_id")
	var input dailyTotalTokenLimitRequest
	if err := req.Read(&input); err != nil {
		return types2.NewErrHTTP(http.StatusBadRequest, "invalid daily total token limit request body")
	}
	// 0 means inherit, positives are custom, and negatives are unlimited.
	if err := req.GatewayClient.UpdateDailyTotalTokensLimit(req.Context(), userID, input.DailyTotalTokensLimit); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types2.NewErrNotFound("user %s not found", userID)
		}
		return err
	}
	return s.getUser(req)
}
