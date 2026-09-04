package ldapauth

import (
	"errors"
	"testing"

	"github.com/go-ldap/ldap/v3"
)

func TestProfileFromEntryPrefixesDirectGroups(t *testing.T) {
	entry := &ldap.Entry{DN: "CN=Jane Doe,OU=People,DC=example,DC=com", Attributes: []*ldap.EntryAttribute{
		{Name: "uid", Values: []string{"jane"}},
		{Name: "mail", Values: []string{"jane@example.com"}},
		{Name: "cn", Values: []string{"Jane Doe"}},
		{Name: "memberOf", Values: []string{"CN=Engineering,OU=Groups,DC=example,DC=com", "CN=Finance,OU=Groups,DC=example,DC=com"}},
	}}

	profile := profileFromEntry(entry, "ignored", Config{}.normalized())
	if profile.ID != "jane" || profile.Email != "jane@example.com" || profile.Name != "Jane Doe" {
		t.Fatalf("unexpected profile: %+v", profile)
	}
	if len(profile.Groups) != 2 || profile.Groups[0] != "ldap/Engineering" || profile.Groups[1] != "ldap/Finance" {
		t.Fatalf("groups = %#v", profile.Groups)
	}
}

func TestGroupNameUsesLDAPDNParser(t *testing.T) {
	if got, want := groupName("CN=Engineering\\, Platform,OU=Groups,DC=example,DC=com"), "Engineering, Platform"; got != want {
		t.Fatalf("groupName() = %q, want %q", got, want)
	}
	if got := groupName("not-a-dn"); got != "" {
		t.Fatalf("groupName(invalid) = %q", got)
	}
}

func TestConfigFromValuesUsesUIValuesAndDefaults(t *testing.T) {
	config := ConfigFromValues(map[string]string{
		URLConfigKey:                "ldaps://ldap.example.com:636",
		BindDNConfigKey:             "CN=Obot,OU=Service Accounts,DC=example,DC=com",
		BindPasswordConfigKey:       "secret",
		UserBaseDNConfigKey:         "OU=People,DC=example,DC=com",
		StartTLSConfigKey:           "false",
		InsecureSkipVerifyConfigKey: "true",
		CACertificateConfigKey:      "first\\nsecond",
	})

	config, err := config.validate()
	if err != nil {
		t.Fatal(err)
	}
	if config.UserFilter != "(|(uid={username})(mail={username}))" || config.UserIDAttribute != "uid" || config.SyncUserFilter != "(objectClass=person)" {
		t.Fatalf("unexpected defaults: %+v", config)
	}
	if !config.InsecureSkipVerify || config.StartTLS || config.CACertificate != "first\nsecond" {
		t.Fatalf("unexpected UI configuration: %+v", config)
	}
}

func TestLDAPConfigRequiresUsernamePlaceholder(t *testing.T) {
	_, err := (Config{URL: "ldap://ldap.example.com", BindDN: "cn=service,dc=example,dc=com", UserBaseDN: "dc=example,dc=com", UserFilter: "(uid=user)"}).validate()
	if err == nil {
		t.Fatal("expected login-filter validation error")
	}
}

func TestLDAPConfigRejectsMalformedSyncFilter(t *testing.T) {
	_, err := (Config{
		URL:            "ldap://ldap.example.com",
		BindDN:         "cn=service,dc=example,dc=com",
		UserBaseDN:     "dc=example,dc=com",
		UserFilter:     "(uid={username})",
		SyncUserFilter: "(&(objectClass=person)",
	}).validate()
	if err == nil {
		t.Fatal("expected LDAP sync filter validation error")
	}
}

func TestProfileFromEntryKeepsADUsernameAndEncodesBinaryGUID(t *testing.T) {
	rawGUID := string([]byte{0, 255, 1, 2})
	entry := &ldap.Entry{Attributes: []*ldap.EntryAttribute{
		{Name: "objectGUID", Values: []string{rawGUID}},
		{Name: "sAMAccountName", Values: []string{"jane"}},
		{Name: "mail", Values: []string{"jane@example.com"}},
		{Name: "displayName", Values: []string{"Jane Doe"}},
	}}
	profile := profileFromEntry(entry, "", Config{
		UserIDAttribute:   "objectGUID",
		UsernameAttribute: "sAMAccountName",
		EmailAttribute:    "mail",
		NameAttribute:     "displayName",
	}.normalized())
	if profile.Username != "jane" {
		t.Fatalf("username = %q, want jane", profile.Username)
	}
	if profile.ID != "ldap-b64:AP8BAg" {
		t.Fatalf("stable binary ID = %q", profile.ID)
	}
	if got := rawLDAPID(profile.ID); got != rawGUID {
		t.Fatalf("rawLDAPID() = %v, want original binary GUID", []byte(got))
	}
}

func TestValidationAcceptsLDAPSearchSizeLimit(t *testing.T) {
	if !isValidationSizeLimit(ldap.NewError(ldap.LDAPResultSizeLimitExceeded, errors.New("limit reached"))) {
		t.Fatal("expected validation to accept LDAP size-limit result")
	}
	if isValidationSizeLimit(ldap.NewError(ldap.LDAPResultInvalidCredentials, errors.New("bad credentials"))) {
		t.Fatal("unexpected acceptance of non-size-limit LDAP error")
	}
}
