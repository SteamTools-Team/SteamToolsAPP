package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var apiHeaders = map[string]string{
	"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
	"Accept":     "application/json",
}

func requestSigningKey(token string) string {
	if token == "" {
		return ""
	}
	dot := strings.LastIndex(token, ".")
	if dot < 1 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(token[:dot])
	if err != nil {
		return ""
	}
	var p struct {
		RK string `json:"rk"`
	}
	if json.Unmarshal(raw, &p) != nil || p.RK == "" {
		return ""
	}
	return p.RK
}

func randomNonceHex() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

func sha256hexBody(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func formEscapeWHATWG(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ':
			b.WriteByte('+')
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9'):
			b.WriteByte(c)
		case c == '*' || c == '-' || c == '.' || c == '_':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteString(strings.ToUpper(hex.EncodeToString([]byte{c})))
		}
	}
	return b.String()
}

func encodeQueryWHATWG(v url.Values) string {
	if len(v) == 0 {
		return ""
	}
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		for _, val := range v[k] {
			if b.Len() > 0 {
				b.WriteByte('&')
			}
			b.WriteString(formEscapeWHATWG(k))
			b.WriteByte('=')
			b.WriteString(formEscapeWHATWG(val))
		}
	}
	return b.String()
}

var (
	authLogMu      sync.Mutex
	lastServerDate time.Time
	clockOffset    time.Duration
)

func setLastServerDate(v string) {
	t, err := http.ParseTime(v)
	if err != nil {
		return
	}
	authLogMu.Lock()
	lastServerDate = t

	off := time.Until(t)
	if off > 6*time.Hour || off < -6*time.Hour {
		off = 0
	}
	clockOffset = off
	authLogMu.Unlock()
}

func signClockOffset() time.Duration {
	authLogMu.Lock()
	defer authLogMu.Unlock()
	return clockOffset
}

func serverClockSkew() time.Duration {
	authLogMu.Lock()
	d := lastServerDate
	authLogMu.Unlock()
	if d.IsZero() {
		return 0
	}
	return time.Since(d)
}

func authLog(msg string) {
	dir := filepath.Join(appDataDir(), "SteamTools")
	_ = os.MkdirAll(dir, 0o755)
	p := filepath.Join(dir, "auth.log")
	if fi, err := os.Stat(p); err == nil && fi.Size() > 512*1024 {
		_ = os.Remove(p)
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(time.Now().Format("2006-01-02 15:04:05") + " " + msg + "\n")
}

func classifyVerifyError(data map[string]interface{}) string {
	errMsg := toString(data["error"])
	sigErr := toString(data["sig_error"])
	if strings.Contains(sigErr, "Timestamp") || strings.Contains(errMsg, "Timestamp") {
		return "clock"
	}
	if sigErr != "" || strings.Contains(errMsg, "signature") {
		return "signature"
	}
	return "token"
}

func signProxyRequest(method, path string, rawBody string) map[string]string {
	token := sessionToken()
	key := requestSigningKey(token)
	headers := map[string]string{}
	if key == "" {
		return headers
	}
	ts := strconv.FormatInt(time.Now().Add(signClockOffset()).UnixMilli(), 10)
	nonce := randomNonceHex()
	canonical := strings.Join([]string{
		method,
		path,
		"",
		ts,
		nonce,
		sha256hexBody(rawBody),
	}, "\n")
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(canonical))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	headers["X-ST-Ts"] = ts
	headers["X-ST-Nonce"] = nonce
	headers["X-ST-Sig"] = sig
	return headers
}

func addRequestSignature(req *http.Request, token, rawBody string) {
	key := requestSigningKey(token)
	if key == "" {
		return
	}
	ts := strconv.FormatInt(time.Now().Add(signClockOffset()).UnixMilli(), 10)
	nonce := randomNonceHex()
	canonical := strings.Join([]string{
		req.Method,
		req.URL.Path,
		req.URL.RawQuery,
		ts,
		nonce,
		sha256hexBody(rawBody),
	}, "\n")
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(canonical))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	req.Header.Set("X-ST-Ts", ts)
	req.Header.Set("X-ST-Nonce", nonce)
	req.Header.Set("X-ST-Sig", sig)
}

type sessionState struct {
	mu          sync.Mutex
	token       string
	user        map[string]interface{}
	tier        string
	usage       interface{}
	blocked     bool
	blockReason string
}

var session = &sessionState{tier: "standard"}

func loadSession() {
	data, err := os.ReadFile(sessionFile())
	if err != nil {
		return
	}
	var d struct {
		Token       string                 `json:"token"`
		User        map[string]interface{} `json:"user"`
		Tier        string                 `json:"tier"`
		Usage       interface{}            `json:"usage"`
		Blocked     bool                   `json:"blocked"`
		BlockReason string                 `json:"block_reason"`
	}
	if json.Unmarshal(data, &d) != nil {
		return
	}
	session.mu.Lock()
	session.token = d.Token
	session.user = d.User
	session.tier = d.Tier
	if session.tier == "" {
		session.tier = "standard"
	}
	session.usage = d.Usage
	session.blocked = d.Blocked
	session.blockReason = d.BlockReason
	t := session.tier
	session.mu.Unlock()
	authLog(fmt.Sprintf("session loaded tier=%s token_len=%d hwid=%s", t, len(d.Token), serverHWID))
}

func sessionSnapshot() map[string]interface{} {
	session.mu.Lock()
	defer session.mu.Unlock()
	return map[string]interface{}{
		"authenticated": session.token != "",
		"user":          session.user,
		"tier":          session.tier,
		"usage":         session.usage,
		"blocked":       session.blocked,
		"block_reason":  session.blockReason,
	}
}

func setSession(token string, user map[string]interface{}, tier string, usage interface{}) {
	session.mu.Lock()
	session.token = token
	session.user = user
	session.tier = tier
	if session.tier == "" {
		session.tier = "standard"
	}
	session.usage = usage
	t := session.tier
	session.mu.Unlock()
	authLog(fmt.Sprintf("session set tier=%s token_len=%d hwid=%s", t, len(token), serverHWID))
	saveSession()
}

func saveSession() {
	session.mu.Lock()
	data, _ := json.Marshal(map[string]interface{}{
		"token":        session.token,
		"user":         session.user,
		"tier":         session.tier,
		"usage":        session.usage,
		"blocked":      session.blocked,
		"block_reason": session.blockReason,
	})
	session.mu.Unlock()
	dir := filepath.Dir(sessionFile())
	_ = os.MkdirAll(dir, 0o755)
	tmp := filepath.Join(dir, ".session_tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, sessionFile())
}

func clearSession() {
	session.mu.Lock()
	session.token = ""
	session.user = nil
	session.tier = "standard"
	session.usage = nil
	session.blocked = false
	session.blockReason = ""
	session.mu.Unlock()
	authLog("session cleared")
	_ = os.Remove(sessionFile())
}

func apiGet(path, token string, timeout time.Duration, params url.Values) (int, map[string]interface{}) {
	u := WEBSITE_BASE + path
	if q := encodeQueryWHATWG(params); q != "" {
		u += "?" + q
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return 0, nil
	}
	for k, v := range apiHeaders {
		req.Header.Set(k, v)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	addRequestSignature(req, token, "")
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	if d := resp.Header.Get("Date"); d != "" {
		setLastServerDate(d)
	}
	body, _ := io.ReadAll(resp.Body)
	var out map[string]interface{}
	if json.Unmarshal(body, &out) != nil {
		out = map[string]interface{}{}
	}
	return resp.StatusCode, out
}

func apiPost(path string, body map[string]interface{}, token string, timeout time.Duration) (int, map[string]interface{}) {
	data, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", WEBSITE_BASE+path, bytes.NewReader(data))
	if err != nil {
		return 0, nil
	}
	for k, v := range apiHeaders {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	addRequestSignature(req, token, string(data))
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	if d := resp.Header.Get("Date"); d != "" {
		setLastServerDate(d)
	}
	bodyRaw, _ := io.ReadAll(resp.Body)
	var out map[string]interface{}
	if json.Unmarshal(bodyRaw, &out) != nil {
		trunc := ""
		if len(bodyRaw) > 0 {
			trunc = string(bodyRaw)
			if len(bodyRaw) > 300 {
				trunc = string(bodyRaw[:300])
			}
		}
		out = map[string]interface{}{"_raw": trunc}
	}
	return resp.StatusCode, out
}

func verifyAndRefresh() (bool, string) {
	session.mu.Lock()
	token := session.token
	user := session.user
	tier := session.tier
	usage := session.usage
	session.mu.Unlock()
	_ = user
	_ = tier
	_ = usage
	if token == "" {
		return false, "no_token"
	}
	params := url.Values{}
	if serverHWID != "" {
		params.Set("hwid", serverHWID)
	}
	status, data := apiGet("/api/desktop/me", token, 10*time.Second, params)

	if status == 401 {
		time.Sleep(2 * time.Second)
		status, data = apiGet("/api/desktop/me", token, 10*time.Second, params)
	}
	if status == 200 {
		ti, _ := data["tier"].(string)
		if ti == "" {
			ti = "standard"
		}
		session.mu.Lock()
		session.tier = ti
		session.usage = data["usage"]
		session.blocked = false
		session.blockReason = ""

		if u, ok := data["user"].(map[string]interface{}); ok {
			if session.user == nil {
				session.user = map[string]interface{}{}
			}
			for k, v := range u {
				session.user[k] = v
			}
		}
		session.mu.Unlock()
		saveSession()
		return true, ""
	}
	if status == 403 {
		if b, _ := data["blocked"].(bool); b {
			reason, _ := data["error"].(string)
			if reason == "" {
				reason = "Your account has been restricted. Please verify the discord server."
			}
			session.mu.Lock()
			session.blocked = true
			session.blockReason = reason
			session.mu.Unlock()
			saveSession()
			return true, ""
		}
		return true, ""
	}
	if status == 401 {
		code := classifyVerifyError(data)
		authLog(fmt.Sprintf("me 401 code=%s error=%q sig_error=%q clock_skew=%ds",
			code, toString(data["error"]), toString(data["sig_error"]), int(serverClockSkew().Seconds())))

		return false, code
	}
	if status == 0 {
		authLog("me unreachable (network/timeout) - keeping session")
	} else if status != 200 {
		authLog(fmt.Sprintf("me unexpected status=%d error=%q", status, toString(data["error"])))
	}
	return true, ""
}

func sessionUser() map[string]interface{} {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.user
}

func sessionToken() string {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.token
}

func sessionBlocked() (bool, string) {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.blocked, session.blockReason
}

func proxyPost(provider, action string, extra map[string]interface{}) (int, map[string]interface{}) {
	body := map[string]interface{}{"provider": provider, "action": action}
	for k, v := range extra {
		body[k] = v
	}
	return apiPost("/api/desktop/proxy", body, sessionToken(), 30*time.Second)
}

func proxySearch(provider, search string) (int, map[string]interface{}) {
	return proxyPost(provider, "search", map[string]interface{}{"search": search})
}

func proxyDownload(provider, appid string) (int, map[string]interface{}) {
	return proxyPost(provider, "download", map[string]interface{}{"appid": appid})
}

func proxyGameCount() (int, map[string]interface{}) {
	return proxyPost("steamtools", "game-count", nil)
}

func proxyGameList() (int, map[string]interface{}) {
	return proxyPost("steamtools", "gamelist", nil)
}

func proxyLua(appid string) (int, []byte) {
	body := map[string]interface{}{"provider": "steamtools", "action": "lua", "appid": appid}
	data, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", WEBSITE_BASE+"/api/desktop/proxy", bytes.NewReader(data))
	if err != nil {
		return 0, nil
	}
	for k, v := range apiHeaders {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	token := sessionToken()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	addRequestSignature(req, token, string(data))
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	bodyRaw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, bodyRaw
}

func proxyFixes() (int, map[string]interface{}) {
	return proxyPost("depotbox", "fixes", nil)
}
