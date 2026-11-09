package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const githubFixReleaseAPI = "https://api.github.com/repos/skyflarefox/fix/releases/tags/fix"

type ghFixAsset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type ghFixRelease struct {
	Assets []ghFixAsset `json:"assets"`
}

func (st *SteamTools) githubFixes() []map[string]interface{} {
	body, status, err := httpGetBytes(githubFixReleaseAPI, 20*time.Second, map[string]string{
		"User-Agent": "SteamTools",
		"Accept":     "application/vnd.github+json",
	})
	if err != nil || status != 200 {
		return nil
	}
	var rel ghFixRelease
	if json.Unmarshal(body, &rel) != nil {
		return nil
	}

	names, _ := readJSONMap(nameCacheFile())
	out := make([]map[string]interface{}, 0, len(rel.Assets))
	for _, a := range rel.Assets {
		if !strings.HasSuffix(strings.ToLower(a.Name), ".zip") || a.BrowserDownloadURL == "" {
			continue
		}
		base := strings.TrimSuffix(a.Name, ".zip")
		us := strings.LastIndex(base, "_")
		if us <= 0 {
			continue
		}
		appid := base[:us]
		variant := base[us+1:]
		if !isAllDigits(appid) {
			continue
		}

		name := ""
		if names != nil {
			if n, ok := names[appid].(string); ok {
				name = n
			}
		}

		badges := []string{"SkyFix"}
		if variant != "" {
			badges = append(badges, capitalizeFirst(variant))
		}

		out = append(out, map[string]interface{}{
			"name":         name,
			"appid":        appid,
			"size":         humanSize(a.Size),
			"id":           a.Name,
			"downloadName": a.Name,
			"filename":     a.Name,
			"badges":       badges,
			"tags":         []string{variant},
			"url":          a.BrowserDownloadURL,
			"source":       "github",
		})
	}
	return out
}

func isDirectFixURL(u string) bool {
	low := strings.ToLower(u)
	return strings.Contains(low, "github.com/") || strings.Contains(low, "githubusercontent.com/")
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func humanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
