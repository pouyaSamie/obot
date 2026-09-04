package client

import (
	"math"
	"testing"
	"time"

	"github.com/obot-platform/obot/pkg/gateway/types"
)

func TestEffectiveTokenLimit(t *testing.T) {
	for _, tt := range []struct {
		name               string
		server, user, want int
		unlimited          bool
	}{
		{"inherits organization", 1000, 0, 1000, false},
		{"custom user limit", 1000, 250, 250, false},
		{"user unlimited", 1000, -1, 0, true},
		{"organization unlimited", -1, 0, 0, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, unlimited := effectiveTokenLimit(tt.server, tt.user)
			if got != tt.want || unlimited != tt.unlimited {
				t.Fatalf("effectiveTokenLimit(%d, %d) = (%d, %t), want (%d, %t)", tt.server, tt.user, got, unlimited, tt.want, tt.unlimited)
			}
		})
	}
}

// TestTokenActivityRoundTrip verifies TokenUsage persistence and aggregation.
func TestTokenActivityRoundTrip(t *testing.T) {
	c := newTestClient(t)
	ctx := t.Context()
	now := time.Now()
	apiKeyID := uint(42)
	if !c.db.WithContext(ctx).Migrator().HasIndex(&types.RunTokenActivity{}, "idx_run_token_activity_api_key_created") {
		t.Fatal("expected API key/date token usage index")
	}

	rows := []types.RunTokenActivity{
		{
			UserID:     "u1",
			Model:      "claude-opus-4-5",
			CreatedAt:  now,
			APIKeyID:   &apiKeyID,
			APIKeyName: "CLI token",
			Usage: types.TokenUsage{
				InputTokens:      100050,
				CacheReadTokens:  100000,
				CacheWriteTokens: 500,
				OutputTokens:     800,
				ThinkingTokens:   600,
				TotalTokens:      100050 + 500 + 800,
				InputSpend:       0.00025,
				CacheReadSpend:   0.05,
				CacheWriteSpend:  0.0,
				OutputSpend:      0.02,
				TotalSpend:       0.07025,
			},
		},
		{
			UserID:    "u1",
			Model:     "gpt-5",
			CreatedAt: now,
			Usage: types.TokenUsage{
				InputTokens:  2006,
				OutputTokens: 300,
				TotalTokens:  2306,
				TotalSpend:   0.0033475,
			},
		},
	}
	for i := range rows {
		if err := c.InsertTokenUsage(ctx, &rows[i]); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	got, err := c.TokenUsageForUser(ctx, "u1", now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("TokenUsageForUser: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}
	var opus types.RunTokenActivity
	for _, r := range got {
		if r.Model == "claude-opus-4-5" {
			opus = r
		}
	}
	if opus.Usage.CacheReadTokens != 100000 || opus.Usage.CacheWriteTokens != 500 {
		t.Errorf("cache buckets wrong: %+v", opus.Usage)
	}
	if opus.Usage.ThinkingTokens != 600 {
		t.Errorf("thinking = %d, want 600", opus.Usage.ThinkingTokens)
	}
	if math.Abs(opus.Usage.TotalSpend-0.07025) > 1e-9 {
		t.Errorf("total spend = %v, want 0.07025", opus.Usage.TotalSpend)
	}
	if opus.APIKeyID == nil || *opus.APIKeyID != apiKeyID || opus.APIKeyName != "CLI token" {
		t.Errorf("API key attribution = ID %v, name %q; want ID %d, name %q", opus.APIKeyID, opus.APIKeyName, apiKeyID, "CLI token")
	}
	converted := types.ConvertTokenActivity(opus)
	if converted.APIKeyID == nil || *converted.APIKeyID != apiKeyID || converted.APIKeyName != "CLI token" {
		t.Errorf("converted API key attribution = ID %v, name %q", converted.APIKeyID, converted.APIKeyName)
	}

	total, err := c.TotalTokenUsageForUser(ctx, "u1", now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("TotalTokenUsageForUser: %v", err)
	}
	if total.Usage.InputTokens != 100050+2006 {
		t.Errorf("summed input = %d, want %d", total.Usage.InputTokens, 100050+2006)
	}
	if total.Usage.OutputTokens != 800+300 {
		t.Errorf("summed output = %d, want %d", total.Usage.OutputTokens, 800+300)
	}
	if total.Usage.CacheReadTokens != 100000 {
		t.Errorf("summed cache read = %d, want 100000", total.Usage.CacheReadTokens)
	}
	if want := 0.07025 + 0.0033475; math.Abs(total.Usage.TotalSpend-want) > 1e-9 {
		t.Errorf("summed total spend = %v, want %v", total.Usage.TotalSpend, want)
	}
}
