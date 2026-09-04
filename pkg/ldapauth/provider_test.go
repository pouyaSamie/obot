package ldapauth

import (
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

func TestGroupNameRejectsNonDN(t *testing.T) {
	if got := groupName("not-a-dn"); got != "" {
		t.Fatalf("groupName = %q, want empty", got)
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
	if config.UserFilter != "(&(objectClass=person)(uid={username}))" || config.UserIDAttribute != "uid" {
		t.Fatalf("unexpected defaults: %+v", config)
	}
	if !config.InsecureSkipVerify || config.StartTLS || config.CACertificate != "first\nsecond" {
		t.Fatalf("unexpected UI configuration: %+v", config)
	}
}