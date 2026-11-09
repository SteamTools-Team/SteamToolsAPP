package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procGetDiskFreeSpaceEx = kernel32.NewProc("GetDiskFreeSpaceExW")
)

func diskUsage(path string) (total, used, free uint64) {
	freeBytes := uint64(0)
	totalBytes := uint64(0)
	totalFree := uint64(0)
	ptr, _ := syscall.UTF16PtrFromString(path)
	r, _, _ := procGetDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(ptr)),
		uintptr(unsafe.Pointer(&freeBytes)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r == 0 {
		return 0, 0, 0
	}
	return totalBytes, totalBytes - freeBytes, freeBytes
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

func (st *SteamTools) runDiagnostic() map[string]interface{} {
	result := map[string]interface{}{}
	var issues []string

	steam := st.getSteamStatus()
	result["detect"] = map[string]interface{}{"ok": steam["detected"], "path": steam["path"], "running": steam["running"]}
	if ok, _ := steam["detected"].(bool); !ok {
		issues = append(issues, "Steam not detected")
		return map[string]interface{}{"ok": false, "issues": issues, "results": result}
	}
	p := toString(steam["path"])

	critFiles := []string{"steam.exe", "steamapps/libraryfolders.vdf", "config/loginusers.vdf"}
	var filesRes []map[string]interface{}
	for _, f := range critFiles {
		ok := false
		if _, err := os.Stat(filepath.Join(p, filepath.FromSlash(f))); err == nil {
			ok = true
		}
		filesRes = append(filesRes, map[string]interface{}{"name": f, "ok": ok})
	}
	result["files"] = map[string]interface{}{"files": filesRes}
	missingFiles := 0
	for _, f := range filesRes {
		if ok, _ := f["ok"].(bool); !ok {
			missingFiles++
		}
	}
	if missingFiles > 0 {
		issues = append(issues, fmt.Sprintf("%d critical file(s) missing", missingFiles))
	}

	critDirs := []string{"steamapps", "steamapps/common", "config", "userdata", "config/stplug-in", "config/depotcache", "logs", "appcache"}
	var dirsRes []map[string]interface{}
	for _, d := range critDirs {
		ok := false
		if fi, err := os.Stat(filepath.Join(p, filepath.FromSlash(d))); err == nil && fi.IsDir() {
			ok = true
		}
		dirsRes = append(dirsRes, map[string]interface{}{"name": d, "ok": ok})
	}
	result["dirs"] = map[string]interface{}{"dirs": dirsRes}
	missingDirs := 0
	for _, d := range dirsRes {
		if ok, _ := d["ok"].(bool); !ok {
			missingDirs++
		}
	}
	if missingDirs > 0 {
		issues = append(issues, fmt.Sprintf("%d director(ies) missing", missingDirs))
	}

	userdata := filepath.Join(p, "userdata")
	var users []map[string]interface{}
	if entries, err := os.ReadDir(userdata); err == nil {
		for _, uid := range entries {
			if !isDigits(uid.Name()) {
				continue
			}
			cfgOk := false
			if fi, err := os.Stat(filepath.Join(userdata, uid.Name(), "config")); err == nil && fi.IsDir() {
				cfgOk = true
			}
			users = append(users, map[string]interface{}{"id": uid.Name(), "configOk": cfgOk})
		}
	}
	result["users"] = map[string]interface{}{"count": len(users), "users": users}
	if len(users) == 0 {
		issues = append(issues, "No Steam users found")
	}

	var procs []map[string]interface{}
	for _, exe := range scanProcessNames() {
		if strings.Contains(strings.ToLower(exe), "steam") {
			procs = append(procs, map[string]interface{}{"name": exe, "pid": 0})
		}
	}
	result["processes"] = map[string]interface{}{"processes": procs}

	total, used, free := diskUsage(p)
	totalGB := round1(float64(total) / (1024 * 1024 * 1024))
	usedGB := round1(float64(used) / (1024 * 1024 * 1024))
	freeGB := round1(float64(free) / (1024 * 1024 * 1024))
	result["disk"] = map[string]interface{}{"totalGB": totalGB, "usedGB": usedGB, "freeGB": freeGB}
	if freeGB < 5 {
		issues = append(issues, fmt.Sprintf("Low disk space (%v GB free)", freeGB))
	}

	return map[string]interface{}{"ok": len(issues) == 0, "issues": issues, "results": result}
}

func (st *SteamTools) checkMissingDlls() map[string]interface{} {
	steam := st.getSteamStatus()
	if ok, _ := steam["detected"].(bool); !ok {
		return map[string]interface{}{"ok": false, "missing": []string{}, "error": "Steam not found"}
	}
	steamPath := toString(steam["path"])
	var missing []string
	for _, d := range requiredDLLs {
		if _, err := os.Stat(filepath.Join(steamPath, d)); err != nil {
			missing = append(missing, d)
		}
	}
	return map[string]interface{}{"ok": true, "missing": missing, "all_present": len(missing) == 0}
}

func (st *SteamTools) repairSteam(cloudredirect, bst bool) map[string]interface{} {
	step := func(sid, status string, detail ...string) {
		d := ""
		if len(detail) > 0 {
			d = detail[0]
		}
		st.cb(map[string]interface{}{"type": "step", "stepId": sid, "status": status, "detail": d})
	}
	logf := func(msg, t string) { st.cb(map[string]interface{}{"type": "log", "msg": msg, "logType": t}) }
	var repairs []string

	step("kill", "active")
	steam := st.getSteamStatus()
	if ok, _ := steam["detected"].(bool); !ok {
		step("kill", "error", "Not found")
		return map[string]interface{}{"ok": false, "repairs": repairs, "error": "Steam not found"}
	}
	steamPath := toString(steam["path"])
	logf("Steam found: "+steamPath, "success")

	stopSteam()
	time.Sleep(2 * time.Second)
	step("kill", "done", "Steam closed")

	step("cache", "active")
	cacheDirs := []string{"appcache", "htmlcache", filepath.Join("config", "htmlcache"), "steamui", "clientui"}
	cleaned := 0
	for _, cd := range cacheDirs {
		full := filepath.Join(steamPath, cd)
		if fi, err := os.Stat(full); err == nil && fi.IsDir() {
			_ = os.RemoveAll(full)
			cleaned++
		}
	}
	repairs = append(repairs, fmt.Sprintf("%d cache folder(s) cleared", cleaned))
	step("cache", "done", fmt.Sprintf("%d folders cleared", cleaned))
	logf(fmt.Sprintf("Cleaned %d cache directories", cleaned), "success")

	step("temp", "active")
	tmpFiles := []string{"steam.pid", "WriteMiniDump.lock", "updating"}
	removed := 0
	for _, tf := range tmpFiles {
		fp := filepath.Join(steamPath, tf)
		if fi, err := os.Stat(fp); err == nil && !fi.IsDir() {
			if os.Remove(fp) == nil {
				removed++
			}
		}
	}
	repairs = append(repairs, fmt.Sprintf("%d temp file(s) removed", removed))
	step("temp", "done", fmt.Sprintf("%d files removed", removed))
	logf(fmt.Sprintf("Removed %d temporary file(s)", removed), "success")

	step("package", "active")
	pkg := filepath.Join(steamPath, "package")
	if fi, err := os.Stat(pkg); err == nil && fi.IsDir() {
		_ = os.RemoveAll(pkg)
		_ = os.MkdirAll(pkg, 0o755)
		repairs = append(repairs, "Package folder reset")
	}
	step("package", "done")
	logf("Package folder reset", "success")

	step("logs", "active")
	logsPath := filepath.Join(steamPath, "logs")
	cleanedLogs := 0
	if fi, err := os.Stat(logsPath); err == nil && fi.IsDir() {
		if entries, err := os.ReadDir(logsPath); err == nil {
			for _, lf := range entries {
				if strings.HasSuffix(strings.ToLower(lf.Name()), ".log") {
					if os.Remove(filepath.Join(logsPath, lf.Name())) == nil {
						cleanedLogs++
					}
				}
			}
		}
	}
	repairs = append(repairs, fmt.Sprintf("%d log file(s) cleaned", cleanedLogs))
	step("logs", "done", fmt.Sprintf("%d log files removed", cleanedLogs))
	logf(fmt.Sprintf("Cleaned %d Steam log file(s)", cleanedLogs), "success")

	step("dns", "active")
	flushDNS()
	repairs = append(repairs, "DNS cache flushed")
	step("dns", "done")
	logf("DNS cache flushed", "success")

	if cloudredirect {
		step("cloudredirect", "active")
		err := st.applyCloudRedirect(steamPath, logf)
		if err != nil {
			step("cloudredirect", "error", err.Error())
			logf("CloudRedirect failed: "+err.Error(), "warning")
		} else {
			repairs = append(repairs, "CloudRedirect fix applied")
			step("cloudredirect", "done")
			logf("CloudRedirect completed", "success")
		}
	}

	if bst {
		step("bettersteamtools", "active")
		res := st.installBSTFix(steamPath, logf)
		if ok, _ := res["ok"].(bool); ok {
			repairs = append(repairs, fmt.Sprintf("BetterSteamTools v%s installed", toString(res["version"])))
			step("bettersteamtools", "done", "v"+toString(res["version"]))
		} else {
			step("bettersteamtools", "error", toString(res["error"]))
			logf("BetterSteamTools failed: "+toString(res["error"]), "warning")
		}
	}

	step("restart", "active")
	logf("Restarting Steam...", "info")
	launched := false
	for _, path := range steamPaths {
		exe := filepath.Join(path, "steam.exe")
		if _, err := os.Stat(exe); err == nil {
			if launchSteamExe(exe) == nil {
				launched = true
			}
			break
		}
	}
	if launched {
		repairs = append(repairs, "Steam restarted")
		step("restart", "done", "Steam restarted")
		logf("Steam restarted", "success")
	} else {
		step("restart", "error", "Steam executable not found")
		logf("Steam executable not found", "warning")
	}

	st.invalidateSteamCache()
	return map[string]interface{}{"ok": true, "repairs": repairs}
}

func streamDownloadTo(spec dlSpec, destPath string) error {
	status, err := streamDownload(spec, destPath, nil)
	if err != nil {
		return err
	}
	if status != 200 {
		return fmt.Errorf("HTTP %d", status)
	}
	return nil
}

func (st *SteamTools) applyCloudRedirect(steamPath string, logf func(msg, t string)) error {
	tmpDir := os.TempDir()
	cliPath := filepath.Join(tmpDir, "CloudRedirectCLI.exe")
	dllTmp := filepath.Join(tmpDir, "cloud_redirect.dll")

	logf("Fetching latest CloudRedirect release...", "info")
	body, status, err := httpGetBytes("https://api.github.com/repos/Selectively11/CloudRedirect/releases/latest", 15*time.Second, map[string]string{"User-Agent": "Mozilla/5.0"})
	if err != nil || status != 200 {
		if err == nil {
			err = fmt.Errorf("HTTP %d", status)
		}
		return fmt.Errorf("CloudRedirect release fetch failed: %v", err)
	}
	var rel map[string]interface{}
	if err := json.Unmarshal(body, &rel); err != nil {
		return fmt.Errorf("CloudRedirect release parse failed")
	}
	logf("Latest version: "+toString(rel["tag_name"]), "info")
	assets := toGameList(rel["assets"])
	var cliAsset, dllAsset map[string]interface{}
	for _, a := range assets {
		if toString(a["name"]) == "CloudRedirectCLI.exe" {
			cliAsset = a
		}
		if toString(a["name"]) == "cloud_redirect.dll" {
			dllAsset = a
		}
	}
	if cliAsset != nil {
		logf("Downloading CloudRedirectCLI.exe...", "info")
		if err := streamDownloadTo(dlSpec{Method: "GET", URL: toString(cliAsset["browser_download_url"]), Headers: map[string]string{"User-Agent": "Mozilla/5.0"}}, cliPath); err != nil {
			return fmt.Errorf("CloudRedirectCLI download failed: %v", err)
		}
		logf("CloudRedirectCLI.exe downloaded", "success")
	}
	if dllAsset != nil {
		logf("Downloading cloud_redirect.dll...", "info")
		if err := streamDownloadTo(dlSpec{Method: "GET", URL: toString(dllAsset["browser_download_url"]), Headers: map[string]string{"User-Agent": "Mozilla/5.0"}}, dllTmp); err != nil {
			return fmt.Errorf("cloud_redirect.dll download failed: %v", err)
		}
		logf("cloud_redirect.dll downloaded", "success")
	}

	if _, err := os.Stat(cliPath); err == nil {
		logf("Running CloudRedirect /stfixer (admin required)...", "info")
		ps := fmt.Sprintf(`Start-Process -FilePath "%s" -ArgumentList "/stfixer" -Verb RunAs -Wait`, cliPath)
		cmd := execCmdPS(ps)
		if cmd != nil {
			done := make(chan struct{})
			go func() {
				_ = cmd.Wait()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(60 * time.Second):
				_ = cmd.Process.Kill()
				logf("CloudRedirectCLI timed out", "warning")
			}
		}
	}

	if _, err := os.Stat(dllTmp); err == nil {
		_ = copyFile(dllTmp, filepath.Join(steamPath, "cloud_redirect.dll"))
		logf("cloud_redirect.dll installed to Steam folder", "success")
	}

	_ = os.Remove(cliPath)
	_ = os.Remove(dllTmp)

	steamExe := filepath.Join(steamPath, "steam.exe")
	if _, err := os.Stat(steamExe); err == nil {
		launchSteamExeArgs(steamExe, []string{"-clearbeta"})
		logf("Steam restarting with -clearbeta (may take longer than usual)", "info")
	}
	return nil
}

func execCmdPS(ps string) *exec.Cmd {

	cmd := exec.Command("powershell", "-NoProfile", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000010}
	if err := cmd.Start(); err != nil {
		return nil
	}
	return cmd
}

func launchSteamExeArgs(exe string, args []string) {
	full := append([]string{exe}, args...)
	cmd := exec.Command(full[0], full[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	_ = cmd.Start()
}
