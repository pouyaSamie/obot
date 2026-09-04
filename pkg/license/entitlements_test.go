package license

import "testing"

func TestAlwaysOnEditionIsUnlimited(t *testing.T) {
	provider, err := NewProvider(t.Context(), nil, Config{})
	if err != nil {
		t.Fatal(err)
	}

	missing, err := provider.MissingEntitlements(t.Context(), []string{"ANY_LEGACY_ENTITLEMENT"})
	if err != nil || len(missing) != 0 {
		t.Fatalf("MissingEntitlements() = %v, %v", missing, err)
	}

	userLimit, err := provider.UserLimit(t.Context())
	if err != nil || !userLimit.Unlimited {
		t.Fatalf("UserLimit() = %+v, %v", userLimit, err)
	}
	deviceLimit, err := provider.DeviceLimit(t.Context())
	if err != nil || !deviceLimit.Unlimited {
		t.Fatalf("DeviceLimit() = %+v, %v", deviceLimit, err)
	}
}
