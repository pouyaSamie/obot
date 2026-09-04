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
	"strings"
	"sync"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/obot-platform/obot/pkg/auth"
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
	EmailAttributeConfigKey     = "OBOT_LDAP_EMAIL_ATTRIBUTE"
	NameAttributeConfigKey      = "OBOT_LDAP_NAME_ATTRIBUTE"
	GroupAttributeConfigKey     = "OBOT_LDAP_GROUP_ATTRIBUTE"
	StartTLSConfigKey           = "OBOT_LDAP_START_TLS"
	InsecureSkipVerifyConfigKey = "OBOT_LDAP_INSECURE_SKIP_VERIFY"
	CACertificateConfigKey      = "OBOT_LDAP_CA_CERTIFICATE"
)

// Config is loaded from the encrypted auth-provider credential store.
type Config struct {
	URL                string
	BindDN             string
	BindPassword       string
	UserBaseDN         string
	UserFilter         string
	UserIDAttribute    string
	EmailAttribute     string
	NameAttribute      string
	GroupAttribute     string
	StartTLS           bool
	InsecureSkipVerify bool
	CACertificate      string
}

func (c Config) normalized() Config {
	if c.UserFilter == "" {
		c.UserFilter = "(&(objectClass=person)(uid={username}))"
	}
	if c.UserIDAttribute == "" {
		c.UserIDAttribute = "uid"
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
		EmailAttribute:     values[EmailAttributeConfigKey],
		NameAttribute:      values[NameAttributeConfigKey],
		GroupAttribute:     values[GroupAttributeConfigKey],
		StartTLS:           values[StartTLSConfigKey] == "true",
		InsecureSkipVerify: values[InsecureSkipVerifyConfigKey] == "true",
		CACertificate:      strings.ReplaceAll(values[CACertificateConfigKey], "\\n", "\n"),
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
	if err != nil || (u.Scheme != "ldap" && u.Scheme != "ldaps") {
		return Config{}, fmt.Errorf("invalid LDAP URL %q", c.URL)
	}
	if c.StartTLS && u.Scheme != "ldap" {
		return Config{}, errors.New("StartTLS requires an ldap:// URL")
	}
	return c, nil
}

type profile struct {
	ID, Username, Email, Name string
	Groups                    []string
}
type session struct {
	profile   profile
	expiresAt time.Time
}

type Provider struct {
	loadConfig ConfigLoader
	serverURL string
	key       []byte
	mu        sync.Mutex
	sessions  map[string]session
}

func New(serverURL string, loadConfig ConfigLoader) (*Provider, error) {
	if loadConfig == nil {
		return nil, errors.New("LDAP configuration loader is required")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return &Provider{loadConfig: loadConfig, serverURL: serverURL, key: key, sessions: map[string]session{}}, nil
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
	return url.URL{Scheme: "http", Host: listener.Addr().String()}, nil
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
	user, err := p.authenticate(r.Context(), username, password)
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
	p.mu.Lock()
	p.sessions[token] = session{profile: user, expiresAt: expires}
	p.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: auth.ObotAccessTokenCookie, Value: token, Path: "/", Expires: expires, HttpOnly: true, Secure: strings.HasPrefix(p.serverURL, "https://"), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, rd, http.StatusFound)
}

func (p *Provider) signOut(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.ObotAccessTokenCookie); err == nil {
		p.mu.Lock()
		delete(p.sessions, c.Value)
		p.mu.Unlock()
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
	s, ok := p.session(cookieValue(request.Header, auth.ObotAccessTokenCookie))
	if !ok {
		http.Error(w, "record not found", http.StatusInternalServerError)
		return
	}
	writeJSON(w, auth.SerializableState{ExpiresOn: &s.expiresAt, AccessToken: p.signProfile(s.profile), User: s.profile.ID, PreferredUsername: s.profile.Username, Email: s.profile.Email})
}

func (p *Provider) getUserInfo(w http.ResponseWriter, r *http.Request) {
	user, err := p.verifyProfile(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
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

func (p *Provider) session(token string) (session, bool) {
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
	conn, err := p.connect(config)
	if err != nil {
		return profile{}, err
	}
	defer conn.Close()
	if err := conn.Bind(config.BindDN, config.BindPassword); err != nil {
		return profile{}, err
	}
	filter := strings.ReplaceAll(config.UserFilter, "{username}", ldap.EscapeFilter(username))
	attrs := []string{config.UserIDAttribute, config.EmailAttribute, config.NameAttribute, config.GroupAttribute}
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
	filter := fmt.Sprintf("(%s=%s)", config.UserIDAttribute, ldap.EscapeFilter(id))
	attrs := []string{config.UserIDAttribute, config.EmailAttribute, config.NameAttribute, config.GroupAttribute}
	result, err := conn.Search(ldap.NewSearchRequest(config.UserBaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 10, false, filter, attrs, nil))
	if err != nil || len(result.Entries) != 1 {
		return profile{}, errors.New("LDAP user not found")
	}
	return profileFromEntry(result.Entries[0], id, config), nil
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
	id := entry.GetAttributeValue(config.UserIDAttribute)
	if id == "" {
		id = entry.DN
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
	return profile{ID: id, Username: id, Email: email, Name: name, Groups: groups}
}

func groupName(dn string) string {
	parts := strings.SplitN(dn, ",", 2)
	if len(parts) == 0 {
		return ""
	}
	_, value, ok := strings.Cut(parts[0], "=")
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}
func (p *Provider) signProfile(user profile) string {
	b, _ := json.Marshal(user)
	payload := base64.RawURLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, p.key)
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (p *Provider) verifyProfile(token string) (profile, error) {
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
	return user, json.Unmarshal(b, &user)
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
