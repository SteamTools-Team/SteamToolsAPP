package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (st *SteamTools) installBSTFix(steamPath string, logf func(msg, t string)) map[string]interface{} {
	_log := logf
	if _log == nil {
		_log = func(msg, t string) { fmt.Println("[BetterSteamTools]", msg) }
	}

	bstVersion, bstURL := resolveBSTRelease()
	_log("Downloading BetterSteamTools v"+bstVersion+" (Release)...", "info")
	tmp, err := os.MkdirTemp("", "bst-*")
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	defer os.RemoveAll(tmp)

	zipPath := filepath.Join(tmp, "bst.zip")
	status, err := streamDownload(dlSpec{Method: "GET", URL: bstURL, Headers: map[string]string{"User-Agent": "Mozilla/5.0"}}, zipPath, nil)
	if err != nil || status != 200 {
		msg := "BetterSteamTools download failed"
		if err != nil {
			msg += ": " + err.Error()
		} else {
			msg += fmt.Sprintf(" (HTTP %d)", status)
		}
		return map[string]interface{}{"ok": false, "error": msg}
	}

	if isSteamRunning() {
		_log("Stopping Steam...", "warning")
		stopSteam()
		time.Sleep(2 * time.Second)
	}

	return bstCopyZip(zipPath, steamPath, bstVersion, _log)
}

func resolveBSTRelease() (version, zipURL string) {
	version, zipURL = BST_VERSION, BST_ZIP_URL

	body, status, err := httpGetBytes(
		"https://api.github.com/repos/madoiscool/BetterSteamTools/releases/latest",
		15*time.Second, map[string]string{"User-Agent": "Mozilla/5.0"})
	if err != nil || status != 200 {
		return
	}
	var rel map[string]interface{}
	if json.Unmarshal(body, &rel) != nil {
		return
	}

	var firstZip, releaseZip string
	for _, a := range toGameList(rel["assets"]) {
		name := strings.ToLower(toString(a["name"]))
		url := toString(a["browser_download_url"])
		if url == "" || !strings.HasSuffix(name, ".zip") {
			continue
		}
		if firstZip == "" {
			firstZip = url
		}
		if strings.HasSuffix(name, "-release.zip") {
			releaseZip = url
			break
		}
	}
	chosen := releaseZip
	if chosen == "" {
		chosen = firstZip
	}
	if chosen == "" {
		return
	}

	if tag := strings.TrimPrefix(toString(rel["tag_name"]), "v"); tag != "" {
		version = tag
	}
	zipURL = chosen
	return
}

func bstCopyZip(zipPath, steamPath, version string, _log func(msg, t string)) map[string]interface{} {
	if _log == nil {
		_log = func(msg, t string) { fmt.Println("[BetterSteamTools]", msg) }
	}
	tmp, err := os.MkdirTemp("", "bstx-*")
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	defer os.RemoveAll(tmp)

	extractPath := filepath.Join(tmp, "extracted")
	if err := extractZip(zipPath, extractPath); err != nil {
		return map[string]interface{}{"ok": false, "error": "Extraction failed: " + err.Error()}
	}

	var copied []string
	var walkErr error
	_ = filepath.Walk(extractPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(extractPath, path)
		if relErr != nil || rel == "." {
			return nil
		}

		if !strings.HasSuffix(strings.ToLower(path), ".dll") {
			return nil
		}
		dest := filepath.Join(steamPath, rel)
		_ = os.MkdirAll(filepath.Dir(dest), 0o755)
		var copyErr error
		for attempt := 0; attempt < 3; attempt++ {
			copyErr = copyFile(path, dest)
			if copyErr == nil {
				break
			}
			time.Sleep(time.Second)
		}
		if copyErr != nil {
			walkErr = copyErr
			return copyErr
		}
		copied = append(copied, rel)
		return nil
	})
	if walkErr != nil {
		return map[string]interface{}{"ok": false, "error": walkErr.Error()}
	}
	if len(copied) == 0 {
		return map[string]interface{}{"ok": false, "error": "No files found in BetterSteamTools package"}
	}
	_log(fmt.Sprintf("%d file(s) installed to Steam folder", len(copied)), "success")

	luaDir := filepath.Join(steamPath, "config", "stplug-in")
	_ = os.MkdirAll(luaDir, 0o755)
	_log("Lua config directory ensured: "+luaDir, "info")

	return map[string]interface{}{"ok": true, "version": version, "copied": copied}
}
