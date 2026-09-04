// Package ldapauth implements Obot's built-in LDAP username/password provider.
package ldapauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-ldap/ldap/v3"
	"github.com/obot-platform/obot/pkg/auth"
	gateway "github.com/obot-platform/obot/pkg/gateway/client"
	"github.com/obot-platform/obot/pkg/hash"
	"github.com/obot-platform/obot/pkg/system"
)

const (
	ProviderName = "ldap-auth-provider"
	LoginPath    = "/login/ldap"
	groupPrefix  = "ldap/"
	sessionTTL   = 7 * 24 * time.Hour

	URLConfigKey                = "OBOT_LDAP_URL"
	BindDNConfigKey             = "OBOT_LDAP_BIND_DN"
	BindPasswordConfigKey       = "OBOT_LDAP_BIND_PASSWORD"
	UserBaseDNConfigKey         = "OBOT_LDAP_USER_BASE_DN"
	UserFilterConfigKey         = "OBOT_LDAP_USER_FILTER"
	UserIDAttributeConfigKey    = "OBOT_LDAP_USER_ID_ATTRIBUTE"
	UsernameAttributeConfigKey  = "OBOT_LDAP_USERNAME_ATTRIBUTE"
	EmailAttributeConfigKey     = "OBOT_LDAP_EMAIL_ATTRIBUTE"
	NameAttributeConfigKey      = "OBOT_LDAP_NAME_ATTRIBUTE"
	GroupAttributeConfigKey     = "OBOT_LDAP_GROUP_ATTRIBUTE"
	StartTLSConfigKey           = "OBOT_LDAP_START_TLS"
	InsecureSkipVerifyConfigKey = "OBOT_LDAP_INSECURE_SKIP_VERIFY"
	CACertificateConfigKey      = "OBOT_LDAP_CA_CERTIFICATE"
	SyncUserFilterConfigKey     = "OBOT_LDAP_SYNC_USER_FILTER"
)

// Config is loaded from the encrypted auth-provider credential store.
type Config struct {
	URL                string
	BindDN             string
	BindPassword       string
	UserBaseDN         string
	UserFilter         string
	UserIDAttribute    string
	UsernameAttribute  string
	EmailAttribute     string
	NameAttribute      string
	GroupAttribute     string
	StartTLS           bool
	InsecureSkipVerify bool
	CACertificate      string
	SyncUserFilter     string
}

func (c Config) normalized() Config {
	if c.UserIDAttribute == "" {
		c.UserIDAttribute = "uid"
	}
	if c.UsernameAttribute == "" {
		c.UsernameAttribute = c.UserIDAttribute
	}
	if c.EmailAttribute == "" {
		c.EmailAttribute = "mail"
	}
	if c.NameAttribute == "" {
		c.NameAttribute = "cn"
	}
	if c.GroupAttribute == "" {
		c.GroupAttribute = "memberOf"
	}
	if c.UserFilter == "" {
		c.UserFilter = fmt.Sprintf("(|(%s={username})(%s={username}))", c.UserIDAttribute, c.EmailAttribute)
	}
	if c.SyncUserFilter == "" {
		c.SyncUserFilter = "(objectClass=person)"
	}
	return c
}
// ConfigLoader obtains the current configuration. It is called per LDAP
// operation so a saved UI change applies immediately without a restart.
type ConfigLoader func(context.Context) (Config, error)

// ConfigFromValues turns the provider credential map into an LDAP config.
func ConfigFromValues(values map[string]string) Config {
	return Config{
		URL:                values[URLConfigKey],
		BindDN:             values[BindDNConfigKey],
		BindPassword:       values[BindPasswordConfigKey],
		UserBaseDN:         values[UserBaseDNConfigKey],
		UserFilter:         values[UserFilterConfigKey],
		UserIDAttribute:    values[UserIDAttributeConfigKey],
		UsernameAttribute:  values[UsernameAttributeConfigKey],
		EmailAttribute:     values[EmailAttributeConfigKey],
		NameAttribute:      values[NameAttributeConfigKey],
		GroupAttribute:     values[GroupAttributeConfigKey],
		StartTLS:           values[StartTLSConfigKey] == "true",
		InsecureSkipVerify: values[InsecureSkipVerifyConfigKey] == "true",
		CACertificate:      strings.ReplaceAll(values[CACertificateConfigKey], "\\n", "\n"),
		SyncUserFilter:     values[SyncUserFilterConfigKey],
	}
}

func (c Config) validate() (Config, error) {
	c = c.normalized()
	if c.UserBaseDN == "" {
		return Config{}, errors.New("LDAP user base DN is required")
	}
	if c.BindDN == "" {
		return Config{}, errors.New("LDAP bind DN is required")
	}
	u, err := url.Parse(c.URL)
	if err != nil || u.Host == "" || (u.Scheme != "ldap" && u.Scheme != "ldaps") {
		return Config{}, fmt.Errorf("invalid LDAP URL %q", c.URL)
	}
	if c.StartTLS && u.Scheme != "ldap" {
		return Config{}, errors.New("StartTLS requires an ldap:// URL")
	}
	if !strings.Contains(c.UserFilter, "{username}") {
		return Config{}, errors.New("LDAP login filter must contain {username}; use the User Sync Filter for a directory user-object filter")
	}
	if _, err := ldap.CompileFilter(strings.ReplaceAll(c.UserFilter, "{username}", "obot-configuration-check")); err != nil {
		return Config{}, fmt.Errorf("invalid LDAP login filter: %w", err)
	}
	if _, err := ldap.CompileFilter(c.SyncUserFilter); err != nil {
		return Config{}, fmt.Errorf("invalid LDAP user sync filter: %w", err)
	}
	if _, err := ldap.ParseDN(c.UserBaseDN); err != nil {
		return Config{}, fmt.Errorf("invalid LDAP user base DN: %w", err)
	}
	for _, attribute := range []string{c.UserIDAttribute, c.UsernameAttribute, c.EmailAttribute, c.NameAttribute, c.GroupAttribute} {
		if !ldapAttributeName.MatchString(attribute) {
			return Config{}, fmt.Errorf("invalid LDAP attribute name %q", attribute)
		}
	}
	return c, nil
}

var ldapAttributeName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)

// revision changes whenever a saved LDAP configuration changes. It is used to
// invalidate sessions issued under an earlier server, filter, or TLS policy.
func (c Config) revision() string {
	b, _ := json.Marshal(c.normalized())
	sum := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type profile struct {
	ID, Username, Email, Name string
	Groups                    []string
	ConfigRevision            string
}

// DirectoryUser is the directory-safe representation consumed by the admin
// synchronization endpoint. Passwords and bind credentials are never exposed.
type DirectoryUser struct {
	ID, Username, Email, Name string
	Groups                    []string
}

// ValidateConfig verifies that a configuration can establish the requested
// secure connection, bind the service account, access the base DN, and run
// both configured search filters. It deliberately performs no user changes.
func (p *Provider) ValidateConfig(ctx context.Context, config Config) error {
	config, err := config.validate()
	if err != nil {
		return err
	}
	conn, err := p.connect(config)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.Bind(config.BindDN, config.BindPassword); err != nil {
		return fmt.Errorf("LDAP service-account bind: %w", err)
	}
	attrs := directoryAttributes(config)
	loginFilter := strings.ReplaceAll(config.UserFilter, "{username}", ldap.EscapeFilter("obot-configuration-check"))
	for _, filter := range []string{loginFilter, config.SyncUserFilter} {
		if _, err := conn.Search(ldap.NewSearchRequest(config.UserBaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 1, 10, false, filter, attrs, nil)); err != nil {
			return fmt.Errorf("LDAP search validation: %w", err)
		}
	}
	return nil
}

// ConfigurationRevision identifies the complete active LDAP configuration
// without exposing its credentials.
func (p *Provider) ConfigurationRevision(ctx context.Context) (string, error) {
	config, err := p.config(ctx)
	if err != nil {
		return "", err
	}
	return config.revision(), nil
}
type session struct {
	profile   profile
	expiresAt time.Time
}

type Provider struct {
	loadConfig ConfigLoader
	serverURL string
	gatewayClient *gateway.Client
	key       []byte
	mu        sync.Mutex
	sessions  map[string]session
}

func New(serverURL string, loadConfig ConfigLoader, gatewayClient ...*gateway.Client) (*Provider, error) {
	if loadConfig == nil {
		return nil, errors.New("LDAP configuration loader is required")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	var client *gateway.Client
	if len(gatewayClient) > 0 {
		client = gatewayClient[0]
	}
	return &Provider{loadConfig: loadConfig, serverURL: serverURL, gatewayClient: client, key: key, sessions: map[string]session{}}, nil
}

func (p *Provider) Start(ctx context.Context) (url.URL, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return url.URL{}, err
	}
	server := &http.Server{Handler: p.handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	go func() { _ = server.Serve(listener) }()
	if p.gatewayClient != nil {
		go p.cleanupSessions(ctx)
	}
	return url.URL{Scheme: "http", Host: listener.Addr().String()}, nil
}

func (p *Provider) cleanupSessions(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if err := p.gatewayClient.DeleteExpiredLDAPAuthSessions(ctx); err != nil {
			// Cleanup is best effort; expired sessions are rejected on read too.
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

func (p *Provider) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /oauth2/start", p.start)
	mux.HandleFunc("POST /oauth2/start", p.login)
	mux.HandleFunc("GET /oauth2/sign_out", p.signOut)
	mux.HandleFunc("POST /obot-get-state", p.getState)
	mux.HandleFunc("GET /obot-get-user-info", p.getUserInfo)
	mux.HandleFunc("POST /obot-list-user-auth-groups", p.listGroups)
	return mux
}

func (p *Provider) start(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, LoginPath+"?rd="+url.QueryEscape(redirectTarget(r.URL.Query().Get("rd"))), http.StatusFound)
}

func (p *Provider) login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	rd := redirectTarget(r.FormValue("rd"))
	if !sameOrigin(r) {
		http.Error(w, "cross-origin login is not allowed", http.StatusForbidden)
		return
	}
	username, password := strings.TrimSpace(r.FormValue("email")), r.FormValue("password")
	if username == "" || password == "" {
		p.loginFailed(w, r, rd, "Enter your username and password.")
		return
	}
	config, err := p.config(r.Context())
	if err != nil {
		p.loginFailed(w, r, rd, "LDAP is not available.")
		return
	}
	user, err := p.authenticateWithConfig(r.Context(), config, username, password)
	if err != nil {
		p.loginFailed(w, r, rd, "Incorrect username or password.")
		return
	}
	token, err := randomToken()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	expires := time.Now().Add(sessionTTL)
	if p.gatewayClient != nil {
		user.ConfigRevision = config.revision()
		serialized, err := json.Marshal(user)
		if err != nil || p.gatewayClient.CreateLDAPAuthSession(r.Context(), hash.String(token), system.DefaultNamespace, ProviderName, user.ID, user.ConfigRevision, string(serialized), expires) != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	} else {
		p.mu.Lock()
		p.sessions[token] = session{profile: user, expiresAt: expires}
		p.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: auth.ObotAccessTokenCookie, Value: token, Path: "/", Expires: expires, HttpOnly: true, Secure: strings.HasPrefix(p.serverURL, "https://"), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, rd, http.StatusFound)
}

func (p *Provider) signOut(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.ObotAccessTokenCookie); err == nil {
		if p.gatewayClient != nil {
			_ = p.gatewayClient.DeleteLDAPAuthSession(r.Context(), hash.String(c.Value))
		} else {
			p.mu.Lock()
			delete(p.sessions, c.Value)
			p.mu.Unlock()
		}
	}
	http.SetCookie(w, &http.Cookie{Name: auth.ObotAccessTokenCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: strings.HasPrefix(p.serverURL, "https://"), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, redirectTarget(r.URL.Query().Get("rd")), http.StatusFound)
}

func (p *Provider) getState(w http.ResponseWriter, r *http.Request) {
	var request auth.SerializableRequest
	if json.NewDecoder(r.Body).Decode(&request) != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	s, ok := p.session(r.Context(), cookieValue(request.Header, auth.ObotAccessTokenCookie))
	if !ok {
		http.Error(w, "record not found", http.StatusInternalServerError)
		return
	}
	writeJSON(w, auth.SerializableState{ExpiresOn: &s.expiresAt, AccessToken: p.signProfile(s.profile), User: s.profile.ID, PreferredUsername: s.profile.Username, Email: s.profile.Email})
}

func (p *Provider) getUserInfo(w http.ResponseWriter, r *http.Request) {
	user, err := p.verifyProfile(r.Context(), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]any{"id": user.ID, "email": user.Email, "name": user.Name})
}

func (p *Provider) listGroups(w http.ResponseWriter, r *http.Request) {
	user, err := p.findProfileByID(r.Context(), strings.TrimSpace(readBody(r)))
	if err != nil {
		http.Error(w, "unable to fetch LDAP groups", http.StatusInternalServerError)
		return
	}
	groups := make([]auth.GroupInfo, 0, len(user.Groups))
	for _, group := range user.Groups {
		groups = append(groups, auth.GroupInfo{ID: group, Name: strings.TrimPrefix(group, groupPrefix)})
	}
	writeJSON(w, groups)
}

func (p *Provider) session(ctx context.Context, token string) (session, bool) {
	if p.gatewayClient != nil {
		config, err := p.config(ctx)
		if err != nil {
			return session{}, false
		}
		stored, err := p.gatewayClient.LDAPAuthSession(ctx, hash.String(token), config.revision())
		if err != nil {
			return session{}, false
		}
		var user profile
		if json.Unmarshal([]byte(stored.Profile), &user) != nil {
			return session{}, false
		}
		return session{profile: user, expiresAt: stored.ExpiresAt}, true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.sessions[token]
	if !ok || time.Now().After(s.expiresAt) {
		delete(p.sessions, token)
		return session{}, false
	}
	return s, true
}

func (p *Provider) authenticate(ctx context.Context, username, password string) (profile, error) {
	config, err := p.config(ctx)
	if err != nil {
		return profile{}, err
	}
	return p.authenticateWithConfig(ctx, config, username, password)
}

func (p *Provider) authenticateWithConfig(_ context.Context, config Config, username, password string) (profile, error) {
	conn, err := p.connect(config)
	if err != nil {
		return profile{}, err
	}
	defer conn.Close()
	if err := conn.Bind(config.BindDN, config.BindPassword); err != nil {
		return profile{}, err
	}
	filter := strings.ReplaceAll(config.UserFilter, "{username}", ldap.EscapeFilter(username))
	attrs := directoryAttributes(config)
	result, err := conn.Search(ldap.NewSearchRequest(config.UserBaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 10, false, filter, attrs, nil))
	if err != nil || len(result.Entries) != 1 {
		return profile{}, errors.New("LDAP user not found")
	}
	entry := result.Entries[0]
	if err := conn.Bind(entry.DN, password); err != nil {
		return profile{}, err
	}
	return profileFromEntry(entry, username, config), nil
}

func (p *Provider) findProfileByID(ctx context.Context, id string) (profile, error) {
	config, err := p.config(ctx)
	if err != nil {
		return profile{}, err
	}
	conn, err := p.connect(config)
	if err != nil {
		return profile{}, err
	}
	defer conn.Close()
	if err := conn.Bind(config.BindDN, config.BindPassword); err != nil {
		return profile{}, err
	}
	filter := fmt.Sprintf("(%s=%s)", config.UserIDAttribute, ldap.EscapeFilter(rawLDAPID(id)))
	attrs := directoryAttributes(config)
	result, err := conn.Search(ldap.NewSearchRequest(config.UserBaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 10, false, filter, attrs, nil))
	if err != nil || len(result.Entries) != 1 {
		return profile{}, errors.New("LDAP user not found")
	}
	return profileFromEntry(result.Entries[0], id, config), nil
}

// ListDirectoryUsers returns every entry eligible for manual synchronization.
// RFC 2696 paging is mandatory so a directory result-size limit cannot turn a
// partial response into an accidental account-disable operation.
func (p *Provider) ListDirectoryUsers(ctx context.Context) ([]DirectoryUser, error) {
	config, err := p.config(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := p.connect(config)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := conn.Bind(config.BindDN, config.BindPassword); err != nil {
		return nil, err
	}

	attrs := directoryAttributes(config)
	paging := ldap.NewControlPaging(500)
	users := make([]DirectoryUser, 0)
	for {
		result, err := conn.Search(ldap.NewSearchRequest(config.UserBaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 30, false, config.SyncUserFilter, attrs, []ldap.Control{paging}))
		if err != nil {
			return nil, err
		}
		for _, entry := range result.Entries {
			profile := profileFromEntry(entry, "", config)
			users = append(users, DirectoryUser{ID: profile.ID, Username: profile.Username, Email: profile.Email, Name: profile.Name, Groups: profile.Groups})
		}
		control := ldap.FindControl(result.Controls, ldap.ControlTypePaging)
		page, ok := control.(*ldap.ControlPaging)
		if !ok || len(page.Cookie) == 0 {
			break
		}
		paging.SetCookie(page.Cookie)
	}
	return users, nil
}

func (p *Provider) config(ctx context.Context) (Config, error) {
	config, err := p.loadConfig(ctx)
	if err != nil {
		return Config{}, err
	}
	return config.validate()
}

func (p *Provider) connect(config Config) (*ldap.Conn, error) {
	u, err := url.Parse(config.URL)
	if err != nil {
		return nil, err
	}
	tlsConfig := &tls.Config{ServerName: u.Hostname(), InsecureSkipVerify: config.InsecureSkipVerify} // #nosec G402 -- an administrator explicitly opts in.
	if config.CACertificate != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(config.CACertificate)) {
			return nil, errors.New("LDAP custom CA certificate contains no certificates")
		}
		tlsConfig.RootCAs = pool
	}
	conn, err := ldap.DialURL(config.URL, ldap.DialWithTLSConfig(tlsConfig))
	if err != nil {
		return nil, err
	}
	if config.StartTLS {
		if err := conn.StartTLS(tlsConfig); err != nil {
			conn.Close()
			return nil, err
		}
	}
	return conn, nil
}
func profileFromEntry(entry *ldap.Entry, fallback string, config Config) profile {
	id := stableLDAPID(entry.GetAttributeValue(config.UserIDAttribute))
	if id == "" {
		id = stableLDAPID(entry.DN)
	}
	username := entry.GetAttributeValue(config.UsernameAttribute)
	if username == "" {
		username = fallback
	}
	if username == "" {
		username = id
	}
	email := entry.GetAttributeValue(config.EmailAttribute)
	if email == "" {
		email = fallback
	}
	name := entry.GetAttributeValue(config.NameAttribute)
	if name == "" {
		name = id
	}
	groups := make([]string, 0)
	seen := map[string]struct{}{}
	for _, dn := range entry.GetAttributeValues(config.GroupAttribute) {
		if name := groupName(dn); name != "" {
			group := groupPrefix + name
			if _, ok := seen[group]; !ok {
				seen[group] = struct{}{}
				groups = append(groups, group)
			}
		}
	}
	return profile{ID: id, Username: username, Email: email, Name: name, Groups: groups}
}

func directoryAttributes(config Config) []string {
	return []string{config.UserIDAttribute, config.UsernameAttribute, config.EmailAttribute, config.NameAttribute, config.GroupAttribute}
}

const binaryLDAPIDPrefix = "ldap-b64:"

func stableLDAPID(value string) string {
	if value == "" {
		return value
	}
	if utf8.ValidString(value) {
		isText := true
		for _, r := range value {
			if r < 0x20 || r == 0x7f {
				isText = false
				break
			}
		}
		if isText {
			return value
		}
	}
	return binaryLDAPIDPrefix + base64.RawURLEncoding.EncodeToString([]byte(value))
}

func rawLDAPID(value string) string {
	if !strings.HasPrefix(value, binaryLDAPIDPrefix) {
		return value
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, binaryLDAPIDPrefix))
	if err != nil {
		return value
	}
	return string(decoded)
}

func groupName(dn string) string {
	parsed, err := ldap.ParseDN(dn)
	if err != nil || len(parsed.RDNs) == 0 || len(parsed.RDNs[0].Attributes) == 0 {
		return ""
	}
	return parsed.RDNs[0].Attributes[0].Value
}
func (p *Provider) signProfile(user profile) string {
	b, _ := json.Marshal(user)
	payload := base64.RawURLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, p.key)
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (p *Provider) verifyProfile(ctx context.Context, token string) (profile, error) {
	payload, signature, ok := strings.Cut(token, ".")
	if !ok {
		return profile{}, errors.New("malformed token")
	}
	got, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return profile{}, err
	}
	mac := hmac.New(sha256.New, p.key)
	mac.Write([]byte(payload))
	if !hmac.Equal(got, mac.Sum(nil)) {
		return profile{}, errors.New("invalid token")
	}
	b, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return profile{}, err
	}
	var user profile
	if err := json.Unmarshal(b, &user); err != nil {
		return profile{}, err
	}
	if p.gatewayClient != nil {
		config, err := p.config(ctx)
		if err != nil || config.revision() != user.ConfigRevision {
			return profile{}, errors.New("LDAP session is no longer valid")
		}
		active, err := p.gatewayClient.LDAPIdentityActive(ctx, system.DefaultNamespace, ProviderName, user.ID)
		if err != nil || !active {
			return profile{}, errors.New("LDAP identity is disabled")
		}
	}
	return user, nil
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b), err
}
func redirectTarget(rd string) string {
	if !strings.HasPrefix(rd, "/") || strings.HasPrefix(rd, "//") {
		return "/"
	}
	return rd
}
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}
func cookieValue(header map[string][]string, name string) string {
	r := http.Request{Header: http.Header(header)}
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}
func readBody(r *http.Request) string { b, _ := io.ReadAll(r.Body); return string(b) }
func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
func (p *Provider) loginFailed(w http.ResponseWriter, r *http.Request, rd, message string) {
	http.Redirect(w, r, LoginPath+"?rd="+url.QueryEscape(rd)+"&error="+url.QueryEscape(message), http.StatusFound)
}
