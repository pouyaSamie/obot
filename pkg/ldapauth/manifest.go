package ldapauth

import (
	"encoding/base64"
	"github.com/obot-platform/obot/apiclient/types"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	"github.com/obot-platform/obot/pkg/system"
)

var icon = "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><path fill="#4f7ef3" d="M8 8h48v48H8z"/><path fill="white" d="M18 20h28v6H24v8h18v6H24v10h22v6H18z"/></svg>`))

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
				Icon:        icon,
				IconDark:    icon,
				Description: "Authenticate users against LDAP or Active Directory. Direct LDAP groups are exposed with the ldap/ prefix.",
				RequiredConfigurationParameters: []types.ProviderConfigurationParameter{
					{Name: URLConfigKey, FriendlyName: "LDAP Server URL", Description: "For Active Directory on port 389, use ldap://server.example.com:389. Use ldaps:// only when the server provides LDAPS."},
					{Name: BindDNConfigKey, FriendlyName: "Service Account", Description: "Account used to search the directory. Active Directory commonly accepts a UPN such as infra@example.com or a full DN."},
					{Name: BindPasswordConfigKey, FriendlyName: "Service Account Password", Description: "Password for the LDAP service account.", Sensitive: true},
					{Name: UserBaseDNConfigKey, FriendlyName: "User Base DN", Description: "Directory root below which Obot searches for users, for example DC=example,DC=com."},
				},
				OptionalConfigurationParameters: []types.ProviderConfigurationParameter{
					{Name: UserFilterConfigKey, FriendlyName: "Login Filter", Description: "Used only when a person signs in. It must contain {username}; for AD username or email use (|(sAMAccountName={username})(mail={username})). Do not paste the Confluence User Object Filter here."},
					{Name: SyncUserFilterConfigKey, FriendlyName: "User Sync Filter", Description: "Used only by the manual directory sync. This is the equivalent of Confluence's User Object Filter and does not use {username}. Defaults to (objectClass=person)."},
					{Name: UserIDAttributeConfigKey, FriendlyName: "Stable User ID Attribute", Description: "Identity key across renames. For Active Directory use objectGUID; default is uid."},
					{Name: UsernameAttributeConfigKey, FriendlyName: "Username Attribute", Description: "Username shown by Obot. For Active Directory use sAMAccountName; defaults to the Stable User ID Attribute."},
					{Name: EmailAttributeConfigKey, FriendlyName: "Email Attribute", Description: "For Active Directory use mail."},
					{Name: NameAttributeConfigKey, FriendlyName: "Display Name Attribute", Description: "For Active Directory use displayName; default is cn."},
					{Name: GroupAttributeConfigKey, FriendlyName: "Direct Group Attribute", Description: "For Active Directory use memberOf. Direct groups become ldap/<group>."},
					{Name: StartTLSConfigKey, FriendlyName: "Use StartTLS", Description: "Upgrade an ldap:// connection with StartTLS."},
					{Name: InsecureSkipVerifyConfigKey, FriendlyName: "Skip TLS Certificate Verification", Description: "Development diagnostics only. Do not use in production."},
					{Name: CACertificateConfigKey, FriendlyName: "Custom CA Certificate", Description: "PEM-encoded CA certificate used to validate the LDAP server.", Sensitive: true, Multiline: true},
				},
			},
		}},
	}
}
