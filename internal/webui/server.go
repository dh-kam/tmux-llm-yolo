package webui

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dh-kam/tmux-llm-yolo/internal/i18n"
	"github.com/dh-kam/tmux-llm-yolo/internal/tmux"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	defaultWebMaxTerminals = 3
	defaultWebPollInterval = 700 * time.Millisecond
	sessionCookieName      = "r8_watcher_session"
	sessionTokenBytes      = 24
	oauthStateBytes        = 16
	maxCaptureLines        = 500
	oauthStateTTL          = 10 * time.Minute
	webSessionDefaultTTL   = 24 * time.Hour
)

type Config struct {
	ListenAddr         string
	PollInterval       time.Duration
	MaxTerminals       int
	Locale             string
	OAuthBaseURL       string
	GoogleClientID     string
	GoogleClientSecret string
	GitHubClientID     string
	GitHubClientSecret string
	AllowedEmails      []string
	AdminEmails        []string
	AllowedSessions    []string
	TmuxCommand        string
	CookieSecure       bool
	SessionTTL         time.Duration
}

type Server struct {
	cfg          Config
	tmuxClient   tmux.API
	providers    map[string]*oauth2.Config
	providerList []string
	oauthStates  map[string]oauthState
	sessions     map[string]*webSession

	allowedEmails   map[string]struct{}
	adminEmails     map[string]struct{}
	allowedSessions map[string]struct{}

	stateMu   sync.Mutex
	sessionMu sync.Mutex
	router    *gin.Engine
}

type oauthState struct {
	Provider  string
	ExpiresAt time.Time
}

type webSession struct {
	Provider  string
	UserID    string
	Email     string
	Name      string
	Role      string
	CanWrite  bool
	ExpiresAt time.Time
}

type userInfo struct {
	ID    string
	Email string
	Name  string
}

type wsMessage struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

type googleProfile struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

type githubProfile struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type githubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func New(cfg Config) (*Server, error) {
	cfg.Locale = i18n.NormalizeLocale(cfg.Locale)
	cfg.ListenAddr = strings.TrimSpace(cfg.ListenAddr)
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultWebPollInterval
	}
	if cfg.MaxTerminals <= 0 {
		cfg.MaxTerminals = defaultWebMaxTerminals
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = webSessionDefaultTTL
	}

	baseURL := strings.TrimRight(cfg.OAuthBaseURL, "/")
	providers := make(map[string]*oauth2.Config)
	providerList := []string{}
	if cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "" {
		providers["google"] = &oauth2.Config{
			ClientID:     cfg.GoogleClientID,
			ClientSecret: cfg.GoogleClientSecret,
			Endpoint:     google.Endpoint,
			RedirectURL:  baseURL + "/auth/google/callback",
			Scopes:       []string{"openid", "email", "profile"},
		}
		providerList = append(providerList, "google")
	}
	if cfg.GitHubClientID != "" && cfg.GitHubClientSecret != "" {
		providers["github"] = &oauth2.Config{
			ClientID:     cfg.GitHubClientID,
			ClientSecret: cfg.GitHubClientSecret,
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://github.com/login/oauth/authorize",
				TokenURL: "https://github.com/login/oauth/access_token",
			},
			RedirectURL: baseURL + "/auth/github/callback",
			Scopes:      []string{"read:user", "user:email"},
		}
		providerList = append(providerList, "github")
	}

	if len(providers) == 0 {
		return nil, errors.New(i18n.T(cfg.Locale, "web.error_provider_credentials_required"))
	}
	if baseURL == "" {
		return nil, errors.New(i18n.T(cfg.Locale, "web.error_oauth_base_url_required"))
	}

	client, err := tmux.NewWithCommand(cfg.TmuxCommand)
	if err != nil {
		return nil, err
	}

	sort.Strings(providerList)
	return &Server{
		cfg:             cfg,
		tmuxClient:      client,
		providers:       providers,
		providerList:    providerList,
		oauthStates:     map[string]oauthState{},
		sessions:        map[string]*webSession{},
		allowedEmails:   toLowerSet(cfg.AllowedEmails),
		adminEmails:     toLowerSet(cfg.AdminEmails),
		allowedSessions: toLowerSet(cfg.AllowedSessions),
	}, nil
}

func (s *Server) t(key string, args ...interface{}) string {
	return i18n.T(s.cfg.Locale, key, args...)
}

func (s *Server) Run() error {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.Logger())
	r.NoRoute(func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/")
	})

	r.GET("/login", s.handleLogin)
	r.GET("/auth/:provider/login", s.handleOAuthLogin)
	r.GET("/auth/:provider/callback", s.handleOAuthCallback)
	r.GET("/logout", s.handleLogout)
	r.GET("/", s.requireAuth(s.handleDashboard))
	r.GET("/api/user", s.requireAuth(s.handleUserAPI))
	r.GET("/api/sessions", s.requireAuth(s.handleSessionList))
	r.GET("/api/sessions/available", s.requireAuth(s.handleAvailableSessions))
	r.GET("/ws/:session", s.requireAuthWS(s.handleTerminalWS))

	s.router = r
	return r.Run(s.cfg.ListenAddr)
}

func (s *Server) handleLogin(c *gin.Context) {
	if s.getSessionFromRequest(c) != nil {
		c.Redirect(http.StatusFound, "/")
		return
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, loginPageHTML(s.cfg.Locale, s.providerList))
}

func (s *Server) handleDashboard(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, dashboardHTML(s.cfg.Locale, s.cfg.MaxTerminals))
}

func (s *Server) handleUserAPI(c *gin.Context) {
	session := s.getSessionFromRequest(c)
	if session == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": s.t("web.error_unauthorized")})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"email":      session.Email,
		"name":       session.Name,
		"role":       session.Role,
		"provider":   session.Provider,
		"can_write":  session.CanWrite,
		"expires_at": session.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleSessionList(c *gin.Context) {
	session := s.getSessionFromRequest(c)
	if session == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": s.t("web.error_unauthorized")})
		return
	}

	sessions, err := s.tmuxClient.ListSessions(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": s.t("web.error_tmux_list_failed", err)})
		return
	}

	available := make([]string, 0, len(sessions))
	for _, item := range sessions {
		if item.Name == "" {
			continue
		}
		if s.canAccessSession(item.Name) {
			available = append(available, item.Name)
		}
	}

	limit := parseIntQuery(c, "limit", 0)
	if limit > 0 && len(available) > limit {
		available = available[:limit]
	}

	c.JSON(http.StatusOK, gin.H{
		"can_write":     session.CanWrite,
		"sessions":      available,
		"max_terminals": s.cfg.MaxTerminals,
	})
}

func (s *Server) handleAvailableSessions(c *gin.Context) {
	session := s.getSessionFromRequest(c)
	if session == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": s.t("web.error_unauthorized")})
		return
	}
	limit := parseIntQuery(c, "limit", s.cfg.MaxTerminals)
	if limit <= 0 {
		limit = s.cfg.MaxTerminals
	}
	sessions, err := s.tmuxClient.ListSessions(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": s.t("web.error_tmux_list_failed", err)})
		return
	}
	available := make([]string, 0, len(sessions))
	for _, item := range sessions {
		if item.Name == "" {
			continue
		}
		if s.canAccessSession(item.Name) {
			available = append(available, item.Name)
		}
	}
	if len(available) > limit {
		available = available[:limit]
	}
	c.JSON(http.StatusOK, gin.H{"sessions": available})
}

func (s *Server) handleLogout(c *gin.Context) {
	token, _ := c.Cookie(sessionCookieName)
	if token != "" {
		s.sessionMu.Lock()
		delete(s.sessions, token)
		s.sessionMu.Unlock()
	}
	c.SetCookie(sessionCookieName, "", -1, "/", "", s.cfg.CookieSecure, true)
	c.Redirect(http.StatusFound, "/login")
}

func (s *Server) handleOAuthLogin(c *gin.Context) {
	provider := strings.ToLower(strings.TrimSpace(c.Param("provider")))
	conf, ok := s.providers[provider]
	if !ok {
		c.String(http.StatusNotFound, s.t("web.error_provider_unsupported"))
		return
	}

	state, err := randomToken(oauthStateBytes)
	if err != nil {
		c.String(http.StatusInternalServerError, s.t("web.error_state_generation_failed"))
		return
	}

	s.saveOAuthState(state, provider)
	authURL := conf.AuthCodeURL(state, oauth2.AccessTypeOffline)
	c.Redirect(http.StatusFound, authURL)
}

func (s *Server) handleOAuthCallback(c *gin.Context) {
	provider := strings.ToLower(strings.TrimSpace(c.Param("provider")))
	conf, ok := s.providers[provider]
	if !ok {
		c.String(http.StatusNotFound, s.t("web.error_provider_unsupported"))
		return
	}
	state := strings.TrimSpace(c.Query("state"))
	code := strings.TrimSpace(c.Query("code"))
	if state == "" || code == "" {
		c.String(http.StatusBadRequest, s.t("web.error_state_or_code_missing"))
		return
	}
	if err := s.consumeOAuthState(state, provider); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	ctx := c.Request.Context()
	token, err := conf.Exchange(ctx, code)
	if err != nil {
		c.String(http.StatusBadRequest, s.t("web.error_oauth_exchange_failed", err))
		return
	}

	user, err := s.fetchUserFromProvider(ctx, provider, conf.Client(ctx, token))
	if err != nil {
		c.String(http.StatusUnauthorized, s.t("web.error_user_fetch_failed", err))
		return
	}
	email := strings.ToLower(strings.TrimSpace(user.Email))
	if email == "" {
		c.String(http.StatusBadRequest, s.t("web.error_email_missing"))
		return
	}
	if !s.isEmailAllowed(email) {
		c.String(http.StatusForbidden, s.t("web.error_email_not_allowed"))
		return
	}

	isAdmin := s.isAdminEmail(email)
	sessionToken, err := randomToken(sessionTokenBytes)
	if err != nil {
		c.String(http.StatusInternalServerError, s.t("web.error_session_token_failed"))
		return
	}

	expires := time.Now().Add(s.cfg.SessionTTL)
	s.sessionMu.Lock()
	s.sessions[sessionToken] = &webSession{
		Provider:  provider,
		UserID:    user.ID,
		Email:     email,
		Name:      user.Name,
		Role:      roleFromAdmin(isAdmin),
		CanWrite:  isAdmin,
		ExpiresAt: expires,
	}
	s.sessionMu.Unlock()

	c.SetCookie(sessionCookieName, sessionToken, int(s.cfg.SessionTTL.Seconds()), "/", "", s.cfg.CookieSecure, true)
	c.Redirect(http.StatusFound, "/")
}

func (s *Server) handleTerminalWS(c *gin.Context) {
	wsSession := c.MustGet("user-session").(*webSession)
	sessionName := strings.TrimSpace(c.Param("session"))
	if sessionName == "" {
		c.String(http.StatusBadRequest, s.t("web.error_session_name_empty"))
		return
	}

	if !s.canAccessSession(sessionName) {
		c.String(http.StatusForbidden, s.t("web.error_session_access_denied"))
		return
	}

	exists, err := s.tmuxClient.HasSession(c.Request.Context(), sessionName)
	if err != nil {
		c.String(http.StatusBadGateway, s.t("web.error_tmux_session_lookup_failed", err))
		return
	}
	if !exists {
		c.String(http.StatusNotFound, s.t("web.error_session_not_found"))
		return
	}

	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx := c.Request.Context()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, msgData, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg wsMessage
			if err := json.Unmarshal(msgData, &msg); err != nil {
				continue
			}
			if msg.Type != "input" {
				continue
			}
			if !wsSession.CanWrite {
				_ = conn.WriteJSON(wsMessage{Type: "error", Data: s.t("web.error_read_only")})
				continue
			}
			if err := s.sendInputToTmux(ctx, sessionName, msg.Data); err != nil {
				_ = conn.WriteJSON(wsMessage{Type: "error", Data: s.t("web.error_input_forward_failed", err)})
			}
		}
	}()

	var lastCapture string
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()

	for {
		capture, err := s.tmuxClient.CapturePane(ctx, sessionName, maxCaptureLines, true)
		if err == nil && capture != lastCapture {
			lastCapture = capture
			if err := conn.WriteJSON(wsMessage{Type: "output", Data: capture}); err != nil {
				return
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			continue
		}
	}
}

func (s *Server) sendInputToTmux(ctx context.Context, sessionName string, data string) error {
	if data == "" {
		return nil
	}

	if data == "\u0003" {
		return s.tmuxClient.SendKeys(ctx, sessionName, "C-c")
	}

	text := strings.ReplaceAll(data, "\r", "\n")
	parts := strings.Split(text, "\n")
	for idx, part := range parts {
		part = strings.TrimSuffix(part, "\x00")
		part = strings.ReplaceAll(part, "\r", "")
		if part != "" {
			if err := s.tmuxClient.SendKeys(ctx, sessionName, "-l", part); err != nil {
				return err
			}
		}
		if idx < len(parts)-1 {
			if err := s.tmuxClient.SendKeys(ctx, sessionName, "C-m"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) fetchUserFromProvider(ctx context.Context, provider string, client *http.Client) (userInfo, error) {
	switch provider {
	case "google":
		return s.fetchGoogleUser(ctx, client)
	case "github":
		return s.fetchGitHubUser(ctx, client)
	default:
		return userInfo{}, errors.New(s.t("web.error_provider_unsupported"))
	}
}

func (s *Server) fetchGoogleUser(ctx context.Context, client *http.Client) (userInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return userInfo{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return userInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return userInfo{}, errors.New(s.t("web.error_google_userinfo", resp.Status, strings.TrimSpace(string(body))))
	}

	var profile googleProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return userInfo{}, err
	}
	if profile.Sub == "" {
		return userInfo{}, errors.New(s.t("web.error_google_id_missing"))
	}
	return userInfo{
		ID:    profile.Sub,
		Email: profile.Email,
		Name:  profile.Name,
	}, nil
}

func (s *Server) fetchGitHubUser(ctx context.Context, client *http.Client) (userInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return userInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "r8-watcher-webui")

	resp, err := client.Do(req)
	if err != nil {
		return userInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return userInfo{}, errors.New(s.t("web.error_github_userinfo", resp.Status, strings.TrimSpace(string(body))))
	}

	var profile githubProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return userInfo{}, err
	}

	email := profile.Email
	if email == "" {
		email = s.fetchPrimaryGitHubEmail(ctx, client)
		if email == "" && profile.Login != "" {
			email = strings.ToLower(profile.Login + "@users.noreply.github.com")
		}
	}
	if profile.ID == 0 {
		return userInfo{}, errors.New(s.t("web.error_github_id_missing"))
	}
	if profile.Name == "" {
		profile.Name = profile.Login
	}
	return userInfo{
		ID:    strconv.FormatInt(profile.ID, 10),
		Email: email,
		Name:  profile.Name,
	}, nil
}

func (s *Server) fetchPrimaryGitHubEmail(ctx context.Context, client *http.Client) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user/emails", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "r8-watcher-webui")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var emails []githubEmail
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return ""
	}
	for _, item := range emails {
		if item.Primary && item.Verified && item.Email != "" {
			return item.Email
		}
	}
	for _, item := range emails {
		if item.Verified && item.Email != "" {
			return item.Email
		}
	}
	return ""
}

func (s *Server) canAccessSession(name string) bool {
	if len(s.allowedSessions) == 0 {
		return true
	}
	_, ok := s.allowedSessions[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func (s *Server) isEmailAllowed(email string) bool {
	if len(s.allowedEmails) == 0 {
		return true
	}
	_, ok := s.allowedEmails[strings.ToLower(email)]
	return ok
}

func (s *Server) isAdminEmail(email string) bool {
	if len(s.adminEmails) == 0 {
		return true
	}
	_, ok := s.adminEmails[strings.ToLower(email)]
	return ok
}

func (s *Server) saveOAuthState(state, provider string) {
	s.stateMu.Lock()
	s.oauthStates[state] = oauthState{
		Provider:  provider,
		ExpiresAt: time.Now().Add(oauthStateTTL),
	}
	s.stateMu.Unlock()
}

func (s *Server) consumeOAuthState(state, provider string) error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	item, ok := s.oauthStates[state]
	if !ok {
		return errors.New(s.t("web.error_state_invalid"))
	}
	delete(s.oauthStates, state)

	if time.Now().After(item.ExpiresAt) {
		return errors.New(s.t("web.error_state_expired"))
	}
	if item.Provider != provider {
		return errors.New(s.t("web.error_provider_mismatch"))
	}
	return nil
}

func (s *Server) getSessionFromRequest(c *gin.Context) *webSession {
	token, err := c.Cookie(sessionCookieName)
	if err != nil || strings.TrimSpace(token) == "" {
		return nil
	}
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	session, ok := s.sessions[token]
	if !ok || session == nil {
		delete(s.sessions, token)
		return nil
	}
	if time.Now().After(session.ExpiresAt) {
		delete(s.sessions, token)
		return nil
	}
	return session
}

func (s *Server) requireAuth(handler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := s.getSessionFromRequest(c)
		if session == nil {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
		c.Set("user-session", session)
		handler(c)
	}
}

func (s *Server) requireAuthWS(handler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := s.getSessionFromRequest(c)
		if session == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": s.t("web.error_unauthorized")})
			c.Abort()
			return
		}
		c.Set("user-session", session)
		handler(c)
	}
}

func roleFromAdmin(isAdmin bool) string {
	if isAdmin {
		return "admin"
	}
	return "viewer"
}

func parseIntQuery(c *gin.Context, key string, fallback int) int {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func randomToken(byteLen int) (string, error) {
	raw := make([]byte, byteLen)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func toLowerSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, item := range values {
		item = strings.TrimSpace(strings.ToLower(item))
		if item == "" {
			continue
		}
		result[item] = struct{}{}
	}
	return result
}

func loginPageHTML(locale string, providers []string) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf(`<!doctype html>
<html lang="%s">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width,initial-scale=1" />
  <title>%s</title>
  <style>
    :root { font-family: Inter, Arial, sans-serif; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: linear-gradient(160deg, #0f172a, #1e293b); color: #e2e8f0; }
    .card { background: #0f172acc; border: 1px solid #334155; border-radius: 16px; padding: 24px 28px; width: min(520px, 100%% - 32px); box-shadow: 0 20px 50px #020617aa; }
    h1 { margin: 0 0 8px; }
    .providers { display: grid; gap: 12px; margin-top: 16px; }
    .btn { border: 0; border-radius: 8px; padding: 12px 14px; cursor: pointer; color: #fff; text-decoration: none; text-align: center; font-weight: 700; }
    .btn.google { background: #1d4ed8; }
    .btn.github { background: #111827; }
    .note { color: #94a3b8; font-size: 0.95rem; margin-top: 16px; }
  </style>
</head>
<body>
  <main class="card">
    <h1>%s</h1>
    <p class="note">%s</p>
    <div class="providers">
`, locale, i18n.T(locale, "web.html_title"), i18n.T(locale, "web.login_title"), i18n.T(locale, "web.login_note")))

	for _, provider := range providers {
		cssClass := "btn " + provider
		builder.WriteString(fmt.Sprintf("<a class=\"%s\" href=\"/auth/%s/login\">%s %s</a>\n", cssClass, provider, strings.ToUpper(provider), i18n.T(locale, "web.login_action")))
	}
	builder.WriteString(fmt.Sprintf(`    </div>
    <p class="note">%s</p>
  </main>
</body>
</html>`, i18n.T(locale, "web.login_allowed_only")))
	return builder.String()
}

func dashboardHTML(locale string, maxTerminals int) string {
	return fmt.Sprintf(`<!doctype html>
<html lang="%s">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width,initial-scale=1" />
  <title>%s</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/xterm@5.4.0/css/xterm.min.css" />
  <script src="https://cdn.jsdelivr.net/npm/xterm@5.4.0/lib/xterm.min.js"></script>
  <style>
    :root {
      --bg: #020617;
      --panel: #0f172a;
      --text: #e2e8f0;
      --sub: #94a3b8;
      --accent: #38bdf8;
      font-family: 'Trebuchet MS', 'Helvetica Neue', sans-serif;
    }
    * { box-sizing: border-box; }
    body { margin: 0; padding: 0; background: radial-gradient(circle at top, #1e293b 0%%, #0f172a 70%%); color: var(--text); min-height: 100vh; }
    header { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 14px 18px; background: #0f172acc; position: sticky; top: 0; }
    h1 { margin: 0; font-size: 1.2rem; }
    .sub { color: var(--sub); font-size: 0.85rem; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); gap: 12px; padding: 12px; }
    .panel { border: 1px solid #334155; border-radius: 12px; background: var(--panel); min-height: 300px; display: flex; flex-direction: column; overflow: hidden; }
    .panel-head { padding: 10px 12px; display: flex; justify-content: space-between; align-items: center; border-bottom: 1px solid #334155; background: #1e293bcc; font-size: 0.92rem; }
    .terminal-shell { padding: 8px; flex: 1; }
    .terminal-shell:empty::before { content: %q; color: var(--sub); padding: 12px; display: block; }
    .status { color: var(--sub); }
    footer { display: flex; justify-content: center; gap: 10px; padding: 10px; color: var(--sub); }
    .btn { color: var(--text); text-decoration: none; }
  </style>
</head>
<body>
  <header>
    <div>
      <h1>%s</h1>
      <div class="sub" id="user-info">%s</div>
    </div>
    <a class="btn" href="/logout">%s</a>
  </header>
  <div class="grid" id="grid"></div>
  <footer>%s</footer>
  <script>
    const maxTerminals = %d;
    const terminals = [];
    const sockets = [];
    const textWindow = %q;
    const textWaiting = %q;
    const textDisconnected = %q;
    const textUnused = %q;
    const textSession = %q;
    const textConnected = %q;
    const textWebError = %q;
    const textConnectionClosed = %q;
    const textConnectionError = %q;
    const textRole = %q;

    function createPanel(index, label, sessionName, canWrite) {
      const panel = document.createElement('section');
      panel.className = 'panel';
      const head = document.createElement('div');
      head.className = 'panel-head';
      head.innerHTML = '<span>' + textWindow + ' ' + (index + 1) + '</span><span class="status" id="status-' + index + '">' + textWaiting + '</span>';
      const shell = document.createElement('div');
      shell.className = 'terminal-shell';
      panel.appendChild(head);
      panel.appendChild(shell);
      document.getElementById('grid').appendChild(panel);
      if (!sessionName) {
        head.innerHTML = '<span>' + textDisconnected + '</span><span class="status">' + textUnused + '</span>';
        return;
      }
      head.firstChild.textContent = textSession + ': ' + sessionName;
      const terminal = new Terminal({convertEol: true, fontSize: 12, rows: 18, cols: 80});
      terminal.open(shell);
      terminal.focus();
      terminal.options.theme = {
        background: '#020617',
        foreground: '#e2e8f0'
      };
      terminal.onData((data) => {
        if (!canWrite) return;
        socket.send(JSON.stringify({type: 'input', data}));
      });
      const wsProtocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
      const socket = new WebSocket(wsProtocol + '//' + window.location.host + '/ws/' + encodeURIComponent(sessionName));
      socket.onopen = () => {
        statusElement(index, textConnected);
      };
      socket.onmessage = (event) => {
        const payload = JSON.parse(event.data);
        if (payload.type === 'output') {
          terminal.write(payload.data);
        }
        if (payload.type === 'error') {
          terminal.write('\\r\\n[' + textWebError + '] ' + payload.data + '\\r\\n');
        }
      };
      socket.onclose = () => {
        statusElement(index, textConnectionClosed);
        terminal.write('\\r\\n[' + textConnectionClosed + ']\\r\\n');
      };
      socket.onerror = () => statusElement(index, textConnectionError);
      terminals.push(terminal);
      sockets.push(socket);
    }

    function statusElement(index, status) {
      const item = document.getElementById('status-' + index);
      if (item) item.textContent = status;
    }

    async function init() {
      const userResponse = await fetch('/api/user');
      if (!userResponse.ok) {
        location.href = '/login';
        return;
      }
      const user = await userResponse.json();
      document.getElementById('user-info').textContent = user.name + ' (' + user.email + ') / ' + textRole + '=' + user.role;
      const sessionsResponse = await fetch('/api/sessions?limit=' + maxTerminals);
      if (!sessionsResponse.ok) return;
      const payload = await sessionsResponse.json();
      const sessions = (payload.sessions || []);
      for (let i = 0; i < maxTerminals; i++) {
        createPanel(i, textWindow + ' ' + (i + 1), sessions[i], !!payload.can_write);
      }
    }

    window.addEventListener('beforeunload', () => {
      sockets.forEach((socket) => socket.close());
      terminals.forEach((terminal) => terminal.dispose());
    });
    init();
  </script>
</body>
</html>`, locale, i18n.T(locale, "web.html_title"), i18n.T(locale, "web.dashboard_disconnected"), i18n.T(locale, "web.dashboard_title"), i18n.T(locale, "web.dashboard_user_loading"), i18n.T(locale, "web.logout"), i18n.T(locale, "web.dashboard_footer", maxTerminals), maxTerminals, i18n.T(locale, "web.dashboard_window"), i18n.T(locale, "web.dashboard_waiting"), i18n.T(locale, "web.dashboard_disconnected"), i18n.T(locale, "web.dashboard_unused"), i18n.T(locale, "web.dashboard_session"), i18n.T(locale, "web.dashboard_connected"), i18n.T(locale, "web.dashboard_error_label"), i18n.T(locale, "web.dashboard_connection_closed"), i18n.T(locale, "web.dashboard_connection_error"), i18n.T(locale, "web.dashboard_role"))
}
