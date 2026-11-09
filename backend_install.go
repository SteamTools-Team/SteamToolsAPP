package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var requiredDLLs = []string{"xinput1_4.dll", "dwmapi.dll", "OpenSteamTool.dll"}

var streamTransport = &http.Transport{
	DialContext:           (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
	ResponseHeaderTimeout: 60 * time.Second,
}

var streamClient = &http.Client{Transport: streamTransport}

type dlSpec struct {
	Method  string
	URL     string
	Headers map[string]string
	Data    map[string]interface{}
}

func effectiveProvider(settings map[string]interface{}) string {
	switch toString(settings["provider"]) {
	case "hubcap", "depotbox", "steamtools":
		return toString(settings["provider"])
	default:
		return "ryuu"
	}
}

func (st *SteamTools) manifestDownload(appid string) (dlSpec, error) {
	settings := st.getSettings()
	provider := toString(settings["provider"])
	if provider == "hubcap" {
		key := strings.TrimSpace(toString(settings["hubcap_api_key"]))
		if key == "" {
			return dlSpec{}, fmt.Errorf("Hubcap API key not set")
		}
		return dlSpec{Method: "GET", URL: fmt.Sprintf("%s/api/v1/manifest/%s", HUBCAP_BASE, appid), Headers: map[string]string{"Authorization": "Bearer " + key, "User-Agent": "Mozilla/5.0"}}, nil
	}
	if provider == "depotbox" {
		proxyBody := map[string]interface{}{"provider": "depotbox", "action": "download", "appid": appid}
		proxyJSON, _ := json.Marshal(proxyBody)
		proxyPath := "/api/desktop/proxy"
		sigHeaders := signProxyRequest("POST", proxyPath, string(proxyJSON))
		allHeaders := map[string]string{
			"Content-Type": "application/json",
			"User-Agent":   "Mozilla/5.0",
		}
		token := sessionToken()
		if token != "" {
			allHeaders["Authorization"] = "Bearer " + token
		}
		for k, v := range sigHeaders {
			allHeaders[k] = v
		}
		return dlSpec{Method: "POST", URL: WEBSITE_BASE + proxyPath, Headers: allHeaders, Data: proxyBody}, nil
	}
	if provider == "steamtools" {
		proxyBody := map[string]interface{}{"provider": "steamtools", "action": "manifest", "appid": appid}
		proxyJSON, _ := json.Marshal(proxyBody)
		proxyPath := "/api/desktop/proxy"
		sigHeaders := signProxyRequest("POST", proxyPath, string(proxyJSON))
		allHeaders := map[string]string{
			"Content-Type": "application/json",
			"User-Agent":   "Mozilla/5.0",
		}
		token := sessionToken()
		if token != "" {
			allHeaders["Authorization"] = "Bearer " + token
		}
		for k, v := range sigHeaders {
			allHeaders[k] = v
		}
		return dlSpec{Method: "POST", URL: WEBSITE_BASE + proxyPath, Headers: allHeaders, Data: proxyBody}, nil
	}

	proxyBody := map[string]interface{}{"provider": "ryuu", "action": "manifest", "appid": appid}
	proxyJSON, _ := json.Marshal(proxyBody)
	proxyPath := "/api/desktop/proxy"
	sigHeaders := signProxyRequest("POST", proxyPath, string(proxyJSON))
	allHeaders := map[string]string{
		"Content-Type": "application/json",
		"User-Agent":   "Mozilla/5.0",
	}
	token := sessionToken()
	if token != "" {
		allHeaders["Authorization"] = "Bearer " + token
	}
	for k, v := range sigHeaders {
		allHeaders[k] = v
	}
	return dlSpec{Method: "POST", URL: WEBSITE_BASE + proxyPath, Headers: allHeaders, Data: proxyBody}, nil
}

func streamDownload(spec dlSpec, destPath string, progress func(pct int, label string)) (int, error) {
	var req *http.Request
	var err error
	if spec.Method == "POST" {
		data, _ := json.Marshal(spec.Data)
		req, err = http.NewRequest("POST", spec.URL, bytes.NewReader(data))
	} else {
		req, err = http.NewRequest("GET", spec.URL, nil)
	}
	if err != nil {
		return 0, err
	}
	for k, v := range spec.Headers {
		req.Header.Set(k, v)
	}
	resp, err := streamClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return resp.StatusCode, nil
	}
	f, err := os.Create(destPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	total := resp.ContentLength
	fetched := int64(0)
	buf := make([]byte, 65536)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return 0, werr
			}
			fetched += int64(n)
			if total > 0 && progress != nil {
				pct := int(fetched * 100 / total)
				progress(pct, fmt.Sprintf("%d KB / %d KB", fetched/1024, total/1024))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return 0, rerr
		}
	}
	return resp.StatusCode, nil
}

func downloadBytes(url string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func copyFile(src, dest string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(dest); err == nil {
		_ = os.Chmod(dest, fi.Mode()|0o200)
	}
	return os.WriteFile(dest, data, 0o644)
}

func extractZip(zipPath, dest string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	destClean := filepath.Clean(dest)
	for _, f := range zr.File {
		target := filepath.Join(dest, f.Name)
		clean := filepath.Clean(target)
		if clean != destClean && !strings.HasPrefix(clean, destClean+string(os.PathSeparator)) {
			continue
		}
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(target, 0o755)
			continue
		}
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return err
		}
		out.Close()
		rc.Close()
	}
	return nil
}

func findLuaManifest(root string) (luaFile, manifestFile string) {
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		name := strings.ToLower(info.Name())
		if strings.HasSuffix(name, ".lua") && luaFile == "" {
			luaFile = path
		}
		if strings.HasSuffix(name, ".manifest") && manifestFile == "" {
			manifestFile = path
		}
		return nil
	})
	return
}

func (st *SteamTools) steamtoolsLuaOnly(appid, extractPath string) bool {
	status, body := proxyLua(appid)
	if status != 200 || body == nil || len(bytes.TrimSpace(body)) == 0 {
		return false
	}
	if strings.HasPrefix(strings.TrimSpace(string(body)), "{") {
		var e map[string]interface{}
		if json.Unmarshal(body, &e) == nil && (toString(e["error"]) != "" || toString(e["message"]) != "") {
			return false
		}
	}
	_ = os.MkdirAll(extractPath, 0o755)
	if err := os.WriteFile(filepath.Join(extractPath, appid+".lua"), body, 0o644); err != nil {
		return false
	}
	return true
}

func (st *SteamTools) installGame(appid string, skipRestart bool) map[string]interface{} {
	appid = strings.TrimSpace(appid)
	step := func(sid, status string, detail ...string) {
		d := ""
		if len(detail) > 0 {
			d = detail[0]
		}
		st.cb(map[string]interface{}{"type": "step", "stepId": sid, "status": status, "detail": d})
	}
	logf := func(msg, t string) { st.cb(map[string]interface{}{"type": "log", "msg": msg, "logType": t}) }
	progress := func(pct int, label string) {
		st.cb(map[string]interface{}{"type": "progress", "pct": pct, "label": label})
	}

	step("detect", "active")
	steam := st.getSteamStatus()
	if ok, _ := steam["detected"].(bool); !ok {
		step("detect", "error", "Steam not found")
		return map[string]interface{}{"ok": false, "error": "Steam not found. Check Settings."}
	}
	steamPath := toString(steam["path"])
	step("detect", "done", steamPath)
	logf("Steam detected: "+steamPath, "success")

	nameCh := make(chan string, 1)
	go func() {
		n := st.fetchGameName(appid)
		if n == "" {
			n = "Game_" + appid
		}
		nameCh <- n
	}()

	settings := st.getSettings()
	var missing []string
	for _, d := range requiredDLLs {
		if _, err := os.Stat(filepath.Join(steamPath, d)); err != nil {
			missing = append(missing, d)
		}
	}

	step("stop", "active")
	if len(missing) > 0 {
		logf("Required files missing — forcing Steam close.", "warning")
	}
	if isSteamRunning() && (toBool(settings["auto_close_steam"]) || len(missing) > 0) {
		logf("Stopping Steam...", "info")
		stopSteam()
		time.Sleep(3 * time.Second)
	} else {
		logf("Steam not running or auto-close disabled, skipping stop.", "info")
	}
	step("stop", "done")

	step("dlls", "active")
	if toBool(settings["auto_install_ost"]) || len(missing) > 0 {
		if len(missing) > 0 {
			logf("Required files missing: "+strings.Join(missing, ", "), "warning")
		}
		if res := st.installBSTFix(steamPath, logf); !toBool(res["ok"]) {
			logf("BetterSteamTools install failed: "+toString(res["error"]), "warning")
		}
	} else {
		logf("BetterSteamTools auto-install disabled and all required files present, skipping.", "info")
	}
	step("dlls", "done")

	step("download", "active")
	progress(0, "Starting download...")
	spec, err := st.manifestDownload(appid)
	if err != nil {
		step("download", "error")
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	logf(fmt.Sprintf("Downloading game ID %s via %s...", appid, effectiveProvider(settings)), "info")

	tmpDir, err := os.MkdirTemp("", "steamtools-install-*")
	if err != nil {
		step("download", "error")
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	defer os.RemoveAll(tmpDir)
	zipPath := filepath.Join(tmpDir, appid+".zip")
	extractPath := filepath.Join(tmpDir, "extracted")

	var fallbackSpec *dlSpec
	if toString(settings["provider"]) == "hubcap" {
		st.mu.Lock()
		apiURL := st.apiURL
		apiKey := st.apiKey
		cfg := st.config
		st.mu.Unlock()
		if apiURL == "" || apiKey == "" {
			st.loadConfig()
			st.mu.Lock()
			apiURL = st.apiURL
			apiKey = st.apiKey
			cfg = st.config
			st.mu.Unlock()
		}
		if apiURL == "" && cfg != nil {
			apiURL = toString(cfg["server_url"])
		}
		if apiKey == "" && cfg != nil {
			apiKey = toString(cfg["auth_code"])
		}
		if apiURL != "" && apiKey != "" {
			fs := dlSpec{Method: "GET", URL: fmt.Sprintf("%s?appid=%s&auth_code=%s", apiURL, appid, apiKey), Headers: map[string]string{"User-Agent": "Mozilla/5.0"}}
			fallbackSpec = &fs
		}
	}

	status, dlerr := streamDownload(spec, zipPath, progress)
	if (status == 403 || status == 429) && fallbackSpec != nil {
		logf(fmt.Sprintf("Hubcap download failed (%d), falling back to default manifest...", status), "info")
		status, dlerr = streamDownload(*fallbackSpec, zipPath, progress)
	}
	if dlerr != nil {
		tmpDirErr := ""
		if ne, ok := dlerr.(net.Error); ok && ne.Timeout() {
			tmpDirErr = "Download timed out. Please try again."
			step("download", "error", "Timeout")
		} else if strings.Contains(dlerr.Error(), "connection refused") || strings.Contains(dlerr.Error(), "no such host") {
			tmpDirErr = "Connection error. Please check your internet."
			step("download", "error", "Connection error")
		} else {
			tmpDirErr = "Download failed. Please try again."
			step("download", "error", "Download failed")
		}
		return map[string]interface{}{"ok": false, "error": tmpDirErr}
	}
	if status == 404 {
		step("download", "error", "Not found (404)")
		return map[string]interface{}{"ok": false, "error": fmt.Sprintf("Game %s not found in database (404)", appid), "not_found": true}
	}
	if status == 403 {
		step("download", "error", "Forbidden (403)")
		return map[string]interface{}{"ok": false, "error": "Access denied (403). Please contact support."}
	}
	if status == 401 {
		step("download", "error", "Unauthorized (401)")
		return map[string]interface{}{"ok": false, "error": "Session expired. Please reconnect your account."}
	}
	if status == 429 {
		step("download", "error", "Rate limited (429)")
		return map[string]interface{}{"ok": false, "error": "Too many requests. Please wait a moment and try again."}
	}
	if status != 200 {
		if toString(settings["provider"]) == "steamtools" && st.steamtoolsLuaOnly(appid, extractPath) {
			logf("Manifest unavailable (HTTP "+fmt.Sprint(status)+") — installing lua only.", "warning")
			status = 200
		} else {
			step("download", "error", fmt.Sprintf("HTTP %d", status))
			return map[string]interface{}{"ok": false, "error": fmt.Sprintf("Download failed (HTTP %d). Please try again.", status)}
		}
	}

	progress(100, "Download complete")
	step("download", "done")
	logf("Download complete", "success")

	step("extract", "active")
	if err := extractZip(zipPath, extractPath); err != nil {
		if toString(settings["provider"]) == "steamtools" && st.steamtoolsLuaOnly(appid, extractPath) {
			logf("Manifest archive unavailable — installing lua only.", "warning")
		} else {
			step("extract", "error", "Bad ZIP")
			return map[string]interface{}{"ok": false, "error": "Downloaded file is not a valid ZIP archive"}
		}
	}
	step("extract", "done")
	logf("Archive extracted", "success")

	step("copy", "active")
	luaDest := filepath.Join(steamPath, "config", "stplug-in")
	manifestDest := filepath.Join(steamPath, "config", "depotcache")
	_ = os.MkdirAll(luaDest, 0o755)
	_ = os.MkdirAll(manifestDest, 0o755)

	luaFile, manifestFile := findLuaManifest(extractPath)
	if luaFile == "" && manifestFile == "" {
		if toString(settings["provider"]) == "steamtools" && st.steamtoolsLuaOnly(appid, extractPath) {
			luaFile, manifestFile = findLuaManifest(extractPath)
		}
		if luaFile == "" && manifestFile == "" {
			step("copy", "error", "No files found")
			return map[string]interface{}{"ok": false, "error": "Archive contains neither .lua nor .manifest files"}
		}
	}
	var luaNames []string
	if luaFile != "" {
		base := filepath.Base(luaFile)
		_ = copyFile(luaFile, filepath.Join(luaDest, base))
		luaNames = append(luaNames, base)
		logf("Lua file copied: "+base, "success")
	}
	if manifestFile != "" {
		base := filepath.Base(manifestFile)
		_ = copyFile(manifestFile, filepath.Join(manifestDest, base))
		logf("Manifest file copied: "+base, "success")
	}
	step("copy", "done")
	logf("Files copied", "success")

	step("register", "active")
	gameName := <-nameCh
	installdir := sanitizeDir(gameName, appid)
	st.registerGame(appid, gameName, installdir, "", luaNames)
	step("register", "done")
	logf("Registered: "+gameName, "success")

	step("restart", "active")
	if toBool(settings["auto_restart"]) || len(missing) > 0 {
		if len(missing) > 0 {
			logf("Required files were missing — forcing Steam restart.", "info")
		}
		logf("Restarting Steam...", "info")
		r := st.launchSteam()
		if ok, _ := r["ok"].(bool); ok {
			step("restart", "done", "Steam restarted")
			logf("Steam restarted", "success")
		} else {
			step("restart", "error", "Steam executable not found")
			logf("Steam executable not found", "warning")
		}
	} else {
		logf("Auto-restart disabled, skipping restart.", "info")
		step("restart", "done", "Skipped")
	}

	return map[string]interface{}{"ok": true, "game": gameName, "appid": appid}
}

func (st *SteamTools) installBatch(appids []string) map[string]interface{} {
	if len(appids) == 0 {
		return map[string]interface{}{"ok": false, "error": "No games to install"}
	}

	logf := func(msg, t string) { st.cb(map[string]interface{}{"type": "log", "msg": msg, "logType": t}) }
	progress := func(pct int, label string) {
		st.cb(map[string]interface{}{"type": "progress", "pct": pct, "label": label})
	}
	gameProgress := func(appid string, pct int, label string) {
		st.cb(map[string]interface{}{"type": "game_progress", "appid": appid, "pct": pct, "label": label})
	}

	settings := st.getSettings()
	total := len(appids)
	results := make([]map[string]interface{}, total)
	successCount := 0

	steam := st.getSteamStatus()
	if ok, _ := steam["detected"].(bool); !ok {
		return map[string]interface{}{"ok": false, "error": "Steam not found. Check Settings."}
	}
	steamPath := toString(steam["path"])
	logf("Steam detected: "+steamPath, "success")

	var missing []string
	for _, d := range requiredDLLs {
		if _, err := os.Stat(filepath.Join(steamPath, d)); err != nil {
			missing = append(missing, d)
		}
	}

	if isSteamRunning() && (toBool(settings["auto_close_steam"]) || len(missing) > 0) {
		if len(missing) > 0 {
			logf("Required files missing — forcing Steam close.", "warning")
		}
		logf("Stopping Steam for batch install...", "info")
		stopSteam()
		time.Sleep(3 * time.Second)
	}

	if toBool(settings["auto_install_ost"]) || len(missing) > 0 {
		if len(missing) > 0 {
			logf("Required files missing: "+strings.Join(missing, ", "), "warning")
		}
		if res := st.installBSTFix(steamPath, logf); !toBool(res["ok"]) {
			logf("BetterSteamTools install failed: "+toString(res["error"]), "warning")
		}
	}

	for i, appid := range appids {
		appid = strings.TrimSpace(appid)
		pctBase := (i * 100) / total
		progress(pctBase, fmt.Sprintf("Installing %d/%d...", i+1, total))

		logf(fmt.Sprintf("--- [%d/%d] Game %s ---", i+1, total, appid), "info")
		gameProgress(appid, 0, "Starting...")

		gameName := st.fetchGameName(appid)
		if gameName == "" {
			gameName = "Game_" + appid
		}

		gameProgress(appid, 10, "Downloading...")
		spec, err := st.manifestDownload(appid)
		if err != nil {
			results[i] = map[string]interface{}{"ok": false, "appid": appid, "error": err.Error()}
			logf(fmt.Sprintf("[%d/%d] %s download failed: %s", i+1, total, appid, err.Error()), "error")
			gameProgress(appid, -1, "Failed")
			continue
		}
		logf(fmt.Sprintf("[%d/%d] Downloading %s via %s...", i+1, total, appid, effectiveProvider(settings)), "info")

		tmpDir, err := os.MkdirTemp("", "steamtools-batch-*")
		if err != nil {
			results[i] = map[string]interface{}{"ok": false, "appid": appid, "error": "Temp dir failed"}
			gameProgress(appid, -1, "Failed")
			continue
		}
		zipPath := filepath.Join(tmpDir, appid+".zip")
		extractPath := filepath.Join(tmpDir, "extracted")

		var fallbackSpec *dlSpec
		if toString(settings["provider"]) == "hubcap" {
			st.mu.Lock()
			apiURL := st.apiURL
			apiKey := st.apiKey
			cfg := st.config
			st.mu.Unlock()
			if apiURL == "" || apiKey == "" {
				st.loadConfig()
				st.mu.Lock()
				apiURL = st.apiURL
				apiKey = st.apiKey
				cfg = st.config
				st.mu.Unlock()
			}
			if apiURL == "" && cfg != nil {
				apiURL = toString(cfg["server_url"])
			}
			if apiKey == "" && cfg != nil {
				apiKey = toString(cfg["auth_code"])
			}
			if apiURL != "" && apiKey != "" {
				fs := dlSpec{Method: "GET", URL: fmt.Sprintf("%s?appid=%s&auth_code=%s", apiURL, appid, apiKey), Headers: map[string]string{"User-Agent": "Mozilla/5.0"}}
				fallbackSpec = &fs
			}
		}

		gameDlProgress := func(pct int, label string) { gameProgress(appid, pct, label) }
		status, dlerr := streamDownload(spec, zipPath, gameDlProgress)
		if (status == 403 || status == 429) && fallbackSpec != nil {
			logf(fmt.Sprintf("[%d/%d] Primary download failed (%d), trying fallback...", i+1, total, status), "info")
			status, dlerr = streamDownload(*fallbackSpec, zipPath, gameDlProgress)
		}
		if dlerr != nil {
			tmpDirErr := "Download failed. Please try again."
			if ne, ok := dlerr.(net.Error); ok && ne.Timeout() {
				tmpDirErr = "Download timed out."
			} else if strings.Contains(dlerr.Error(), "connection refused") || strings.Contains(dlerr.Error(), "no such host") {
				tmpDirErr = "Connection error."
			}
			results[i] = map[string]interface{}{"ok": false, "appid": appid, "error": tmpDirErr}
			logf(fmt.Sprintf("[%d/%d] %s: %s", i+1, total, appid, tmpDirErr), "error")
			os.RemoveAll(tmpDir)
			gameProgress(appid, -1, "Failed")
			continue
		}
		if status == 404 {
			results[i] = map[string]interface{}{"ok": false, "appid": appid, "error": "Game not found (404)", "not_found": true}
			logf(fmt.Sprintf("[%d/%d] %s not found", i+1, total, appid), "error")
			os.RemoveAll(tmpDir)
			gameProgress(appid, -1, "Failed")
			continue
		}
		if status == 403 {
			results[i] = map[string]interface{}{"ok": false, "appid": appid, "error": "Access denied (403)"}
			logf(fmt.Sprintf("[%d/%d] %s access denied", i+1, total, appid), "error")
			os.RemoveAll(tmpDir)
			gameProgress(appid, -1, "Failed")
			continue
		}
		if status == 401 {
			results[i] = map[string]interface{}{"ok": false, "appid": appid, "error": "Session expired. Please reconnect."}
			logf(fmt.Sprintf("[%d/%d] %s session expired", i+1, total, appid), "error")
			os.RemoveAll(tmpDir)
			gameProgress(appid, -1, "Failed")
			continue
		}
		if status == 429 {
			results[i] = map[string]interface{}{"ok": false, "appid": appid, "error": "Rate limited. Please wait and try again."}
			logf(fmt.Sprintf("[%d/%d] %s rate limited", i+1, total, appid), "error")
			os.RemoveAll(tmpDir)
			gameProgress(appid, -1, "Failed")
			continue
		}
		if status != 200 {
			if toString(settings["provider"]) == "steamtools" && st.steamtoolsLuaOnly(appid, extractPath) {
				logf(fmt.Sprintf("[%d/%d] Manifest unavailable (HTTP %d) — installing lua only.", i+1, total, status), "warning")
				status = 200
			} else {
				results[i] = map[string]interface{}{"ok": false, "appid": appid, "error": fmt.Sprintf("Download failed (HTTP %d)", status)}
				logf(fmt.Sprintf("[%d/%d] %s: HTTP %d", i+1, total, appid, status), "error")
				os.RemoveAll(tmpDir)
				gameProgress(appid, -1, "Failed")
				continue
			}
		}
		gameProgress(appid, 60, "Extracting...")

		if err := extractZip(zipPath, extractPath); err != nil {
			if toString(settings["provider"]) == "steamtools" && st.steamtoolsLuaOnly(appid, extractPath) {
				logf(fmt.Sprintf("[%d/%d] Archive unavailable — installing lua only.", i+1, total), "warning")
			} else {
				results[i] = map[string]interface{}{"ok": false, "appid": appid, "error": "Invalid ZIP archive"}
				logf(fmt.Sprintf("[%d/%d] %s: bad ZIP", i+1, total, appid), "error")
				os.RemoveAll(tmpDir)
				gameProgress(appid, -1, "Failed")
				continue
			}
		}
		gameProgress(appid, 75, "Copying files...")

		luaDest := filepath.Join(steamPath, "config", "stplug-in")
		manifestDest := filepath.Join(steamPath, "config", "depotcache")
		_ = os.MkdirAll(luaDest, 0o755)
		_ = os.MkdirAll(manifestDest, 0o755)

		luaFile, manifestFile := findLuaManifest(extractPath)
		if luaFile == "" && manifestFile == "" {
			if toString(settings["provider"]) == "steamtools" && st.steamtoolsLuaOnly(appid, extractPath) {
				luaFile, manifestFile = findLuaManifest(extractPath)
			}
			if luaFile == "" && manifestFile == "" {
				results[i] = map[string]interface{}{"ok": false, "appid": appid, "error": "No .lua or .manifest files found"}
				logf(fmt.Sprintf("[%d/%d] %s: no files found", i+1, total, appid), "error")
				os.RemoveAll(tmpDir)
				gameProgress(appid, -1, "Failed")
				continue
			}
		}
		var luaNames []string
		if luaFile != "" {
			base := filepath.Base(luaFile)
			_ = copyFile(luaFile, filepath.Join(luaDest, base))
			luaNames = append(luaNames, base)
		}
		if manifestFile != "" {
			base := filepath.Base(manifestFile)
			_ = copyFile(manifestFile, filepath.Join(manifestDest, base))
		}
		os.RemoveAll(tmpDir)

		installdir := sanitizeDir(gameName, appid)
		st.registerGame(appid, gameName, installdir, "", luaNames)

		gameProgress(appid, 100, "Done")
		results[i] = map[string]interface{}{"ok": true, "appid": appid, "game": gameName}
		successCount++
		logf(fmt.Sprintf("[%d/%d] %s installed successfully", i+1, total, gameName), "success")
	}

	if toBool(settings["auto_restart"]) || len(missing) > 0 {
		if len(missing) > 0 {
			logf("Required files were missing — forcing Steam restart.", "info")
		}
		logf("Restarting Steam after batch install...", "info")
		r := st.launchSteam()
		if ok, _ := r["ok"].(bool); ok {
			logf("Steam restarted", "success")
		} else {
			logf("Steam executable not found", "warning")
		}
	}

	progress(100, fmt.Sprintf("Batch complete: %d/%d succeeded", successCount, total))
	return map[string]interface{}{
		"ok":      successCount > 0,
		"total":   total,
		"success": successCount,
		"failed":  total - successCount,
		"results": results,
	}
}

func (st *SteamTools) updateGame(appid string) map[string]interface{} {
	appid = strings.TrimSpace(appid)
	step := func(sid, status string, detail ...string) {
		d := ""
		if len(detail) > 0 {
			d = detail[0]
		}
		st.cb(map[string]interface{}{"type": "step", "stepId": sid, "status": status, "detail": d})
	}
	logf := func(msg, t string) { st.cb(map[string]interface{}{"type": "log", "msg": msg, "logType": t}) }
	progress := func(pct int, label string) {
		st.cb(map[string]interface{}{"type": "progress", "pct": pct, "label": label})
	}

	step("detect", "active")
	steam := st.getSteamStatus()
	if ok, _ := steam["detected"].(bool); !ok {
		step("detect", "error", "Steam not found")
		return map[string]interface{}{"ok": false, "error": "Steam not found. Check Settings."}
	}
	steamPath := toString(steam["path"])
	step("detect", "done", steamPath)
	logf("Steam detected: "+steamPath, "success")

	gameName := "Game_" + appid
	if res := st.getAvailableGames("", 0); res != nil {
		if ok, _ := res["ok"].(bool); ok {
			for _, g := range toGameList(res["games"]) {
				if toString(g["appid"]) == appid {
					if n := toString(g["name"]); n != "" {
						gameName = n
					}
					break
				}
			}
		}
	}

	settings := st.getSettings()

	step("download", "active")
	progress(0, "Starting download...")
	spec, err := st.manifestDownload(appid)
	if err != nil {
		step("download", "error")
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	logf(fmt.Sprintf("Downloading update for ID %s via %s...", appid, effectiveProvider(settings)), "info")

	tmpDir, err := os.MkdirTemp("", "steamtools-update-*")
	if err != nil {
		step("download", "error")
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	defer os.RemoveAll(tmpDir)
	zipPath := filepath.Join(tmpDir, appid+".zip")
	extractPath := filepath.Join(tmpDir, "extracted")

	status, dlerr := streamDownload(spec, zipPath, progress)
	if dlerr != nil {
		if ne, ok := dlerr.(net.Error); ok && ne.Timeout() {
			step("download", "error", "Timeout")
			return map[string]interface{}{"ok": false, "error": "Download timed out. Please try again."}
		}
		step("download", "error", "Download failed")
		return map[string]interface{}{"ok": false, "error": "Download failed. Please try again."}
	}
	if status == 404 {
		step("download", "error", "Not found (404)")
		return map[string]interface{}{"ok": false, "error": fmt.Sprintf("Game %s not found in database (404)", appid), "not_found": true}
	}
	if status != 200 {
		step("download", "error", fmt.Sprintf("HTTP %d", status))
		return map[string]interface{}{"ok": false, "error": fmt.Sprintf("Download failed (HTTP %d). Please try again.", status)}
	}

	progress(100, "Download complete")
	step("download", "done")
	logf("Download complete", "success")

	step("extract", "active")
	if err := extractZip(zipPath, extractPath); err != nil {
		step("extract", "error", "Bad ZIP")
		return map[string]interface{}{"ok": false, "error": "Downloaded file is not a valid ZIP archive"}
	}
	step("extract", "done")
	logf("Archive extracted", "success")

	step("copy", "active")
	luaDest := filepath.Join(steamPath, "config", "stplug-in")
	manifestDest := filepath.Join(steamPath, "config", "depotcache")
	_ = os.MkdirAll(luaDest, 0o755)
	_ = os.MkdirAll(manifestDest, 0o755)

	luaFile, manifestFile := findLuaManifest(extractPath)
	if luaFile == "" || manifestFile == "" {
		step("copy", "error", "Missing .lua or .manifest")
		return map[string]interface{}{"ok": false, "error": "Archive is missing .lua or .manifest files"}
	}
	luaBase := filepath.Base(luaFile)
	_ = copyFile(luaFile, filepath.Join(luaDest, luaBase))
	_ = copyFile(manifestFile, filepath.Join(manifestDest, filepath.Base(manifestFile)))
	step("copy", "done")
	logf("Manifest files updated", "success")

	installdir := sanitizeDir(gameName, appid)
	st.registerGame(appid, gameName, installdir, "", []string{luaBase})
	logf("Update registered: "+gameName, "success")

	return map[string]interface{}{"ok": true, "game": gameName, "appid": appid}
}
