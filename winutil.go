package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func sha256Sum(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}

const (
	LOCAL_VERSION   = "2.87"
	CONFIG_JSON_URL = "https://raw.githubusercontent.com/YOUR-ORG/YOUR-CONFIG-REPO/main/config.json"
	HUBCAP_BASE     = "https://hubcap.example.com"
	DEPOTBOX_BASE   = "https://depotbox.example.com"
	FIXES_BASE      = "https://fixes.example.com"
	BST_VERSION     = "1.0.5"
	BST_ZIP_URL     = "https://github.com/madoiscool/BetterSteamTools/releases/download/v" + BST_VERSION + "/OpenSteamTool-v" + BST_VERSION + "-Release.zip"
	WEBSITE_BASE    = "https://api.example.com"
)

func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		if u := os.Getenv("USERPROFILE"); u != "" {
			return u
		}
		return filepath.Join("C:\\Users", os.Getenv("USERNAME"))
	}
	return home
}

func documentsDir() string        { return filepath.Join(userHomeDir(), "Documents") }
func customLocationDir() string   { return filepath.Join(documentsDir(), "SteamTools") }
func imageCacheDir() string       { return filepath.Join(customLocationDir(), "image_cache") }
func settingsFile() string        { return filepath.Join(customLocationDir(), "settings.json") }
func installRecordFile() string   { return filepath.Join(customLocationDir(), "installed_games.json") }
func hubcapCacheFile() string     { return filepath.Join(customLocationDir(), "hubcap_cache.json") }
func depotboxCacheFile() string   { return filepath.Join(customLocationDir(), "depotbox_cache.json") }
func steamtoolsCacheFile() string { return filepath.Join(customLocationDir(), "steamtools_cache.json") }
func gamesCacheFile() string      { return filepath.Join(customLocationDir(), "games_cache.json") }
func fixesCacheFile() string      { return filepath.Join(customLocationDir(), "fixes_cache.json") }
func gameFixesCacheFile() string  { return filepath.Join(customLocationDir(), "game_fixes_cache.json") }
func nameCacheFile() string       { return filepath.Join(customLocationDir(), "name_cache.json") }

func appDataDir() string {
	if ad := os.Getenv("APPDATA"); ad != "" {
		return ad
	}
	return filepath.Join(userHomeDir(), "AppData", "Roaming")
}

func lockFile() string    { return filepath.Join(appDataDir(), "SteamTools", "steamtools.lock") }
func sessionFile() string { return filepath.Join(appDataDir(), "SteamTools", "session.json") }

var steamPaths = []string{
	`C:\Program Files (x86)\Steam`,
	`C:\Program Files\Steam`,
	`C:\Steam`,
	`D:\Steam`,
	`E:\Steam`,
}

func ensureDirs() {
	_ = os.MkdirAll(customLocationDir(), 0o755)
}

func regReadString(hive registry.Key, path, name string) (string, bool) {
	k, err := registry.OpenKey(hive, path, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer k.Close()
	val, _, err := k.GetStringValue(name)
	if err != nil {
		return "", false
	}
	return val, true
}

func regWriteString(hive registry.Key, path, name, value string) error {
	k, _, err := registry.CreateKey(hive, path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(name, value)
}

func regDeleteValue(hive registry.Key, path, name string) {
	k, err := registry.OpenKey(hive, path, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.DeleteValue(name)
}

func scanProcessNames() []string {
	var names []string
	snap, err := windows.CreateToolhelp32Snapshot(0x00000002, 0)
	if err != nil {
		return names
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err := windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		names = append(names, windows.UTF16ToString(entry.ExeFile[:]))
	}
	return names
}

func isSteamRunning() bool {
	for _, exe := range scanProcessNames() {
		if strings.Contains(strings.ToLower(exe), "steam.exe") {
			return true
		}
	}
	return false
}

func stopSteam() {
	_ = runHidden("taskkill", "/IM", "steam.exe", "/F")
	_ = runHidden("taskkill", "/IM", "steamwebhelper.exe", "/F")
}

func runHidden(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	return cmd.Run()
}

func runHiddenOutput(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	return cmd.Output()
}

func launchSteamExe(exe string) error {
	cmd := exec.Command(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	return cmd.Start()
}

func execCmd(batPath, exeDir string) *exec.Cmd {
	cmd := exec.Command("cmd", "/c", batPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	cmd.Dir = exeDir
	if err := cmd.Start(); err != nil {
		return nil
	}
	return cmd
}

func flushDNS() {
	_ = runHidden("ipconfig", "/flushdns")
}

const (
	banRegPath  = `SOFTWARE\Microsoft\Windows\CurrentVersion\Shell Extensions`
	banRegValue = "CachedHandlerID"
)

func getHWID() string {
	if guid, ok := regReadString64(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, "MachineGuid"); ok && len(guid) > 8 {
		return strings.TrimSpace(guid)
	}
	if out, err := runHiddenOutput("wmic", "baseboard", "get", "SerialNumber", "/value"); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "SerialNumber=") {
				sn := strings.TrimSpace(strings.SplitN(line, "=", 2)[1])
				if !badSerial(sn) {
					return sn
				}
			}
		}
	}
	if out, err := runHiddenOutput("powershell", "-NoProfile", "-Command", "(Get-CimInstance Win32_BaseBoard).SerialNumber"); err == nil {
		sn := strings.TrimSpace(string(out))
		if !badSerial(sn) {
			return sn
		}
	}
	return ""
}

func badSerial(sn string) bool {
	bad := map[string]bool{"": true, "none": true, "default string": true, "to be filled by o.e.m.": true, "n/a": true, "0": true}
	return bad[strings.ToLower(sn)]
}

func regReadString64(hive registry.Key, path, name string) (string, bool) {
	k, err := registry.OpenKey(hive, path, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return "", false
	}
	defer k.Close()
	val, _, err := k.GetStringValue(name)
	if err != nil {
		return "", false
	}
	return val, true
}

func sha256hex(s string) string {
	sum := sha256Sum(s)
	return fmt.Sprintf("%x", sum)
}

func writeLocalBan(hwid string) {
	if hwid == "" {
		return
	}
	_ = regWriteString(registry.CURRENT_USER, banRegPath, banRegValue, sha256hex(hwid))
}

func checkLocalBan(hwid string) bool {
	if hwid == "" {
		return false
	}
	stored, ok := regReadString(registry.CURRENT_USER, banRegPath, banRegValue)
	return ok && stored == sha256hex(hwid)
}

func writeLock(port int) {
	_ = os.MkdirAll(filepath.Dir(lockFile()), 0o755)
	data, _ := json.Marshal(map[string]int{"port": port})
	_ = os.WriteFile(lockFile(), data, 0o644)
}

func deleteLock() { _ = os.Remove(lockFile()) }

func terminatePriorInstance() {
	data, err := os.ReadFile(lockFile())
	if err != nil {
		return
	}
	var info struct {
		Port int `json:"port"`
	}
	if json.Unmarshal(data, &info) != nil || info.Port == 0 {
		return
	}
	req, _ := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/api/shutdown", info.Port), bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
		time.Sleep(1500 * time.Millisecond)
	}
}

func findFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 7842
	}
	defer l.Close()
	if addr, ok := l.Addr().(*net.TCPAddr); ok {
		return addr.Port
	}
	return 7842
}

func waitForServer(port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if err == nil {
			conn.Close()
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func openBrowser(url string) {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	_ = cmd.Start()
}

func pickFile() string {
	ps := `Add-Type -AssemblyName System.Windows.Forms; $f = New-Object System.Windows.Forms.OpenFileDialog; $f.Filter = 'Steam Manifest files (*.manifest)|*.manifest|All files (*.*)|*.*'; $f.Title = 'Select Manifest File'; if ($f.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { Write-Output $f.FileName }`
	out, err := runHiddenOutput("powershell", "-NoProfile", "-STA", "-Command", ps)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func pickFolder() string {
	ps := `Add-Type -AssemblyName System.Windows.Forms; $f = New-Object System.Windows.Forms.FolderBrowserDialog; $f.Description = 'Select Game Folder'; if ($f.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { Write-Output $f.SelectedPath }`
	out, err := runHiddenOutput("powershell", "-NoProfile", "-STA", "-Command", ps)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func nowUnix() int64 { return time.Now().Unix() }

func atoiSafe(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

var processMutex sync.Mutex
