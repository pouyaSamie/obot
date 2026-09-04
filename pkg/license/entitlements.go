package license

import (
	"context"
	"net/http"

	"github.com/obot-platform/obot/pkg/gateway/client"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Violation is retained for API compatibility. The always-on edition never
// returns a violation.
type Violation struct {
	Type                 string   `json:"type"`
	Namespace            string   `json:"namespace"`
	Name                 string   `json:"name"`
	RequiredEntitlements []string `json:"requiredEntitlements"`
	MissingEntitlements  []string `json:"missingEntitlements"`
	Message              string   `json:"message"`
}

// MissingEntitlements deliberately ignores manifest metadata in the always-on
// edition so existing manifests continue to load without feature gates.
func (p *Provider) MissingEntitlements(context.Context, []string) ([]string, error) {
	return nil, nil
}

func (p *Provider) RequireEntitlements(context.Context, []string) error {
	return nil
}

func (p *Provider) UserLimit(context.Context) (client.UserLimit, error) {
	return client.UserLimit{Unlimited: true}, nil
}

func (p *Provider) DeviceLimit(context.Context) (client.DeviceLimit, error) {
	return client.DeviceLimit{Unlimited: true}, nil
}

func (p *Provider) GetLicenseViolations(context.Context, kclient.Client) ([]Violation, error) {
	return nil, nil
}

// ProviderEntitlementGate remains a no-op so request access is no longer
// controlled by license state.
type ProviderEntitlementGate struct{}

func NewProviderEntitlementGate(*Provider, kclient.Client) *ProviderEntitlementGate {
	return &ProviderEntitlementGate{}
}

func (g *ProviderEntitlementGate) Check(*http.Request) error {
	return nil
}
