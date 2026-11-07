package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeHost struct {
	exchanges int
	saves     int
	failSave  bool
	saved     map[string]any
	body      map[string]any
	gate      chan struct{}
	entered   chan struct{}
}

func raw(value any) json.RawMessage { out, _ := json.Marshal(value); return out }
func (f *fakeHost) call(method string, payload any) (json.RawMessage, error) {
	request := payload.(map[string]any)
	switch method {
	case "host.http.do":
		if request["url"] == tokenURL {
			f.exchanges++
			_ = json.Unmarshal(request["body"].([]byte), &f.body)
			if f.entered != nil {
				close(f.entered)
				<-f.gate
			}
			return raw(upstreamResponse{StatusCode: 200, Body: raw(map[string]any{"access_token": "private-access", "refresh_token": "private-refresh", "expires_in": 3600, "account": map[string]string{"uuid": "account-1", "email_address": "test@example.test"}, "organization": map[string]string{"uuid": "org-1", "name": "Test team"}})}), nil
		}
		return raw(upstreamResponse{StatusCode: 200, Body: raw(map[string]any{"account": map[string]string{"uuid": "account-1", "email": "test@example.test"}, "organization": map[string]string{"uuid": "org-1", "name": "Test team"}})}), nil
	case "host.auth.list":
		return raw(map[string]any{"files": []any{}}), nil
	case "host.auth.save":
		f.saves++
		if f.failSave {
			f.failSave = false
			return nil, errors.New("temporary store failure")
		}
		_ = json.Unmarshal(request["json"].(json.RawMessage), &f.saved)
		return raw(map[string]string{"name": request["name"].(string)}), nil
	default:
		return nil, errors.New("unexpected host call")
	}
}
func TestPKCEAndHostedCallback(t *testing.T) {
	host := &fakeHost{}
	s := newService(host.call)
	flow, err := s.start("oauth")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(flow["url"].(string))
	state := flow["state"].(string)
	digest := sha256.Sum256([]byte(s.sessions[state].verifier))
	if u.Host != "claude.com" || u.Query().Get("redirect_uri") != redirectURI || u.Query().Get("code_challenge") != base64.RawURLEncoding.EncodeToString(digest[:]) {
		t.Fatal("incorrect hosted redirect or PKCE challenge")
	}
	if strings.Contains(string(raw(flow)), s.sessions[state].verifier) {
		t.Fatal("PKCE verifier exposed")
	}
	if _, err := s.complete(state, "the-code#"+state, "callback"); err != nil {
		t.Fatal(err)
	}
	if host.body["redirect_uri"] != u.Query().Get("redirect_uri") || host.body["state"] != state || host.body["code"] != "the-code" {
		t.Fatal("token exchange differs from authorization session")
	}
	if host.saved["type"] != "claude" || host.saved["refresh_token"] != "private-refresh" || host.saved["account_uuid"] != "account-1" {
		t.Fatal("native Claude credential was not saved")
	}
	if host.saved["expired"] == "" || host.saved["claude_device_ids"] == nil {
		t.Fatal("refresh or device metadata missing")
	}
	if _, err := s.complete(state, "the-code#"+state, "callback"); err != nil {
		t.Fatal(err)
	}
	if host.exchanges != 1 || host.saves != 1 {
		t.Fatal("completed code was replayed")
	}
	if s.sessions[state].verifier != "" || s.sessions[state].credential != nil {
		t.Fatal("completed session retained secret material")
	}
}
func TestWrongStateAndExpiredCodeNeverReachClaude(t *testing.T) {
	host := &fakeHost{}
	s := newService(host.call)
	now := time.Unix(1000, 0)
	s.now = func() time.Time { return now }
	flow, _ := s.start()
	state := flow["state"].(string)
	if _, err := s.complete(state, "code#other-state", ""); err == nil {
		t.Fatal("wrong state accepted")
	}
	now = now.Add(sessionTTL)
	if _, err := s.complete(state, "code#"+state, ""); err == nil {
		t.Fatal("expired code accepted")
	}
	if host.exchanges != 0 {
		t.Fatal("invalid session contacted upstream")
	}
}
func TestSaveRetryDoesNotExchangeCodeTwice(t *testing.T) {
	host := &fakeHost{failSave: true}
	s := newService(host.call)
	flow, _ := s.start()
	state := flow["state"].(string)
	if _, err := s.complete(state, "code#"+state, ""); err == nil {
		t.Fatal("save failure hidden")
	}
	if _, err := s.complete(state, "", ""); err != nil {
		t.Fatal(err)
	}
	if host.exchanges != 1 || host.saves != 2 {
		t.Fatal("save retry repeated token exchange")
	}
}
func TestConcurrentCompletionIsRejected(t *testing.T) {
	host := &fakeHost{gate: make(chan struct{}), entered: make(chan struct{})}
	s := newService(host.call)
	flow, _ := s.start()
	state := flow["state"].(string)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _, _ = s.complete(state, "code#"+state, "") }()
	<-host.entered
	if _, err := s.complete(state, "code#"+state, ""); err == nil {
		t.Fatal("concurrent completion accepted")
	}
	close(host.gate)
	wg.Wait()
	if host.exchanges != 1 {
		t.Fatal("multiple token exchanges occurred")
	}
}
func TestInputAndPublicResource(t *testing.T) {
	for _, input := range []string{"", "code#wrong", "code#state#extra", "https://evil.example/?code=one&state=state", "code with spaces"} {
		if _, err := parseCode(input, "state"); err == nil {
			t.Fatalf("invalid code accepted: %q", input)
		}
	}
	for _, input := range []string{"code#state", "code", "https://platform.claude.com/oauth/code/callback?code=code&state=state"} {
		if code, err := parseCode(input, "state"); err != nil || code != "code" {
			t.Fatalf("valid input rejected: %v", err)
		}
	}
	s := newService((&fakeHost{}).call)
	page := s.handle(managementRequest{Method: "GET", Path: resourceURL})
	if page.StatusCode != 200 || !strings.Contains(string(page.Body), "Claude Remote Login") || strings.Contains(string(page.Body), "private-refresh") {
		t.Fatal("public resource is invalid")
	}
	if page.Headers.Get("Cache-Control") != "no-store" {
		t.Fatal("credential page is cacheable")
	}
	response := s.handle(managementRequest{Method: "POST", Path: apiBase + "/complete", Body: []byte(`{"state":"missing","code":"private-code"}`)})
	if response.StatusCode != 404 || strings.Contains(string(response.Body), "private-code") {
		t.Fatal("invalid callback leaked its code")
	}
}

func TestSetupTokenDefaultFlowTracerBullet(t *testing.T) {
	fixedTime := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	var profileCalls int
	var tokenRequestBody map[string]any

	host := &fakeHost{}
	s := newService(func(method string, payload any) (json.RawMessage, error) {
		request := payload.(map[string]any)
		switch method {
		case "host.http.do":
			reqURL := request["url"].(string)
			if reqURL == tokenURL {
				_ = json.Unmarshal(request["body"].([]byte), &tokenRequestBody)
				// 模拟上游 setup-token 响应：不含 refresh_token，expires_in 为 1 年
				return raw(upstreamResponse{
					StatusCode: 200,
					Body: raw(map[string]any{
						"access_token": "sk-ant-oat01-long-lived-tracer",
						"token_type":   "bearer",
						"expires_in":   31536000,
						"account": map[string]string{
							"uuid":          "account-tracer-1",
							"email_address": "tracer@test.local",
						},
						"organization": map[string]string{
							"uuid": "org-tracer-1",
							"name": "Tracer Org",
						},
					}),
				}), nil
			}
			if reqURL == profileURL {
				profileCalls++
				return nil, errors.New("profileURL must not be called in setup_token mode")
			}
			return raw(upstreamResponse{StatusCode: 200, Body: raw(map[string]any{})}), nil
		case "host.auth.list":
			return raw(map[string]any{"files": []any{}}), nil
		case "host.auth.save":
			_ = json.Unmarshal(request["json"].(json.RawMessage), &host.saved)
			return raw(map[string]string{"name": request["name"].(string)}), nil
		default:
			return nil, errors.New("unexpected host call")
		}
	})
	s.now = func() time.Time { return fixedTime }

	// 1. 调用 POST /start（空请求体，默认触发 setup_token）
	startReq := managementRequest{
		Method: "POST",
		Path:   apiBase + "/start",
		Body:   []byte(`{}`),
	}
	startResp := s.handle(startReq)
	if startResp.StatusCode != 200 {
		t.Fatalf("start failed with status %d: %s", startResp.StatusCode, string(startResp.Body))
	}

	var startResult map[string]any
	if err := json.Unmarshal(startResp.Body, &startResult); err != nil {
		t.Fatalf("unmarshal start response: %v", err)
	}

	if startResult["mode"] != "setup_token" {
		t.Fatalf("expected mode setup_token, got: %v", startResult["mode"])
	}

	authURL, _ := url.Parse(startResult["url"].(string))
	if authURL.Query().Get("scope") != "user:inference" {
		t.Fatalf("expected scope user:inference, got: %q", authURL.Query().Get("scope"))
	}
	state := startResult["state"].(string)

	// 2. 调用 POST /complete 提交授权码
	completeReq := managementRequest{
		Method:         "POST",
		Path:           apiBase + "/complete",
		HostCallbackID: "cb-1",
		Body:           raw(map[string]string{"state": state, "code": "tracer-code#" + state}),
	}
	completeResp := s.handle(completeReq)
	if completeResp.StatusCode != 200 {
		t.Fatalf("complete failed with status %d: %s", completeResp.StatusCode, string(completeResp.Body))
	}

	// 3. 验证换码请求体包含 expires_in: 31536000
	if exp, ok := tokenRequestBody["expires_in"].(float64); !ok || int64(exp) != 31536000 {
		t.Fatalf("expected token request expires_in 31536000, got: %v", tokenRequestBody["expires_in"])
	}

	// 4. 验证 profileURL 没有被调用
	if profileCalls != 0 {
		t.Fatalf("expected 0 calls to profileURL, got: %d", profileCalls)
	}

	// 5. 验证落盘凭据的精确字面量
	if host.saved["access_token"] != "sk-ant-oat01-long-lived-tracer" {
		t.Fatalf("unexpected access_token: %v", host.saved["access_token"])
	}
	if host.saved["refresh_token"] != "" {
		t.Fatalf("refresh_token must be empty string, got: %v", host.saved["refresh_token"])
	}
	// 字面量精确断言（固定时钟 2026-11-05T00:00:00Z + 365天 = 2027-11-05T00:00:00Z）
	if host.saved["expired"] != "2027-11-05T00:00:00Z" {
		t.Fatalf("expected expired 2027-11-05T00:00:00Z, got: %v", host.saved["expired"])
	}
	if host.saved["is_setup_token"] != true {
		t.Fatalf("expected is_setup_token true, got: %v", host.saved["is_setup_token"])
	}
	if host.saved["skip_account_profile"] != true {
		t.Fatalf("expected skip_account_profile true, got: %v", host.saved["skip_account_profile"])
	}
	if host.saved["scopes"] != "user:inference" {
		t.Fatalf("expected scopes user:inference, got: %v", host.saved["scopes"])
	}
}

func TestSetupTokenFallbackIdentity(t *testing.T) {
	host := &fakeHost{}
	s := newService(func(method string, payload any) (json.RawMessage, error) {
		request := payload.(map[string]any)
		switch method {
		case "host.http.do":
			return raw(upstreamResponse{
				StatusCode: 200,
				Body: raw(map[string]any{
					"access_token": "sk-ant-oat01-minimal-token",
					"expires_in":   31536000,
				}),
			}), nil
		case "host.auth.list":
			return raw(map[string]any{"files": []any{}}), nil
		case "host.auth.save":
			_ = json.Unmarshal(request["json"].(json.RawMessage), &host.saved)
			return raw(map[string]string{"name": request["name"].(string)}), nil
		default:
			return nil, errors.New("unexpected host call")
		}
	})

	flow, err := s.start()
	if err != nil {
		t.Fatal(err)
	}
	state := flow["state"].(string)

	resp, err := s.complete(state, "code#"+state, "")
	if err != nil {
		t.Fatalf("complete failed on minimal identity response: %v", err)
	}
	if resp["status"] != "connected" {
		t.Fatalf("expected status connected, got: %v", resp["status"])
	}

	// 断言落盘数据包含合法的非空 account_uuid 和至少 1 个 64 位小写 hex device id
	uuidStr, _ := host.saved["account_uuid"].(string)
	if uuidStr == "" {
		t.Fatal("fallback account_uuid must not be empty")
	}
	devIDs, ok := host.saved["claude_device_ids"].([]any)
	if !ok || len(devIDs) != 1 || len(devIDs[0].(string)) != 64 {
		t.Fatalf("expected 1 64-char device id, got: %v", host.saved["claude_device_ids"])
	}
}

func TestSetupTokenUpgradePreservesRouteSettingsAndCleansDirtyKeys(t *testing.T) {
	host := &fakeHost{}
	// 桩定已有存量凭据
	existingCred := map[string]any{
		"type":                     "claude",
		"priority":                 float64(10),
		"weight":                   float64(3),
		"claude_device_ids":        []any{"00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"},
		"refresh_token":            "old-rt",
		"refreshToken":             "old-camel-rt",
		"refresh_interval_seconds": float64(1800),
	}

	identity := "acc-upg-1"
	digest := sha256.Sum256([]byte(identity))
	existingName := "claude-" + hex.EncodeToString(digest[:])[:8] + "-upgrade@example.com.json"

	s := newService(func(method string, payload any) (json.RawMessage, error) {
		request := payload.(map[string]any)
		switch method {
		case "host.http.do":
			return raw(upstreamResponse{
				StatusCode: 200,
				Body: raw(map[string]any{
					"access_token": "sk-ant-oat01-upgraded",
					"expires_in":   31536000,
					"account": map[string]string{
						"uuid":          "acc-upg-1",
						"email_address": "upgrade@example.com",
					},
				}),
			}), nil
		case "host.auth.list":
			return raw(map[string]any{
				"files": []any{
					map[string]string{"name": existingName, "auth_index": "0"},
				},
			}), nil
		case "host.auth.get":
			return raw(map[string]any{"json": raw(existingCred)}), nil
		case "host.auth.save":
			t.Logf("Save requested with name: %v, existingName: %s", request["name"], existingName)
			_ = json.Unmarshal(request["json"].(json.RawMessage), &host.saved)
			return raw(map[string]string{"name": request["name"].(string)}), nil
		default:
			return nil, errors.New("unexpected host call")
		}
	})

	flow, _ := s.start()
	state := flow["state"].(string)
	if _, err := s.complete(state, "code#"+state, ""); err != nil {
		t.Fatal(err)
	}

	// 验证旧属性被完整继承
	if pri, ok := host.saved["priority"].(float64); !ok || pri != 10 {
		t.Fatalf("expected priority 10 preserved, got: %v", host.saved["priority"])
	}
	if w, ok := host.saved["weight"].(float64); !ok || w != 3 {
		t.Fatalf("expected weight 3 preserved, got: %v", host.saved["weight"])
	}
	// 验证旧脏键被彻底移除
	if host.saved["refresh_token"] != "" {
		t.Fatalf("refresh_token must be emptied, got: %v", host.saved["refresh_token"])
	}
	if _, exists := host.saved["refreshToken"]; exists {
		t.Fatal("legacy refreshToken camelCase key must be deleted")
	}
	if _, exists := host.saved["refresh_interval_seconds"]; exists {
		t.Fatal("legacy refresh_interval_seconds key must be deleted")
	}
}

func TestOAuthModeExplicitAndInvalidMode(t *testing.T) {
	host := &fakeHost{}
	s := newService(host.call)

	// 非法 mode 必须返回 400
	invalidResp := s.handle(managementRequest{
		Method: "POST",
		Path:   apiBase + "/start",
		Body:   []byte(`{"mode":"invalid_xyz"}`),
	})
	if invalidResp.StatusCode != 400 {
		t.Fatalf("expected 400 for invalid mode, got: %d", invalidResp.StatusCode)
	}

	// 显式 oauth 模式
	oauthResp := s.handle(managementRequest{
		Method: "POST",
		Path:   apiBase + "/start",
		Body:   []byte(`{"mode":"oauth"}`),
	})
	if oauthResp.StatusCode != 200 {
		t.Fatalf("expected 200 for oauth mode, got: %d", oauthResp.StatusCode)
	}
	var oauthStart map[string]any
	_ = json.Unmarshal(oauthResp.Body, &oauthStart)
	if oauthStart["mode"] != "oauth" {
		t.Fatalf("expected mode oauth, got: %v", oauthStart["mode"])
	}
	u, _ := url.Parse(oauthStart["url"].(string))
	if !strings.Contains(u.Query().Get("scope"), "user:profile") {
		t.Fatalf("expected full scopes in oauth mode, got: %v", u.Query().Get("scope"))
	}
}

func TestSetupTokenEnforcesZeroRefreshAndFixedExpiry(t *testing.T) {
	fixedTime := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	host := &fakeHost{}
	s := newService(func(method string, payload any) (json.RawMessage, error) {
		request := payload.(map[string]any)
		switch method {
		case "host.http.do":
			// 桩定上游恶作剧返回了 refresh_token 和异常的 expires_in 0
			return raw(upstreamResponse{
				StatusCode: 200,
				Body: raw(map[string]any{
					"access_token":  "sk-ant-oat01-strict",
					"refresh_token": "should-be-stripped",
					"expires_in":    0,
					"account": map[string]string{
						"uuid":          "strict-acc",
						"email_address": "strict@example.com",
					},
				}),
			}), nil
		case "host.auth.list":
			return raw(map[string]any{"files": []any{}}), nil
		case "host.auth.save":
			_ = json.Unmarshal(request["json"].(json.RawMessage), &host.saved)
			return raw(map[string]string{"name": request["name"].(string)}), nil
		default:
			return nil, errors.New("unexpected host call")
		}
	})
	s.now = func() time.Time { return fixedTime }

	flow, _ := s.start()
	state := flow["state"].(string)
	if _, err := s.complete(state, "code#"+state, ""); err != nil {
		t.Fatal(err)
	}

	// 验证 refresh_token 依然被强制清空，且 expired 强制等于 1 年后
	if host.saved["refresh_token"] != "" {
		t.Fatalf("refresh_token must be forced empty, got: %v", host.saved["refresh_token"])
	}
	if host.saved["expired"] != "2027-11-05T00:00:00Z" {
		t.Fatalf("expected expired 2027-11-05T00:00:00Z, got: %v", host.saved["expired"])
	}
}

func TestSetupTokenMultipleTeamSeatsDoNotCollide(t *testing.T) {
	savedFiles := make(map[string]map[string]any)

	newSeatService := func(accountUUID string) *service {
		return newService(func(method string, payload any) (json.RawMessage, error) {
			request := payload.(map[string]any)
			switch method {
			case "host.http.do":
				// 相同 organization.uuid，不同 account.uuid，且无邮箱
				return raw(upstreamResponse{
					StatusCode: 200,
					Body: raw(map[string]any{
						"access_token": "sk-ant-oat01-seat-" + accountUUID,
						"expires_in":   31536000,
						"account": map[string]string{
							"uuid": accountUUID,
						},
						"organization": map[string]string{
							"uuid": "shared-team-org-uuid",
							"name": "Acme Team",
						},
					}),
				}), nil
			case "host.auth.list":
				return raw(map[string]any{"files": []any{}}), nil
			case "host.auth.save":
				fileName := request["name"].(string)
				var content map[string]any
				_ = json.Unmarshal(request["json"].(json.RawMessage), &content)
				savedFiles[fileName] = content
				return raw(map[string]string{"name": fileName}), nil
			default:
				return nil, errors.New("unexpected host call")
			}
		})
	}

	// 席位 1
	s1 := newSeatService("seat-uuid-1")
	f1, _ := s1.start()
	if _, err := s1.complete(f1["state"].(string), "code#"+f1["state"].(string), ""); err != nil {
		t.Fatal(err)
	}

	// 席位 2
	s2 := newSeatService("seat-uuid-2")
	f2, _ := s2.start()
	if _, err := s2.complete(f2["state"].(string), "code#"+f2["state"].(string), ""); err != nil {
		t.Fatal(err)
	}

	// 必须生成两份完全不同的文件，绝不发生覆盖
	if len(savedFiles) != 2 {
		t.Fatalf("expected 2 distinct credential files, got %d", len(savedFiles))
	}

	var devIDs []string
	for _, cred := range savedFiles {
		ids := cred["claude_device_ids"].([]any)
		devIDs = append(devIDs, ids[0].(string))
	}
	// 两个席位的设备指纹必须完全不同
	if devIDs[0] == devIDs[1] {
		t.Fatal("device ids between two team seats must not collide")
	}
}

func TestSetupTokenDowngradesToOAuthCleansSetupFlags(t *testing.T) {
	host := &fakeHost{}
	existingSetupCred := map[string]any{
		"type":                 "claude",
		"access_token":         "sk-ant-oat01-old",
		"is_setup_token":       true,
		"skip_account_profile": true,
		"scopes":               "user:inference",
	}

	identity := "acc-downgrade-1"
	digest := sha256.Sum256([]byte(identity))
	existingName := "claude-" + hex.EncodeToString(digest[:])[:8] + "-user@example.com.json"

	s := newService(func(method string, payload any) (json.RawMessage, error) {
		request := payload.(map[string]any)
		switch method {
		case "host.http.do":
			reqURL := request["url"].(string)
			if reqURL == tokenURL {
				return raw(upstreamResponse{
					StatusCode: 200,
					Body: raw(map[string]any{
						"access_token":  "sk-ant-oat01-new-oauth",
						"refresh_token": "new-refresh-token",
						"expires_in":    3600,
						"account": map[string]string{
							"uuid":          identity,
							"email_address": "user@example.com",
						},
					}),
				}), nil
			}
			return raw(upstreamResponse{StatusCode: 200, Body: raw(map[string]any{})}), nil
		case "host.auth.list":
			return raw(map[string]any{
				"files": []any{map[string]string{"name": existingName, "auth_index": "0"}},
			}), nil
		case "host.auth.get":
			return raw(map[string]any{"json": raw(existingSetupCred)}), nil
		case "host.auth.save":
			_ = json.Unmarshal(request["json"].(json.RawMessage), &host.saved)
			return raw(map[string]string{"name": request["name"].(string)}), nil
		default:
			return nil, errors.New("unexpected host call")
		}
	})

	// 显式 oauth 模式重新授权
	flow, _ := s.start("oauth")
	state := flow["state"].(string)
	if _, err := s.complete(state, "code#"+state, ""); err != nil {
		t.Fatal(err)
	}

	// 验证旧 setup 标记被彻底清理
	if _, exists := host.saved["is_setup_token"]; exists {
		t.Fatal("is_setup_token must be removed when switching to oauth")
	}
	if _, exists := host.saved["skip_account_profile"]; exists {
		t.Fatal("skip_account_profile must be removed when switching to oauth")
	}
	if host.saved["scopes"] != scopeOAuth {
		t.Fatalf("expected scopes to be restored to scopeOAuth, got: %v", host.saved["scopes"])
	}
}



