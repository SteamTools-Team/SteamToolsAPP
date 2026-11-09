package main

import (
	"crypto/rand"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var webFS embed.FS

type sseTask struct {
	mu sync.Mutex
	ch chan map[string]interface{}
}

var (
	tasksMu sync.Mutex
	tasks   = map[string]*sseTask{}
)

func newTaskID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano()%100000000)
	}
	return fmt.Sprintf("%08x", b)
}

func newTask() string {
	id := newTaskID()
	t := &sseTask{ch: make(chan map[string]interface{}, 128)}
	tasksMu.Lock()
	tasks[id] = t
	tasksMu.Unlock()
	time.AfterFunc(120*time.Second, func() {
		tasksMu.Lock()
		delete(tasks, id)
		tasksMu.Unlock()
	})
	return id
}

func pushEvent(t *sseTask, kw map[string]interface{}) {
	if t == nil {
		return
	}
	select {
	case t.ch <- kw:
	default:
	}
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func mimeType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html":
		return "text/html;charset=utf-8"
	case ".js":
		return "application/javascript;charset=utf-8"
	case ".css":
		return "text/css;charset=utf-8"
	case ".json":
		return "application/json"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".woff2":
		return "font/woff2"
	default:
		return mime.TypeByExtension(filepath.Ext(path))
	}
}

func authPage(success bool, message string) []byte {
	msg := html.EscapeString(message)
	icon, cls, title, desc := "", "error", "Login failed", "An error occurred during authentication."
	if success {
		icon = `<svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="#3dd68c" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>`
		title = "Login successful"
		desc = "Your Discord account has been connected. You can return to the application."
		cls = "success"
	} else {
		icon = `<svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="#f87171" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>`
		desc = msg
	}
	page := fmt.Sprintf(`<!DOCTYPE html>
<html lang="fr"><head>
<meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>SteamTools — Authentication</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:'Inter','Segoe UI',system-ui,-apple-system,sans-serif;background:#09090e;color:#eceaf8;min-height:100vh;display:flex;align-items:center;justify-content:center;padding:2rem}
.card{background:#0e0e1b;border:1px solid rgba(255,255,255,.07);border-radius:18px;padding:44px 36px;text-align:center;display:flex;flex-direction:column;align-items:center;gap:18px;max-width:360px;width:100%%}
.icon{width:64px;height:64px;border-radius:16px;display:flex;align-items:center;justify-content:center}
.icon--success{background:rgba(61,214,140,.10);border:1px solid rgba(61,214,140,.22)}
.icon--error{background:rgba(248,113,113,.10);border:1px solid rgba(248,113,113,.22)}
h1{font-size:1.25rem;font-weight:800;letter-spacing:-.03em}
.desc{font-size:.88rem;color:#65637e;line-height:1.6}
</style></head><body>
<div class="card">
<div class="icon icon--%s">%s</div>
<h1>%s</h1>
<p class="desc">%s</p>
</div></body></html>`, cls, icon, title, desc)
	return []byte(page)
}

func handleEvents(w http.ResponseWriter, r *http.Request, tid string) {
	tasksMu.Lock()
	t := tasks[tid]
	tasksMu.Unlock()
	if t == nil {
		writeJSON(w, 404, map[string]interface{}{"error": "Task not found"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "close")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}
	missed := 0
	keepTicker := time.NewTicker(5 * time.Second)
	defer keepTicker.Stop()
	for {
		select {
		case ev := <-t.ch:
			missed = 0
			line := fmt.Sprintf("data: %s\n\n", toJSONString(ev))
			if _, err := io.WriteString(w, line); err != nil {
				return
			}
			flusher.Flush()
			if typ, _ := ev["type"].(string); typ == "done" {
				return
			}
		case <-keepTicker.C:
			missed++
			if missed > 36 {
				return
			}
			if _, err := io.WriteString(w, "data: {\"keep\":true}\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func toJSONString(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

type apiHandler struct{}

func isLocalHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	h := host
	if i := strings.LastIndex(h, ":"); i >= 0 {
		if _, err := strconv.Atoi(h[i+1:]); err == nil {
			h = h[:i]
		}
	}
	return h == "127.0.0.1" || h == "::1" || h == "localhost"
}

func (h apiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !isLocalHost(r.Host) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"error":"Forbidden"}`))
		return
	}

	path := strings.TrimSuffix(r.URL.Path, "/")
	if path == "" {
		path = "/"
	}
	if r.Method == http.MethodGet {
		h.handleGET(w, r, path)
		return
	}
	h.handlePOST(w, r, path)
}

func (h apiHandler) handleGET(w http.ResponseWriter, r *http.Request, path string) {
	params := r.URL.Query()

	switch {
	case path == "/api/auth/status":
		writeJSON(w, 200, sessionSnapshot())
		return
	case path == "/api/auth/login-url":
		cb := fmt.Sprintf("http://127.0.0.1:%d/api/auth/callback", serverPort)
		u := WEBSITE_BASE + "/api/desktop/begin-auth?callback=" + url.QueryEscape(cb)
		writeJSON(w, 200, map[string]interface{}{"url": u})
		return
	case path == "/api/auth/callback":
		token := params.Get("token")
		rawUser := params.Get("user")
		if rawUser == "" {
			rawUser = "{}"
		}
		user := map[string]interface{}{}
		_ = json.Unmarshal([]byte(rawUser), &user)
		if token == "" {
			w.Header().Set("Content-Type", "text/html;charset=utf-8")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.WriteHeader(200)
			w.Write(authPage(false, "Missing token parameter."))
			return
		}
		session.mu.Lock()
		session.blocked = false
		session.blockReason = ""
		session.mu.Unlock()
		setSession(token, user, "standard", nil)
		name := toString(user["name"])
		if name == "" {
			name = "user"
		}
		w.Header().Set("Content-Type", "text/html;charset=utf-8")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(200)
		w.Write(authPage(true, "Welcome, "+html.EscapeString(name)+"!"))
		return
	case path == "/api/auth/refresh":
		ok, verr := verifyAndRefresh()
		api.invalidateSteamCache()
		api.invalidateGamesCache()
		if !ok {
			writeJSON(w, 200, map[string]interface{}{"authenticated": false, "verify_error": verr})
			return
		}
		writeJSON(w, 200, sessionSnapshot())
		return
	case path == "/api/auth/pair-status":
		rid := params.Get("requestId")
		if rid == "" {
			writeJSON(w, 200, map[string]interface{}{"status": "error", "authenticated": false, "error": "missing_request_id"})
			return
		}
		p := url.Values{}
		p.Set("requestId", rid)
		if serverHWID != "" {
			p.Set("hwid", serverHWID)
		}
		st, data := apiGet("/api/desktop/pair/status", "", 8*time.Second, p)
		stName := toString(data["status"])
		if st == 200 && stName == "claimed" {
			token := toString(data["token"])
			if token != "" {
				user, _ := data["user"].(map[string]interface{})
				if user == nil {
					user = map[string]interface{}{}
				}
				session.mu.Lock()
				session.blocked = false
				session.blockReason = ""
				session.mu.Unlock()
				setSession(token, user, "standard", nil)
				writeJSON(w, 200, sessionSnapshot())
				return
			}
		}
		if stName == "" {
			stName = "error"
		}
		writeJSON(w, 200, map[string]interface{}{
			"status":        stName,
			"authenticated": false,
			"error":         toString(data["error"]),
		})
		return
	case path == "/api/version":
		writeJSON(w, 200, map[string]interface{}{"local": LOCAL_VERSION})
		return
	case path == "/api/config":
		writeJSON(w, 200, api.loadConfig())
		return
	case path == "/api/steam/status":
		res := api.getSteamStatus()
		if res == nil {
			res = map[string]interface{}{"detected": false, "path": "", "running": false}
		}
		writeJSON(w, 200, res)
		return
	case path == "/api/steam/invalidate":
		api.invalidateSteamCache()
		writeJSON(w, 200, map[string]interface{}{"ok": true})
		return
	case path == "/api/games/available":
		q := params.Get("q")
		limit := 200
		if ls := params.Get("limit"); ls != "" {
			if n, err := strconv.Atoi(ls); err == nil {
				limit = n
			}
		}
		qLower := strings.ToLower(strings.TrimSpace(q))
		var result map[string]interface{}
		if qLower != "" {
			result = api.getAvailableGames(qLower, limit)
		} else {
			result = api.getAvailableGames("", limit)
		}
		if ok, _ := result["ok"].(bool); !ok {
			writeJSON(w, 200, map[string]interface{}{
				"ok": false, "games": []interface{}{}, "total": 0, "found": 0,
				"error": toString(result["error"]),
			})
			return
		}
		allGames := toGameList(result["games"])
		games := allGames
		if limit > 0 {
			n := limit
			if n > 100000 {
				n = 100000
			}
			if n > len(allGames) {
				n = len(allGames)
			}
			if n < 1 {
				n = 1
			}
			games = allGames[:n]
		}
		resp := map[string]interface{}{
			"ok": true, "games": games,
			"total": result["total"], "found": len(allGames),
		}
		if f, _ := result["_hubcap_fallback"].(bool); f {
			resp["_hubcap_fallback"] = true
		}
		if e, ok := result["_hubcap_error"]; ok {
			resp["_hubcap_error"] = e
		}
		writeJSON(w, 200, resp)
		return
	case path == "/api/games/total":
		writeJSON(w, 200, api.getGamesTotal())
		return
	case path == "/api/games/details":
		writeJSON(w, 200, api.getGameDetails(params.Get("appid")))
		return
	case path == "/api/games/installed":
		writeJSON(w, 200, api.getInstalledGames())
		return
	case path == "/api/games/export":
		writeJSON(w, 200, map[string]interface{}{"ok": true, "data": api.exportGamesJSON()})
		return
	case path == "/api/settings":
		writeJSON(w, 200, api.getSettings())
		return
	case path == "/api/fixes":
		writeJSON(w, 200, api.getFixesList())
		return
	case path == "/api/game-fixes":
		writeJSON(w, 200, api.getGameFixesList())
		return
	case path == "/api/hubcap/debug":
		h.handleHubcapDebug(w, r)
		return
	case strings.HasPrefix(path, "/api/events/"):
		tid := strings.TrimPrefix(path, "/api/events/")
		handleEvents(w, r, tid)
		return
	case path == "/api/image":

	}

	if m := regexp.MustCompile(`^/api/image/(\d+)$`).FindStringSubmatch(path); m != nil {
		appid := m[1]
		data := api.getCachedImage(appid)
		if data != nil {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("Cache-Control", "max-age=86400")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.WriteHeader(200)
			_, _ = w.Write(data)
			return
		}
		w.WriteHeader(404)
		return
	}

	h.serveStatic(w, r, path)
}

func (h apiHandler) serveStatic(w http.ResponseWriter, r *http.Request, path string) {
	if path == "/" {
		path = "/index.html"
	}
	rel := strings.TrimPrefix(path, "/")
	rel = filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	if strings.HasPrefix(rel, "..") || strings.Contains(rel, "web/..") {
		writeJSON(w, 403, map[string]interface{}{"error": "Forbidden"})
		return
	}
	data, err := webFS.ReadFile("web/" + rel)
	if err != nil {

		index, ierr := webFS.ReadFile("web/index.html")
		if ierr != nil {
			writeJSON(w, 404, map[string]interface{}{"error": "Not Found"})
			return
		}
		w.Header().Set("Content-Type", "text/html;charset=utf-8")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(200)
		_, _ = w.Write(index)
		return
	}
	ct := mimeType(rel)
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}

func (h apiHandler) handleHubcapDebug(w http.ResponseWriter, r *http.Request) {
	settings := api.getSettings()
	key := strings.TrimSpace(toString(settings["hubcap_api_key"]))
	if key == "" {
		writeJSON(w, 200, map[string]interface{}{"ok": false, "error": "No Hubcap API key set"})
		return
	}
	results := map[string]interface{}{}
	tests := []struct {
		name string
		url  string
		auth bool
	}{
		{"health", "https://hubcap.example.com/api/v1/health", false},
		{"library", "https://hubcap.example.com/api/v1/library?limit=1", true},
		{"search", "https://hubcap.example.com/api/v1/search?q=aaa&limit=1", true},
		{"user_stats", "https://hubcap.example.com/api/v1/user/stats", true},
	}
	for _, t := range tests {
		headers := map[string]string{"User-Agent": "Mozilla/5.0"}
		if t.auth {
			headers["Authorization"] = "Bearer " + key
		}
		body, status, err := httpGetBytes(t.url, 10*time.Second, headers)
		if err != nil {
			results[t.name] = map[string]interface{}{"error": err.Error()}
			continue
		}
		text := string(body)
		if len(text) > 2000 {
			text = text[:2000]
		}
		results[t.name] = map[string]interface{}{"status": status, "body": text}
	}
	writeJSON(w, 200, map[string]interface{}{"ok": true, "debug": results})
}

func (h apiHandler) handlePOST(w http.ResponseWriter, r *http.Request, path string) {

	if path == "/api/install/from-manifest" {
		h.installFromManifest(w, r)
		return
	}

	bodyRaw, _ := io.ReadAll(r.Body)
	bodyJSON := map[string]interface{}{}
	if len(bodyRaw) > 0 {
		_ = json.Unmarshal(bodyRaw, &bodyJSON)
	}

	switch path {
	case "/api/shutdown":
		time.AfterFunc(400*time.Millisecond, func() { os.Exit(0) })
		writeJSON(w, 200, map[string]interface{}{"ok": true})
		return
	case "/api/check-ban":
		hwid := toString(bodyJSON["hwid"])
		if hwid == "" {
			writeJSON(w, 200, map[string]interface{}{"banned": false})
			return
		}
		_, result := apiPost("/api/desktop/security-alert", map[string]interface{}{"hwid": hwid, "checkOnly": true}, "", 8*time.Second)
		writeJSON(w, 200, map[string]interface{}{"banned": toBool(result["banned"])})
		return
	case "/api/auth/logout":
		clearSession()
		writeJSON(w, 200, map[string]interface{}{"ok": true})
		return
	case "/api/auth/pair-begin":
		st, data := apiPost("/api/desktop/pair/begin", map[string]interface{}{"hwid": serverHWID}, "", 8*time.Second)
		if st != 200 || toString(data["code"]) == "" {
			errMsg := toString(data["error"])
			if errMsg == "" {
				errMsg = "unavailable"
			}
			writeJSON(w, 200, map[string]interface{}{"error": errMsg})
			return
		}
		writeJSON(w, 200, map[string]interface{}{
			"code":        toString(data["code"]),
			"requestId":   toString(data["requestId"]),
			"expiresInMs": data["expiresInMs"],
		})
		return
	case "/api/steam/launch":
		writeJSON(w, 200, api.launchSteam())
		return
	case "/api/steam/validate-path":
		writeJSON(w, 200, api.validateSteamPath(toString(bodyJSON["path"])))
		return
	case "/api/games/remove":
		appid := toString(bodyJSON["appid"])
		writeJSON(w, 200, api.removeGame(appid))
		return
	case "/api/games/import":
		writeJSON(w, 200, api.importGamesJSON(toString(bodyJSON["json"])))
		return
	case "/api/settings":
		writeJSON(w, 200, api.saveSettings(bodyJSON))
		return
	case "/api/clear-game-caches":
		api.clearGameCaches()
		writeJSON(w, 200, map[string]interface{}{"ok": true})
		return
	case "/api/clear/image-cache":
		_ = os.RemoveAll(imageCacheDir())
		writeJSON(w, 200, map[string]interface{}{"ok": true})
		return
	case "/api/clear/game-lists":
		for _, p := range []string{hubcapCacheFile(), depotboxCacheFile(), steamtoolsCacheFile(), gamesCacheFile(), nameCacheFile()} {
			_ = os.Remove(p)
		}
		api.invalidateGamesCache()
		writeJSON(w, 200, map[string]interface{}{"ok": true})
		return
	case "/api/clear/fixes":
		for _, p := range []string{fixesCacheFile(), gameFixesCacheFile()} {
			_ = os.Remove(p)
		}
		writeJSON(w, 200, map[string]interface{}{"ok": true})
		return
	case "/api/clear/settings":
		settingsFile := filepath.Join(customLocationDir(), "settings.json")
		_ = os.Remove(settingsFile)
		writeJSON(w, 200, map[string]interface{}{"ok": true})
		return
	case "/api/clear/local-storage":
		writeJSON(w, 200, map[string]interface{}{"ok": true, "action": "clear_local_storage"})
		return
	case "/api/pick-folder":
		writeJSON(w, 200, map[string]interface{}{"path": pickFolder()})
		return
	case "/api/pick-file":
		writeJSON(w, 200, map[string]interface{}{"path": pickFile()})
		return
	case "/api/fixes/detect":
		gameName := toString(bodyJSON["game_name"])
		appid := toString(bodyJSON["appid"])
		writeJSON(w, 200, api.detectFixPath(gameName, appid))
		return
	case "/api/fixes/install":
		fixURL := toString(bodyJSON["fix_url"])
		gamePath := toString(bodyJSON["game_path"])
		if fixURL == "" || gamePath == "" {
			writeJSON(w, 200, map[string]interface{}{"ok": false, "error": "Missing fix_url or game_path"})
			return
		}
		writeJSON(w, 200, api.installFix(fixURL, gamePath))
		return
	case "/api/diagnostic":
		writeJSON(w, 200, api.runDiagnostic())
		return
	case "/api/install/start":
		h.startInstall(w, bodyJSON)
		return
	case "/api/install/batch":
		h.startBatchInstall(w, bodyJSON)
		return
	case "/api/auto-update":
		result := api.prepareUpdate()
		if ok, _ := result["ok"].(bool); !ok {
			writeJSON(w, 500, map[string]interface{}{"ok": false, "error": toString(result["error"])})
			return
		}
		go func() {
			time.Sleep(time.Second)
			os.Exit(0)
		}()
		writeJSON(w, 200, map[string]interface{}{"ok": true})
		return
	case "/api/update-manifest/start":
		h.startUpdateManifest(w, bodyJSON)
		return
	case "/api/update/start":
		h.startUpdate(w, bodyJSON)
		return
	case "/api/repair/check-dlls":
		writeJSON(w, 200, api.checkMissingDlls())
		return
	case "/api/repair/start":
		tid := newTask()
		t := getTask(tid)
		go func() {
			api.setPush(func(kw map[string]interface{}) { pushEvent(t, kw) })
			var result map[string]interface{}
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						result = map[string]interface{}{"ok": false, "repairs": []interface{}{}, "error": fmt.Sprint(rec)}
					}
				}()
				result = api.repairSteam(
					toBool(bodyJSON["cloudredirect"]),
					toBool(bodyJSON["bettersteamtools"]),
				)
			}()
			api.setPush(nil)
			pushEvent(t, appendDone(map[string]interface{}{"type": "done"}, result))
		}()
		writeJSON(w, 200, map[string]interface{}{"task_id": tid})
		return
	case "/api/achievements/games":
		writeJSON(w, 200, api.listOwnedGames())
		return
	case "/api/achievements/appname":
		writeJSON(w, 200, api.achievementAppName(toString(bodyJSON["appid"])))
		return
	case "/api/achievements/list":
		appid := strings.TrimSpace(toString(bodyJSON["appid"]))
		if appid == "" {
			writeJSON(w, 200, map[string]interface{}{"ok": false, "error": "Missing appid"})
			return
		}
		writeJSON(w, 200, api.achievementsList(appid))
		return
	case "/api/achievements/apply":
		appid := strings.TrimSpace(toString(bodyJSON["appid"]))
		if appid == "" {
			writeJSON(w, 200, map[string]interface{}{"ok": false, "error": "Missing appid"})
			return
		}
		writeJSON(w, 200, api.achievementsApply(appid, toStringSlice(bodyJSON["unlock"]), toStringSlice(bodyJSON["lock"])))
		return
	}
	writeJSON(w, 404, map[string]interface{}{"ok": false, "error": "Not found"})
}

func toStringSlice(v interface{}) []string {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s := strings.TrimSpace(toString(item)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func getTask(tid string) *sseTask {
	tasksMu.Lock()
	defer tasksMu.Unlock()
	return tasks[tid]
}

func appendDone(base map[string]interface{}, result map[string]interface{}) map[string]interface{} {
	for k, v := range result {
		base[k] = v
	}
	return base
}

func (h apiHandler) authRequired(w http.ResponseWriter) bool {
	token := sessionToken()
	if token == "" {
		writeJSON(w, 401, map[string]interface{}{"error": "Discord login required.", "auth_required": true})
		return false
	}
	if blocked, reason := sessionBlocked(); blocked {
		if reason == "" {
			reason = "Your account has been restricted. Please verify the discord server."
		}
		writeJSON(w, 403, map[string]interface{}{"error": reason, "blocked": true})
		return false
	}
	return true
}

func (h apiHandler) startInstall(w http.ResponseWriter, bodyJSON map[string]interface{}) {
	if !h.authRequired(w) {
		return
	}
	appid := toString(bodyJSON["appid"])
	skipRestart := toBool(bodyJSON["skip_restart"])
	token := sessionToken()
	settings := api.getSettings()
	provider := toString(settings["provider"])
	status, data := apiPost("/api/desktop/install", map[string]interface{}{"appid": appid, "provider": provider, "hwid": serverHWID}, token, 30*time.Second)
	if status == 429 {
		writeJSON(w, 429, map[string]interface{}{"error": toString(data["error"]), "quota_exceeded": true})
		return
	}
	if status == 403 {
		errMsg := toString(data["error"])
		if errMsg == "" {
			errMsg = toString(data["_raw"])
		}
		if errMsg == "" {
			errMsg = "Access denied. Check your Discord role."
		}
		writeJSON(w, 403, map[string]interface{}{"error": errMsg, "blocked": true})
		return
	}
	if status == 401 {
		clearSession()
		writeJSON(w, 401, map[string]interface{}{"error": "Session expired.", "auth_required": true})
		return
	}
	tid := newTask()
	t := getTask(tid)
	go func() {
		api.setPush(func(kw map[string]interface{}) { pushEvent(t, kw) })
		var result map[string]interface{}
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					result = map[string]interface{}{"ok": false, "error": fmt.Sprint(rec)}
				}
			}()
			result = api.installGame(appid, skipRestart)
		}()
		api.setPush(nil)
		if ok, _ := result["ok"].(bool); !ok {
			if nf, _ := result["not_found"].(bool); nf {
				apiPost("/api/desktop/refund-install", map[string]interface{}{}, token, 10*time.Second)
			}
		}
		pushEvent(t, appendDone(map[string]interface{}{"type": "done"}, result))
	}()
	writeJSON(w, 200, map[string]interface{}{"task_id": tid})
}

func (h apiHandler) startBatchInstall(w http.ResponseWriter, bodyJSON map[string]interface{}) {
	if !h.authRequired(w) {
		return
	}
	appidsRaw, ok := bodyJSON["appids"].([]interface{})
	if !ok || len(appidsRaw) == 0 {
		writeJSON(w, 400, map[string]interface{}{"error": "Missing or empty appids list"})
		return
	}
	appids := make([]string, 0, len(appidsRaw))
	for _, v := range appidsRaw {
		if s, ok := v.(string); ok && s != "" {
			appids = append(appids, s)
		}
	}
	if len(appids) == 0 {
		writeJSON(w, 400, map[string]interface{}{"error": "No valid appids provided"})
		return
	}

	token := sessionToken()
	settings := api.getSettings()
	provider := toString(settings["provider"])
	status, data := apiPost("/api/desktop/install", map[string]interface{}{"appid": appids[0], "provider": provider, "hwid": serverHWID}, token, 30*time.Second)
	if status == 429 {
		writeJSON(w, 429, map[string]interface{}{"error": toString(data["error"]), "quota_exceeded": true})
		return
	}
	if status == 403 {
		errMsg := toString(data["error"])
		if errMsg == "" {
			errMsg = "Access denied. Check your Discord role."
		}
		writeJSON(w, 403, map[string]interface{}{"error": errMsg, "blocked": true})
		return
	}
	if status == 401 {
		clearSession()
		writeJSON(w, 401, map[string]interface{}{"error": "Session expired.", "auth_required": true})
		return
	}

	tid := newTask()
	t := getTask(tid)
	go func() {
		api.setPush(func(kw map[string]interface{}) { pushEvent(t, kw) })
		var result map[string]interface{}
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					result = map[string]interface{}{"ok": false, "error": fmt.Sprint(rec)}
				}
			}()
			result = api.installBatch(appids)
		}()
		api.setPush(nil)
		pushEvent(t, appendDone(map[string]interface{}{"type": "done"}, result))
	}()
	writeJSON(w, 200, map[string]interface{}{"task_id": tid})
}

func (h apiHandler) startUpdateManifest(w http.ResponseWriter, bodyJSON map[string]interface{}) {
	appid := toString(bodyJSON["appid"])
	token := sessionToken()
	if token == "" {
		writeJSON(w, 401, map[string]interface{}{"error": "Discord login required.", "auth_required": true})
		return
	}
	status, data := apiPost("/api/desktop/update", map[string]interface{}{"appid": appid, "hwid": serverHWID}, token, 30*time.Second)
	if status == 403 {
		writeJSON(w, 403, map[string]interface{}{"error": toString(data["error"]), "blocked": true, "premium_required": toBool(data["premium_required"])})
		return
	}
	if status == 429 {
		writeJSON(w, 429, map[string]interface{}{"error": toString(data["error"]), "quota_exceeded": true})
		return
	}
	if status == 401 {
		clearSession()
		writeJSON(w, 401, map[string]interface{}{"error": "Session expired.", "auth_required": true})
		return
	}
	writeJSON(w, 200, map[string]interface{}{"ok": true, "alreadyLatest": toBool(data["alreadyLatest"]), "ryuuMessage": toString(data["ryuuMessage"])})
}

func (h apiHandler) startUpdate(w http.ResponseWriter, bodyJSON map[string]interface{}) {
	if !h.authRequired(w) {
		return
	}
	appid := toString(bodyJSON["appid"])
	token := sessionToken()
	settings := api.getSettings()
	provider := toString(settings["provider"])
	status, data := apiPost("/api/desktop/update", map[string]interface{}{"appid": appid, "provider": provider, "hwid": serverHWID}, token, 30*time.Second)
	if status == 403 {
		writeJSON(w, 403, map[string]interface{}{"error": toString(data["error"]), "blocked": true, "premium_required": toBool(data["premium_required"])})
		return
	}
	if status == 429 {
		writeJSON(w, 429, map[string]interface{}{"error": toString(data["error"]), "quota_exceeded": true})
		return
	}
	if status == 401 {
		clearSession()
		writeJSON(w, 401, map[string]interface{}{"error": "Session expired.", "auth_required": true})
		return
	}
	tid := newTask()
	t := getTask(tid)
	go func() {
		api.setPush(func(kw map[string]interface{}) { pushEvent(t, kw) })
		var result map[string]interface{}
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					result = map[string]interface{}{"ok": false, "error": fmt.Sprint(rec)}
				}
			}()
			result = api.updateGame(appid)
		}()
		api.setPush(nil)
		if ok, _ := result["ok"].(bool); !ok {
			if nf, _ := result["not_found"].(bool); nf {
				apiPost("/api/desktop/refund-install", map[string]interface{}{}, token, 10*time.Second)
			}
		}
		pushEvent(t, appendDone(map[string]interface{}{"type": "done"}, result))
	}()
	writeJSON(w, 200, map[string]interface{}{"task_id": tid})
}

func (h apiHandler) installFromManifest(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(256 << 20); err != nil {
		writeJSON(w, 200, map[string]interface{}{"ok": false, "error": "Upload failed: " + err.Error()})
		return
	}
	if r.MultipartForm == nil || len(r.MultipartForm.File) == 0 {
		writeJSON(w, 200, map[string]interface{}{"ok": false, "error": "No files received. Drop .zip, .manifest or .lua files and try again."})
		return
	}
	uploaded := map[string][]byte{}
	for _, fheaders := range r.MultipartForm.File {
		for _, fh := range fheaders {
			f, err := fh.Open()
			if err != nil {
				continue
			}
			data, _ := io.ReadAll(f)
			f.Close()
			uploaded[fh.Filename] = data
		}
	}
	writeJSON(w, 200, api.installFromManifestUpload(uploaded))
}
