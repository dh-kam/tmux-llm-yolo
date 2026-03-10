package webui

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dh-kam/tmux-llm-yolo/internal/tmux"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	defaultWebMaxTerminals   = 3
	defaultWebPollInterval   = 700 * time.Millisecond
	sessionCookieName        = "r8_watcher_session"
	sessionTokenBytes        = 24
	oauthStateBytes          = 16
	maxCaptureLines          = 500
	oauthStateTTL           = 10 * time.Minute
	webSessionDefaultTTL     = 24 * time.Hour
)

type Config struct {
	ListenAddr         string
	PollInterval       time.Duration
	MaxTerminals       int
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
		return nil, fmt.Errorf("at least one OAuth provider credential must be configured")
	}
	if baseURL == "" {
		return nil, fmt.Errorf("WEB_OAUTH_BASE_URL must be set for oauth callback URL")
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
	c.String(http.StatusOK, loginPageHTML(s.providerList))
}

func (s *Server) handleDashboard(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, dashboardHTML(s.cfg.MaxTerminals))
}

func (s *Server) handleUserAPI(c *gin.Context) {
	session := s.getSessionFromRequest(c)
	if session == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
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
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	sessions, err := s.tmuxClient.ListSessions(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("tmux 목록 조회 실패: %s", err)})
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
		"can_write":    session.CanWrite,
		"sessions":     available,
		"max_terminals": s.cfg.MaxTerminals,
	})
}

func (s *Server) handleAvailableSessions(c *gin.Context) {
	session := s.getSessionFromRequest(c)
	if session == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	limit := parseIntQuery(c, "limit", s.cfg.MaxTerminals)
	if limit <= 0 {
		limit = s.cfg.MaxTerminals
	}
	sessions, err := s.tmuxClient.ListSessions(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("tmux 목록 조회 실패: %s", err)})
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
		c.String(http.StatusNotFound, "지원하지 않는 provider")
		return
	}

	state, err := randomToken(oauthStateBytes)
	if err != nil {
		c.String(http.StatusInternalServerError, "state 생성 실패")
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
		c.String(http.StatusNotFound, "지원하지 않는 provider")
		return
	}
	state := strings.TrimSpace(c.Query("state"))
	code := strings.TrimSpace(c.Query("code"))
	if state == "" || code == "" {
		c.String(http.StatusBadRequest, "state 또는 code가 없습니다")
		return
	}
	if err := s.consumeOAuthState(state, provider); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	ctx := c.Request.Context()
	token, err := conf.Exchange(ctx, code)
	if err != nil {
		c.String(http.StatusBadRequest, fmt.Sprintf("OAuth 토큰 교환 실패: %v", err))
		return
	}

	user, err := s.fetchUserFromProvider(ctx, provider, conf.Client(ctx, token))
	if err != nil {
		c.String(http.StatusUnauthorized, fmt.Sprintf("사용자 조회 실패: %v", err))
		return
	}
	email := strings.ToLower(strings.TrimSpace(user.Email))
	if email == "" {
		c.String(http.StatusBadRequest, "이메일 정보를 가져오지 못했습니다")
		return
	}
	if !s.isEmailAllowed(email) {
		c.String(http.StatusForbidden, "허용되지 않은 이메일 계정입니다")
		return
	}

	isAdmin := s.isAdminEmail(email)
	sessionToken, err := randomToken(sessionTokenBytes)
	if err != nil {
		c.String(http.StatusInternalServerError, "세션 토큰 생성 실패")
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
		c.String(http.StatusBadRequest, "session 값이 비어 있습니다")
		return
	}

	if !s.canAccessSession(sessionName) {
		c.String(http.StatusForbidden, "접근이 허용되지 않은 세션입니다")
		return
	}

	exists, err := s.tmuxClient.HasSession(c.Request.Context(), sessionName)
	if err != nil {
		c.String(http.StatusBadGateway, fmt.Sprintf("tmux 세션 조회 실패: %v", err))
		return
	}
	if !exists {
		c.String(http.StatusNotFound, "요청한 세션이 존재하지 않습니다")
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
				_ = conn.WriteJSON(wsMessage{Type: "error", Data: "read-only 권한입니다"})
				continue
			}
			if err := s.sendInputToTmux(ctx, sessionName, msg.Data); err != nil {
				_ = conn.WriteJSON(wsMessage{Type: "error", Data: fmt.Sprintf("입력 전달 실패: %s", err)})
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
		return userInfo{}, fmt.Errorf("지원하지 않는 provider")
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
		return userInfo{}, fmt.Errorf("google userinfo 응답 오류: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var profile googleProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return userInfo{}, err
	}
	if profile.Sub == "" {
		return userInfo{}, fmt.Errorf("google 식별값 없음")
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
		return userInfo{}, fmt.Errorf("github user 응답 오류: %s %s", resp.Status, strings.TrimSpace(string(body)))
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
		return userInfo{}, fmt.Errorf("github 식별값 없음")
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
		return fmt.Errorf("요청 state가 유효하지 않습니다")
	}
	delete(s.oauthStates, state)

	if time.Now().After(item.ExpiresAt) {
		return fmt.Errorf("요청 state가 만료되었습니다")
	}
	if item.Provider != provider {
		return fmt.Errorf("provider가 일치하지 않습니다")
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
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
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

func loginPageHTML(providers []string) string {
	var builder strings.Builder
	builder.WriteString(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width,initial-scale=1" />
  <title>tmux web ui</title>
  <style>
    :root { font-family: Inter, Arial, sans-serif; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: linear-gradient(160deg, #0f172a, #1e293b); color: #e2e8f0; }
    .card { background: #0f172acc; border: 1px solid #334155; border-radius: 16px; padding: 24px 28px; width: min(520px, 100% - 32px); box-shadow: 0 20px 50px #020617aa; }
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
    <h1>tmux web UI 로그인</h1>
    <p class="note">OAuth 계정으로 로그인해 tmux 터미널을 보거나 입력할 수 있습니다.</p>
    <div class="providers">
`)

	for _, provider := range providers {
		cssClass := "btn " + provider
		builder.WriteString(fmt.Sprintf("<a class=\"%s\" href=\"/auth/%s/login\">%s 로그인</a>\n", cssClass, provider, strings.ToUpper(provider)))
	}
	builder.WriteString(`    </div>
    <p class="note">허용된 사용자만 접근 가능합니다.</p>
  </main>
</body>
</html>`)
	return builder.String()
}

func dashboardHTML(maxTerminals int) string {
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width,initial-scale=1" />
  <title>tmux web ui</title>
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
    .terminal-shell:empty::before { content: "연결 없음"; color: var(--sub); padding: 12px; display: block; }
    .status { color: var(--sub); }
    footer { display: flex; justify-content: center; gap: 10px; padding: 10px; color: var(--sub); }
    .btn { color: var(--text); text-decoration: none; }
  </style>
</head>
<body>
  <header>
    <div>
      <h1>tmux Web Terminal</h1>
      <div class="sub" id="user-info">사용자 정보를 불러오는 중...</div>
    </div>
    <a class="btn" href="/logout">로그아웃</a>
  </header>
  <div class="grid" id="grid"></div>
  <footer>max %d windows</footer>
  <script>
    const maxTerminals = %d;
    const terminals = [];
    const sockets = [];

    function createPanel(index, label, sessionName, canWrite) {
      const panel = document.createElement('section');
      panel.className = 'panel';
      const head = document.createElement('div');
      head.className = 'panel-head';
      head.innerHTML = '<span>Window ' + (index + 1) + '</span><span class="status" id="status-' + index + '">대기중</span>';
      const shell = document.createElement('div');
      shell.className = 'terminal-shell';
      panel.appendChild(head);
      panel.appendChild(shell);
      document.getElementById('grid').appendChild(panel);
      if (!sessionName) {
        head.innerHTML = '<span>미연결</span><span class="status">미사용</span>';
        return;
      }
      head.firstChild.textContent = 'session: ' + sessionName;
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
        statusElement(index, '연결됨');
      };
      socket.onmessage = (event) => {
        const payload = JSON.parse(event.data);
        if (payload.type === 'output') {
          terminal.write(payload.data);
        }
        if (payload.type === 'error') {
          terminal.write('\\r\\n[웹UI 오류] ' + payload.data + '\\r\\n');
        }
      };
      socket.onclose = () => {
        statusElement(index, '연결 종료');
        terminal.write('\\r\\n[연결이 종료되었습니다]\\r\\n');
      };
      socket.onerror = () => statusElement(index, '연결 오류');
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
      document.getElementById('user-info').textContent = user.name + ' (' + user.email + ') / role=' + user.role;
      const sessionsResponse = await fetch('/api/sessions?limit=' + maxTerminals);
      if (!sessionsResponse.ok) return;
      const payload = await sessionsResponse.json();
      const sessions = (payload.sessions || []);
      for (let i = 0; i < maxTerminals; i++) {
        createPanel(i, 'Window ' + (i + 1), sessions[i], !!payload.can_write);
      }
    }

    window.addEventListener('beforeunload', () => {
      sockets.forEach((socket) => socket.close());
      terminals.forEach((terminal) => terminal.dispose());
    });
    init();
  </script>
</body>
</html>`, maxTerminals, maxTerminals)
}
