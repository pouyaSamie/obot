// Package license provides the edition policy used by legacy provider wiring.
//
// The product edition is permanently enabled. The package name is retained so
// existing provider manifests and internal call sites remain compatible while
// the former third-party licensing implementation is removed.
package license

import (
	"context"

	"github.com/obot-platform/obot/pkg/gateway/client"
)

const (
	EnterpriseAuthProvidersEntitlement  = "OBOT_ENTERPRISE_AUTH_PROVIDERS"
	EnterpriseEntitlement               = "OBOT_ENTERPRISE"
	CommunityEntitlement                = "OBOT_COMMUNITY"
	EnterpriseModelProvidersEntitlement = "OBOT_ENTERPRISE_MODEL_PROVIDERS"
)

// Config remains for source compatibility. Licensing is not configurable.
type Config struct{}

// Provider is the fixed, always-on product edition policy.
type Provider struct{}

func NewProvider(_ context.Context, _ *client.Client, _ Config) (*Provider, error) {
	return &Provider{}, nil
}

func (p *Provider) HasValidLicense(context.Context) (bool, error) {
	return true, nil
}

// Entitlements reports the historic Enterprise entitlement for compatibility
// with integrations that display the current edition.
func (p *Provider) Entitlements(context.Context) ([]string, error) {
	return []string{EnterpriseEntitlement}, nil
}
