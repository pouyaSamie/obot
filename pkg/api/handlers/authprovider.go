package handlers

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/api"
	"github.com/obot-platform/obot/pkg/api/handlers/providers"
	gateway "github.com/obot-platform/obot/pkg/gateway/client"
	"github.com/obot-platform/obot/pkg/gateway/server/dispatcher"
	"github.com/obot-platform/obot/pkg/license"
	"github.com/obot-platform/obot/pkg/ldapauth"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	"github.com/obot-platform/obot/pkg/system"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	CookieSecretEnvVar = "OBOT_AUTH_PROVIDER_COOKIE_SECRET"
)

type AuthProviderHandler struct {
	dispatcher  *dispatcher.Dispatcher
	postgresDSN string
	license     *license.Provider
	ldap        *ldapauth.Provider
}

func NewAuthProviderHandler(dispatcher *dispatcher.Dispatcher, postgresDSN string, licenseProvider *license.Provider, ldapProvider ...*ldapauth.Provider) *AuthProviderHandler {
	var ldap *ldapauth.Provider
	if len(ldapProvider) > 0 {
		ldap = ldapProvider[0]
	}
	return &AuthProviderHandler{
		dispatcher:  dispatcher,
		postgresDSN: postgresDSN,
		license:     licenseProvider,
		ldap:        ldap,
	}
}

type ldapSyncRequest struct {
	PreviewToken string `json:"previewToken"`
}

// LDAPSyncPreview lists the LDAP directory without changing Obot users.
func (ap *AuthProviderHandler) LDAPSyncPreview(req api.Context) error {
	entries, err := ap.ldapSyncEntries(req)
	if err != nil {
		return err
	}
	summary, err := req.GatewayClient.PreviewLDAPSync(req.Context(), system.DefaultNamespace, ldapauth.ProviderName, entries)
	if err != nil {
		return err
	}
	token, err := generateCookieSecret()
	if err != nil {
		return err
	}
	revision, err := ap.ldap.ConfigurationRevision(req.Context())
	if err != nil {
		return err
	}
	if err := req.GatewayClient.CreateLDAPSyncPreview(req.Context(), token, system.DefaultNamespace, ldapauth.ProviderName, revision, summary.SnapshotHash); err != nil {
		return err
	}
	summary.PreviewToken = token
	return req.Write(summary)
}

// LDAPSyncApply repeats the directory read and refuses to apply when it differs
// from the administrator-reviewed preview.
func (ap *AuthProviderHandler) LDAPSyncApply(req api.Context) error {
	var body ldapSyncRequest
	if err := req.Read(&body); err != nil {
		return types.NewErrBadRequest("invalid LDAP sync request: %v", err)
	}
	entries, err := ap.ldapSyncEntries(req)
	if err != nil {
		return err
	}
	preview, err := req.GatewayClient.PreviewLDAPSync(req.Context(), system.DefaultNamespace, ldapauth.ProviderName, entries)
	if err != nil {
		return err
	}
	if preview.Conflicts > 0 {
		return types.NewErrHTTP(http.StatusConflict, "LDAP synchronization has conflicts; resolve them and run preview again")
	}
	revision, err := ap.ldap.ConfigurationRevision(req.Context())
	if err != nil {
		return err
	}
	valid, err := req.GatewayClient.LDAPSyncPreviewValid(req.Context(), body.PreviewToken, system.DefaultNamespace, ldapauth.ProviderName, revision, preview.SnapshotHash)
	if err != nil {
		return err
	}
	if !valid {
		return types.NewErrHTTP(http.StatusConflict, "LDAP directory changed since preview; run preview again")
	}
	summary, err := req.GatewayClient.ApplyLDAPSync(req.Context(), system.DefaultNamespace, ldapauth.ProviderName, entries)
	if err != nil {
		return err
	}
	_ = req.GatewayClient.DeleteLDAPSyncPreview(req.Context(), body.PreviewToken)
	return req.Write(summary)
}

func (ap *AuthProviderHandler) ldapSyncEntries(req api.Context) ([]gateway.LDAPSyncUser, error) {
	if req.PathValue("id") != ldapauth.ProviderName || ap.ldap == nil {
		return nil, types.NewErrNotFound("LDAP synchronization is not available")
	}
	configured, err := ap.dispatcher.IsAuthProviderConfigured(req.Context(), system.DefaultNamespace, ldapauth.ProviderName)
	if err != nil {
		return nil, err
	}
	if !configured {
		return nil, types.NewErrBadRequest("LDAP must be configured before users can be synchronized")
	}
	directoryUsers, err := ap.ldap.ListDirectoryUsers(req.Context())
	if err != nil {
		return nil, fmt.Errorf("list LDAP users: %w", err)
	}
	entries := make([]gateway.LDAPSyncUser, 0, len(directoryUsers))
	for _, user := range directoryUsers {
		entries = append(entries, gateway.LDAPSyncUser{ID: user.ID, Username: user.Username, Email: user.Email, DisplayName: user.Name, Groups: user.Groups})
	}
	return entries, nil
}

func (ap *AuthProviderHandler) ByID(req api.Context) error {
	var authProvider v1.AuthProvider
	if err := req.Get(&authProvider, req.PathValue("id")); err != nil {
		return err
	}

	authProviderStatus, err := providers.AuthProviderStatus(req.Context(), authProvider, nil, ap.license)
	if err != nil {
		return err
	}

	return req.Write(ap.convertAuthProvider(authProvider, *authProviderStatus))
}

func (ap *AuthProviderHandler) List(req api.Context) error {
	var authProviders v1.AuthProviderList
	if err := req.List(&authProviders, &kclient.ListOptions{
		Namespace: req.Namespace(),
	}); err != nil {
		return err
	}

	resp := make([]types.AuthProvider, 0, len(authProviders.Items))
	for _, a := range authProviders.Items {
		authProviderStatus, err := providers.AuthProviderStatus(req.Context(), a, nil, ap.license)
		if err != nil {
			return err
		}

		resp = append(resp, ap.convertAuthProvider(a, *authProviderStatus))
	}

	return req.Write(types.AuthProviderList{Items: resp})
}

func (ap *AuthProviderHandler) Configure(req api.Context) error {
	var authProvider v1.AuthProvider
	if err := req.Get(&authProvider, req.PathValue("id")); err != nil {
		return err
	}

	if err := ap.license.RequireEntitlements(req.Context(), authProvider.Spec.RequiredEntitlements); err != nil {
		return err
	}

	if err := ensureNoPendingAuthProviderCleanup(req, authProvider); err != nil {
		return err
	}

	var envVars map[string]string
	if err := req.Read(&envVars); err != nil {
		return err
	} else if envVars == nil {
		envVars = make(map[string]string, 1)
	}

	cookieSecret, err := generateCookieSecret()
	if err != nil {
		return err
	}
	envVars[CookieSecretEnvVar] = cookieSecret

	for key, val := range envVars {
		if val == "" {
			delete(envVars, key)
		}
	}

	if authProvider.Name == ldapauth.ProviderName {
		if ap.ldap == nil {
			return types.NewErrBadRequest("LDAP authentication is not enabled")
		}
		if err := ap.ldap.ValidateConfig(req.Context(), ldapauth.ConfigFromValues(envVars)); err != nil {
			return types.NewErrBadRequest("invalid LDAP configuration: %v", err)
		}
	}

	stagedName, err := stageProviderCredential(req, envVars)
	if err != nil {
		return err
	}

	return submitProviderConfigurationChange(req, &v1.ProviderConfigurationChange{
		Name:      system.ProviderChangeAuthName,
		Namespace: authProvider.Namespace,
		Spec: v1.ProviderConfigurationChangeSpec{
			ProviderType:         v1.ProviderTypeAuth,
			ProviderName:         authProvider.Name,
			DesiredState:         v1.ProviderDesiredStateConfigured,
			StagedCredentialName: stagedName,
		},
	})
}

func ensureNoPendingAuthProviderCleanup(req api.Context, authProvider v1.AuthProvider) error {
	var cleanups v1.AuthProviderCleanupList
	if err := req.List(&cleanups); err != nil {
		return fmt.Errorf("list pending auth provider cleanups: %w", err)
	}
	for _, cleanup := range cleanups.Items {
		sameProvider := cleanup.Spec.AuthProviderName == authProvider.Name
		samePrefix := authProvider.Spec.GroupIDPrefix != "" && cleanup.Spec.GroupIDPrefix == authProvider.Spec.GroupIDPrefix
		if sameProvider || samePrefix {
			return types.NewErrBadRequest("authentication provider %q is still being deconfigured; wait for cleanup to finish before configuring it again", authProvider.Name)
		}
	}
	return nil
}

func (ap *AuthProviderHandler) Deconfigure(req api.Context) error {
	var authProvider v1.AuthProvider
	if err := req.Get(&authProvider, req.PathValue("id")); err != nil {
		return err
	}

	return submitProviderConfigurationChange(req, &v1.ProviderConfigurationChange{
		Name:      system.ProviderChangeAuthName,
		Namespace: authProvider.Namespace,
		Spec: v1.ProviderConfigurationChangeSpec{
			ProviderType: v1.ProviderTypeAuth,
			ProviderName: authProvider.Name,
			DesiredState: v1.ProviderDesiredStateDeconfigured,
		},
	})
}

func (ap *AuthProviderHandler) Reveal(req api.Context) error {
	var authProvider v1.AuthProvider
	if err := req.Get(&authProvider, req.PathValue("id")); err != nil {
		return err
	}

	cred, err := req.GatewayClient.RevealCredential(req.Context(), []string{authProvider.Name, system.GenericAuthProviderCredentialContext}, authProvider.Name)
	if err != nil && !errors.As(err, &gateway.CredentialNotFoundError{}) {
		return fmt.Errorf("failed to reveal credential for auth provider %q: %w", authProvider.Name, err)
	} else if err == nil {
		return req.Write(cred.Secrets)
	}

	return types.NewErrNotFound("no credential found for %q", authProvider.Name)
}

func (ap *AuthProviderHandler) convertAuthProvider(authProvider v1.AuthProvider, authProviderStatus types.AuthProviderStatus) types.AuthProvider {
	authProviderStatus.SupportsUserSync = authProvider.Name == ldapauth.ProviderName
	return types.AuthProvider{
		Metadata:             MetadataFrom(&authProvider),
		AuthProviderManifest: authProvider.Spec.AuthProviderManifest,
		AuthProviderStatus:   authProviderStatus,
	}
}

func generateCookieSecret() (string, error) {
	const length = 32

	// Generate a random token. Repeat until we have one that is 32 bytes long after trimming.
	// This only takes one try in the vast majority of circumstances, but could occasionally take a second.
	var b = make([]byte, length)
	_, err := rand.Read(b)
	if err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}

	for len(bytes.TrimSpace(b)) != length {
		_, err := rand.Read(b)
		if err != nil {
			return "", fmt.Errorf("failed to generate random token: %w", err)
		}
	}

	return base64.StdEncoding.EncodeToString(b), nil
}
