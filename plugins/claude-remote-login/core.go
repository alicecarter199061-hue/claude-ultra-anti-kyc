package main

import (
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	pluginID            = "claude-remote-login"
	pluginVersion       = "1.1.0"
	apiBase             = "/v0/management/plugins/" + pluginID
	resourceURL         = "/v0/resource/plugins/" + pluginID + "/login"
	authorizeURL        = "https://claude.com/cai/oauth/authorize"
	redirectURI         = "https://platform.claude.com/oauth/code/callback"
	tokenURL            = "https://platform.claude.com/v1/oauth/token"
	profileURL          = "https://api.anthropic.com/api/oauth/profile"
	clientID            = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	scopeSetupToken     = "user:inference"
	scopeOAuth          = "user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"
	modeSetupToken      = "setup_token"
	modeOAuth           = "oauth"
	expiresInSetupToken = 31536000
	sessionTTL          = 15 * time.Minute
)

type hostCaller func(string, any) (json.RawMessage, error)
type managementRequest struct {
	Method         string
	Path           string
	Body           []byte
	HostCallbackID string `json:"host_callback_id"`
}
type managementResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}
type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string           { return e.message }
func fail(status int, message string) error { return &apiError{status, message} }

type loginSession struct {
	state      string
	verifier   string
	mode       string
	expires    time.Time
	busy       bool
	completed  bool
	credential map[string]any
	filename   string
	email      string
}
type service struct {
	mu       sync.Mutex
	sessions map[string]*loginSession
	call     hostCaller
	now      func() time.Time
}

func newService(call hostCaller) *service {
	return &service{sessions: make(map[string]*loginSession), call: call, now: time.Now}
}
func main() {}

func randomValue() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}
func (s *service) start(requestedMode ...string) (map[string]any, error) {
	mode := modeSetupToken
	if len(requestedMode) > 0 && strings.TrimSpace(requestedMode[0]) != "" {
		mode = strings.TrimSpace(requestedMode[0])
	}
	if mode != modeSetupToken && mode != modeOAuth {
		return nil, fail(400, "Unsupported login mode.")
	}

	state, err := randomValue()
	if err != nil {
		return nil, fail(500, "Unable to generate a login session.")
	}
	verifier, err := randomValue()
	if err != nil {
		return nil, fail(500, "Unable to generate PKCE material.")
	}
	digest := sha256.Sum256([]byte(verifier))

	selectedScope := scopeSetupToken
	if mode == modeOAuth {
		selectedScope = scopeOAuth
	}

	query := url.Values{
		"client_id": {clientID}, "response_type": {"code"}, "code": {"true"},
		"redirect_uri": {redirectURI}, "scope": {selectedScope}, "state": {state},
		"code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])},
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for key, session := range s.sessions {
		if !session.busy && !now.Before(session.expires) {
			delete(s.sessions, key)
		}
	}
	if len(s.sessions) >= 128 {
		return nil, fail(429, "Too many pending logins. Finish or cancel an existing login first.")
	}
	expires := now.Add(sessionTTL)
	s.sessions[state] = &loginSession{state: state, verifier: verifier, mode: mode, expires: expires}
	return map[string]any{
		"state":      state,
		"mode":       mode,
		"url":        authorizeURL + "?" + query.Encode(),
		"expires_at": expires.UTC().Format(time.RFC3339),
	}, nil
}

func parseCode(input, expectedState string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" || len(input) > 4096 || strings.ContainsAny(input, " \t\r\n") {
		return "", fail(400, "Paste the complete authorization code from this login link.")
	}
	state := ""
	code := input
	if strings.Contains(input, "://") {
		u, err := url.Parse(input)
		if err != nil || u.Scheme != "https" || u.Host != "platform.claude.com" || u.Path != "/oauth/code/callback" || u.User != nil {
			return "", fail(400, "Unexpected callback URL. Paste the displayed code instead.")
		}
		code = u.Query().Get("code")
		state = u.Query().Get("state")
	}
	if strings.Contains(code, "#") {
		parts := strings.Split(code, "#")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", fail(400, "Invalid authorization code format.")
		}
		if state != "" && state != parts[1] {
			return "", fail(400, "Conflicting authorization states.")
		}
		code, state = parts[0], parts[1]
	}
	if code == "" || (state != "" && state != expectedState) {
		return "", fail(400, "This code belongs to a different login attempt. Use its original link.")
	}
	return code, nil
}

type upstreamResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

func (s *service) http(callbackID, method, endpoint, token string, body any) (upstreamResponse, error) {
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return upstreamResponse{}, err
		}
	}
	headers := http.Header{"Accept": {"application/json"}}
	if body != nil {
		headers.Set("Content-Type", "application/json")
	}
	if token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	raw, err := s.call("host.http.do", map[string]any{"host_callback_id": callbackID, "method": method, "url": endpoint, "headers": headers, "body": data})
	if err != nil {
		return upstreamResponse{}, fail(502, "Could not reach Claude. Start a new login attempt and retry.")
	}
	var response upstreamResponse
	if json.Unmarshal(raw, &response) != nil {
		return response, fail(502, "Invalid response from the proxy HTTP bridge.")
	}
	return response, nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Account      struct {
		UUID  string `json:"uuid"`
		Email string `json:"email_address"`
	} `json:"account"`
	Organization struct {
		UUID string `json:"uuid"`
		Name string `json:"name"`
	} `json:"organization"`
}

func (s *service) acquire(session *loginSession, code, callbackID string) error {
	request := map[string]any{
		"grant_type":    "authorization_code",
		"code":          code,
		"redirect_uri":  redirectURI,
		"client_id":     clientID,
		"code_verifier": session.verifier,
		"state":         session.state,
	}
	if session.mode == modeSetupToken {
		request["expires_in"] = expiresInSetupToken
	}

	response, err := s.http(callbackID, http.MethodPost, tokenURL, "", request)
	if err != nil {
		return err
	}
	if response.StatusCode != 200 {
		return fail(502, fmt.Sprintf("Claude rejected the authorization code (HTTP %d). Generate a fresh link and retry.", response.StatusCode))
	}
	var tokens tokenResponse
	if json.Unmarshal(response.Body, &tokens) != nil || tokens.AccessToken == "" {
		return fail(502, "Claude returned an incomplete token response. No credential was saved.")
	}
	// 在 oauth 模式下强制要求 refresh_token；setup_token 模式下上游不会下发 refresh_token，故放行
	if session.mode == modeOAuth && (tokens.RefreshToken == "" || tokens.ExpiresIn <= 0 || tokens.ExpiresIn > 365*24*60*60) {
		return fail(502, "Claude returned an incomplete token response. No credential was saved.")
	}

	// 仅在 modeOAuth 模式下调用 profileURL；setup_token 权限仅为 user:inference，调用必报 403
	if session.mode == modeOAuth {
		profile, errProfile := s.http(callbackID, http.MethodGet, profileURL, tokens.AccessToken, nil)
		if errProfile == nil && profile.StatusCode == 200 {
			var identity struct {
				Account struct {
					UUID  string `json:"uuid"`
					Email string `json:"email"`
				} `json:"account"`
				Organization struct {
					UUID string `json:"uuid"`
					Name string `json:"name"`
				} `json:"organization"`
			}
			if json.Unmarshal(profile.Body, &identity) == nil {
				if identity.Account.UUID != "" {
					tokens.Account.UUID = identity.Account.UUID
				}
				if identity.Account.Email != "" {
					tokens.Account.Email = identity.Account.Email
				}
				if identity.Organization.UUID != "" {
					tokens.Organization.UUID = identity.Organization.UUID
				}
				if identity.Organization.Name != "" {
					tokens.Organization.Name = identity.Organization.Name
				}
			}
		}
	}

	if tokens.Account.UUID == "" {
		h := sha1.Sum([]byte("claude-setup-token:" + tokens.AccessToken))
		v67 := (uint16(h[6])<<8 | uint16(h[7]))&0x0fff | 0x5000
		v89 := (uint16(h[8])<<8 | uint16(h[9]))&0x3fff | 0x8000
		tokens.Account.UUID = fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
			h[0:4], h[4:6], v67, v89, h[10:16])
	}
	if tokens.Account.Email == "" {
		accHash := sha256.Sum256([]byte(tokens.Account.UUID))
		tokens.Account.Email = fmt.Sprintf("claude-%s@isolated.local", hex.EncodeToString(accHash[:])[:8])
	}
	// 凭据文件唯一哈希：优先采用 Account.UUID，确保同 Team 下多席位账号互不覆盖
	identity := tokens.Account.UUID
	if identity == "" {
		identity = tokens.Organization.UUID
	}

	digest := sha256.Sum256([]byte(identity))
	emailForFile := strings.Map(func(r rune) rune {
		if r > 127 || strings.ContainsRune(`/\:*?"<>|`, r) || r < 32 {
			return '_'
		}
		return r
	}, tokens.Account.Email)
	filename := "claude-" + hex.EncodeToString(digest[:])[:8] + "-" + emailForFile + ".json"
	credential := make(map[string]any)
	// Preserve existing credential-specific routing settings when reauthorizing.
	if raw, errList := s.call("host.auth.list", map[string]any{}); errList == nil {
		var list struct {
			Files []struct {
				Name      string `json:"name"`
				AuthIndex string `json:"auth_index"`
			} `json:"files"`
		}
		if json.Unmarshal(raw, &list) == nil {
			for _, item := range list.Files {
				if item.Name == filename {
					if raw, errGet := s.call("host.auth.get", map[string]any{"auth_index": item.AuthIndex}); errGet == nil {
						var saved struct {
							JSON json.RawMessage `json:"json"`
						}
						if json.Unmarshal(raw, &saved) == nil {
							_ = json.Unmarshal(saved.JSON, &credential)
						}
					}
					break
				}
			}
		}
	}
	var device [32]byte
	if _, err := rand.Read(device[:]); err != nil {
		return fail(500, "Could not create the credential identity.")
	}
	if credential == nil {
		credential = make(map[string]any)
	}
	if _, ok := credential["claude_device_ids"]; !ok {
		credential["claude_device_ids"] = []string{hex.EncodeToString(device[:])}
	}
	now := s.now().UTC()

	saveExpired := now.Add(time.Duration(tokens.ExpiresIn) * time.Second).Format(time.RFC3339)
	saveRefreshToken := tokens.RefreshToken
	if session.mode == modeSetupToken {
		saveRefreshToken = ""
		// 若上游返回了合法的较短有效秒数（如 90 天等），以较小值为准；否则兜底 365 天 (31536000s)
		effectiveSeconds := int64(expiresInSetupToken)
		if tokens.ExpiresIn > 0 && tokens.ExpiresIn < effectiveSeconds {
			effectiveSeconds = tokens.ExpiresIn
		}
		saveExpired = now.Add(time.Duration(effectiveSeconds) * time.Second).Format(time.RFC3339)
	}

	for key, value := range map[string]any{
		"type":              "claude",
		"access_token":      tokens.AccessToken,
		"refresh_token":     saveRefreshToken,
		"id_token":          "",
		"email":             tokens.Account.Email,
		"account_uuid":      tokens.Account.UUID,
		"organization_uuid": tokens.Organization.UUID,
		"organization_name": tokens.Organization.Name,
		"last_refresh":      now.Format(time.RFC3339),
		"expired":           saveExpired,
	} {
		credential[key] = value
	}

	if session.mode == modeSetupToken {
		credential["is_setup_token"] = true
		credential["skip_account_profile"] = true
		credential["scopes"] = scopeSetupToken
		delete(credential, "refreshToken")
		delete(credential, "refresh_interval")
		delete(credential, "refresh_interval_seconds")
		delete(credential, "refreshInterval")
		delete(credential, "refreshIntervalSeconds")
	} else {
		delete(credential, "is_setup_token")
		delete(credential, "skip_account_profile")
		credential["scopes"] = scopeOAuth
	}

	session.credential = credential
	session.filename = filename
	session.email = tokens.Account.Email
	session.verifier = ""
	return nil
}

func (s *service) complete(state, input, callbackID string) (map[string]any, error) {
	s.mu.Lock()
	session, ok := s.sessions[state]
	if !ok || !s.now().Before(session.expires) {
		s.mu.Unlock()
		return nil, fail(404, "This login expired or does not exist. Generate a fresh link.")
	}
	if session.completed {
		email := session.email
		s.mu.Unlock()
		return map[string]any{"status": "connected", "email": email}, nil
	}
	if session.busy {
		s.mu.Unlock()
		return nil, fail(409, "This login is already being processed.")
	}
	code, errCode := parseCode(input, state)
	if session.credential == nil && errCode != nil {
		s.mu.Unlock()
		return nil, errCode
	}
	session.busy = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); session.busy = false; s.mu.Unlock() }()
	if session.credential == nil {
		if err := s.acquire(session, code, callbackID); err != nil {
			s.mu.Lock()
			delete(s.sessions, state)
			s.mu.Unlock()
			return nil, err
		}
	}
	raw, errJSON := json.Marshal(session.credential)
	if errJSON != nil {
		return nil, fail(500, "Unable to serialize the credential.")
	}
	_, errSave := s.call("host.auth.save", map[string]any{"name": session.filename, "json": json.RawMessage(raw)})
	if errSave != nil {
		return nil, fail(503, "Claude authorized successfully, but saving failed. Click Connect again to retry saving this same login.")
	}
	s.mu.Lock()
	session.completed = true
	session.credential = nil
	session.expires = s.now().Add(time.Minute)
	s.mu.Unlock()
	return map[string]any{"status": "connected", "email": session.email}, nil
}

func (s *service) cancel(state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session, ok := s.sessions[state]; ok && session.busy {
		return fail(409, "The login is being processed. Wait for it to finish.")
	}
	delete(s.sessions, state)
	return nil
}
func jsonResponse(status int, value any) managementResponse {
	body, _ := json.Marshal(value)
	return managementResponse{StatusCode: status, Headers: http.Header{"Content-Type": {"application/json"}, "Cache-Control": {"no-store"}, "X-Content-Type-Options": {"nosniff"}}, Body: body}
}
func (s *service) handle(req managementRequest) managementResponse {
	if req.Method == http.MethodGet && strings.HasSuffix(req.Path, "/login") {
		return managementResponse{StatusCode: 200, Headers: http.Header{"Content-Type": {"text/html; charset=utf-8"}, "Cache-Control": {"no-store"}, "Referrer-Policy": {"no-referrer"}, "X-Content-Type-Options": {"nosniff"}, "Content-Security-Policy": {"default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; frame-ancestors 'self'"}}, Body: loginPage}
	}
	if req.Method != http.MethodPost {
		return jsonResponse(405, map[string]string{"error": "Use POST for login actions."})
	}
	if len(req.Body) > 8192 {
		return jsonResponse(413, map[string]string{"error": "Request body is too large."})
	}
	var body struct {
		State string `json:"state"`
		Code  string `json:"code"`
		Mode  string `json:"mode"`
	}
	if len(req.Body) > 0 && json.Unmarshal(req.Body, &body) != nil {
		return jsonResponse(400, map[string]string{"error": "Invalid JSON request."})
	}
	var result map[string]any
	var err error
	switch req.Path {
	case apiBase + "/start":
		result, err = s.start(body.Mode)
	case apiBase + "/complete":
		result, err = s.complete(body.State, body.Code, req.HostCallbackID)
	case apiBase + "/cancel":
		err = s.cancel(body.State)
		result = map[string]any{"status": "cancelled"}
	default:
		return jsonResponse(404, map[string]string{"error": "Unknown plugin endpoint."})
	}
	if err != nil {
		var apiErr *apiError
		if errors.As(err, &apiErr) {
			return jsonResponse(apiErr.status, map[string]string{"error": apiErr.message})
		}
		return jsonResponse(500, map[string]string{"error": "Login could not be completed."})
	}
	return jsonResponse(200, result)
}

func (s *service) dispatch(method string, body []byte) (any, error) {
	switch method {
	case "plugin.register", "plugin.reconfigure":
		return map[string]any{
			"schema_version": 6,
			"metadata": map[string]any{
				"Name":             "Claude Remote Login",
				"Version":          pluginVersion,
				"Author":           "alicecarter199061-hue",
				"GitHubRepository": "https://github.com/alicecarter199061-hue/claude-ultra-anti-kyc",
				"ConfigFields":     []any{},
			},
			"capabilities": map[string]bool{"management_api": true},
		}, nil
	case "management.register":
		routes := []map[string]string{}
		for _, action := range []string{"start", "complete", "cancel"} {
			routes = append(routes, map[string]string{"Method": "POST", "Path": apiBase + "/" + action})
		}
		return map[string]any{"routes": routes, "resources": []map[string]string{{"Path": "/login", "Menu": "Claude Remote Login", "Description": "Sign in on another device and paste the returned authorization code."}}}, nil
	case "management.handle":
		var request managementRequest
		if err := json.Unmarshal(body, &request); err != nil {
			return nil, fmt.Errorf("invalid management request")
		}
		return s.handle(request), nil
	default:
		return nil, fmt.Errorf("unsupported plugin method")
	}
}
