package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

var stdHeaders = map[string]string{"User-Agent": "Mozilla/5.0"}

func httpGet(url string, timeout time.Duration, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: timeout}
	return client.Do(req)
}

func httpGetBytes(url string, timeout time.Duration, headers map[string]string) ([]byte, int, error) {
	resp, err := httpGet(url, timeout, headers)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

func httpGetJSON(url string, timeout time.Duration, headers map[string]string) (map[string]interface{}, error) {
	body, _, err := httpGetBytes(url, timeout, headers)
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func httpPostJSON(url string, payload interface{}, timeout time.Duration, headers map[string]string) ([]byte, int, error) {
	data, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}
