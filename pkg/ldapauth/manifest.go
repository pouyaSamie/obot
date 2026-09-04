package ldapauth

import (
	"github.com/obot-platform/obot/apiclient/types"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	"github.com/obot-platform/obot/pkg/system"
)

// AuthProvider describes the built-in LDAP provider. Its settings are stored
// with the other auth-provider credentials, so they are configured from the
// Admin UI and encrypted at rest rather than baked into a deployment.
func AuthProvider() *v1.AuthProvider {
	return &v1.AuthProvider{
		Name:      ProviderName,
		Namespace: system.DefaultNamespace,
		Spec: v1.AuthProviderSpec{AuthProviderManifest: types.AuthProviderManifest{
			GroupIDPrefix: groupPrefix,
			CommonProviderMetadata: types.CommonProviderMetadata{
				Name:        "LDAP",
				Description: "Authenticate users against LDAP or Active Directory. Direct LDAP groups are exposed with the ldap/ prefix.",
				RequiredConfigurationParameters: []types.ProviderConfigurationParameter{
					{Name: URLConfigKey, FriendlyName: "LDAP URL", Description: "LDAP endpoint, for example ldaps://ldap.example.com:636."},
					{Name: BindDNConfigKey, FriendlyName: "Bind DN", Description: "Service-account DN used to find users and refresh group membership."},
					{Name: BindPasswordConfigKey, FriendlyName: "Bind Password", Description: "Password for the LDAP service account.", Sensitive: true},
					{Name: UserBaseDNConfigKey, FriendlyName: "User Base DN", Description: "Base DN used to search for user accounts."},
				},
				OptionalConfigurationParameters: []types.ProviderConfigurationParameter{
					{Name: UserFilterConfigKey, FriendlyName: "User Filter", Description: "LDAP filter for login users. Use {username} as the escaped username placeholder."},
					{Name: UserIDAttributeConfigKey, FriendlyName: "User ID Attribute", Description: "Defaults to uid."},
					{Name: EmailAttributeConfigKey, FriendlyName: "Email Attribute", Description: "Defaults to mail."},
					{Name: NameAttributeConfigKey, FriendlyName: "Display Name Attribute", Description: "Defaults to cn."},
					{Name: GroupAttributeConfigKey, FriendlyName: "Direct Group Attribute", Description: "Defaults to memberOf. Direct groups become ldap/<group>."},
					{Name: StartTLSConfigKey, FriendlyName: "Use StartTLS", Description: "Upgrade an ldap:// connection with StartTLS."},
					{Name: InsecureSkipVerifyConfigKey, FriendlyName: "Skip TLS Certificate Verification", Description: "Development diagnostics only. Do not use in production."},
					{Name: CACertificateConfigKey, FriendlyName: "Custom CA Certificate", Description: "PEM-encoded CA certificate used to validate the LDAP server.", Sensitive: true, Multiline: true},
				},
			},
		}},
	}
}