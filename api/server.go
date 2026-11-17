package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/XIU2/CloudflareSpeedTest/task"
	"github.com/XIU2/CloudflareSpeedTest/utils"
)

type testRequest struct {
	IP string `json:"ip"`
}

type result struct {
	IP           string  `json:"ip"`
	Sent         int     `json:"sent"`
	Received     int     `json:"received"`
	LossRate     float64 `json:"loss_rate"`
	DelayMs      float64 `json:"delay_ms"`
	DownloadMBps float64 `json:"download_mbps"`
	Colo         string  `json:"colo"`
}

type state struct {
	mu      sync.RWMutex
	running bool
	lastRun time.Time
	lastErr string
	results []result
}

var s = &state{results: make([]result, 0)}

// Start 启动 HTTP API 服务
func Start(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("/api/test", handleTest)
	mux.HandleFunc("/api/result", handleResult)
	mux.HandleFunc("/api/best", handleBest)
	mux.HandleFunc("/api/stats", handleStats)
	mux.HandleFunc("/api/saved", handleSaved)
	mux.HandleFunc("/api/ranges", handleRanges)
	mux.HandleFunc("/api/ips", handleIPs)
	mux.HandleFunc("/api/sources", handleSources)
	mux.HandleFunc("/api/fetch", handleFetch)
	fmt.Printf("API 服务已启动: http://%s\n", addr)
	return http.ListenAndServe(addr, mux)
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	lastRun := ""
	if !s.lastRun.IsZero() {
		lastRun = s.lastRun.Format(time.RFC3339)
	}
	running, count, lastErr := s.running, len(s.results), s.lastErr
	s.mu.RUnlock()
	stage, done, total := utils.ProgressInfo()
	percent := 0.0
	if total > 0 {
		percent = float64(done) / float64(total) * 100
	}
	writeJSON(w, map[string]interface{}{
		"running":   running,
		"last_run":  lastRun,
		"last_error": lastErr,
		"count":     count,
		"progress": map[string]interface{}{
			"stage":   stage,
			"done":    done,
			"total":   total,
			"percent": percent,
		},
	})
}

func handleTest(w http.ResponseWriter, r *http.Request) {
	if preflight(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req testRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeJSON(w, map[string]interface{}{"ok": false, "msg": "请求格式错误"})
		return
	}
	ipText := strings.TrimSpace(req.IP)
	if bad := invalidIP(ipText); bad != "" {
		writeJSON(w, map[string]interface{}{"ok": false, "msg": "IP 格式无效: " + bad})
		return
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		writeJSON(w, map[string]interface{}{"ok": false, "msg": "测速进行中"})
		return
	}
	s.running = true
	s.lastRun = time.Now()
	s.lastErr = ""
	s.mu.Unlock()

	go runTest(ipText)
	writeJSON(w, map[string]interface{}{"ok": true, "msg": "测速已启动"})
}

// invalidIP 返回第一个非法的 IP / IP 段，全部合法则返回空
func invalidIP(text string) string {
	for _, item := range strings.Split(text, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.Contains(item, "/") {
			if _, _, err := net.ParseCIDR(item); err != nil {
				return item
			}
		} else if net.ParseIP(item) == nil {
			return item
		}
	}
	return ""
}

func setTestError(msg string) {
	s.mu.Lock()
	s.lastErr = msg
	s.mu.Unlock()
}

func runTest(ipText string) {
	defer func() {
		if r := recover(); r != nil {
			setTestError(fmt.Sprintf("测速异常: %v", r))
		}
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	if ipText != "" {
		old := task.IPText
		task.IPText = ipText
		defer func() { task.IPText = old }()
	}

	utils.SetStage("延迟测速")
	ping, err := task.NewPing()
	if err != nil {
		setTestError(err.Error())
		return
	}
	pingData := ping.Run().FilterDelay().FilterLossRate()
	utils.SetStage("下载测速")
	speedData := task.TestDownloadSpeed(pingData)
	utils.SetStage("完成")

	results := make([]result, 0, len(speedData))
	for _, v := range speedData {
		loss := float64(0)
		if v.Sended > 0 {
			loss = float64(v.Sended-v.Received) / float64(v.Sended)
		}
		colo := v.Colo
		if colo == "" {
			colo = "N/A"
		}
		results = append(results, result{
			IP:           v.IP.String(),
			Sent:         v.Sended,
			Received:     v.Received,
			LossRate:     loss,
			DelayMs:      v.Delay.Seconds() * 1000,
			DownloadMBps: v.DownloadSpeed / 1024 / 1024,
			Colo:         colo,
		})
	}
	s.mu.Lock()
	s.results = results
	s.mu.Unlock()
}

func handleResult(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	results := s.results
	s.mu.RUnlock()
	writeJSON(w, map[string]interface{}{
		"count":   len(results),
		"results": results,
	})
}

func handleBest(w http.ResponseWriter, r *http.Request) {
	n := 10
	if v := r.URL.Query().Get("n"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			n = i
		}
	}
	s.mu.RLock()
	results := s.results
	s.mu.RUnlock()
	if n > len(results) {
		n = len(results)
	}
	ips := make([]string, 0, n)
	for _, v := range results[:n] {
		ips = append(ips, v.IP)
	}
	writeJSON(w, map[string]interface{}{"count": n, "ips": ips})
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	results := s.results
	s.mu.RUnlock()
	stats := map[string]interface{}{
		"total": len(results),
	}
	if len(results) == 0 {
		writeJSON(w, stats)
		return
	}
	var sumDelay, minDelay, maxDelay float64
	var sumSpeed, maxSpeed float64
	var sumLoss float64
	colo := make(map[string]int)
	minDelay = results[0].DelayMs
	for _, v := range results {
		sumDelay += v.DelayMs
		sumSpeed += v.DownloadMBps
		sumLoss += v.LossRate
		if v.DelayMs < minDelay {
			minDelay = v.DelayMs
		}
		if v.DelayMs > maxDelay {
			maxDelay = v.DelayMs
		}
		if v.DownloadMBps > maxSpeed {
			maxSpeed = v.DownloadMBps
		}
		colo[v.Colo]++
	}
	n := float64(len(results))
	stats["avg_delay_ms"] = sumDelay / n
	stats["min_delay_ms"] = minDelay
	stats["max_delay_ms"] = maxDelay
	stats["avg_download_mbps"] = sumSpeed / n
	stats["max_download_mbps"] = maxSpeed
	stats["avg_loss_rate"] = sumLoss / n
	stats["colo"] = colo
	writeJSON(w, stats)
}

func handleIPs(w http.ResponseWriter, r *http.Request) {
	task.RangesMu.Lock()
	ranges, err := task.LoadRanges(task.IPFile)
	task.RangesMu.Unlock()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]interface{}{
		"file":   task.IPFile,
		"count":  len(ranges),
		"ranges": ranges,
	})
}

func handleSources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"count":   len(task.IPSources),
		"sources": task.IPSources,
	})
}

func handleFetch(w http.ResponseWriter, r *http.Request) {
	if preflight(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		writeJSON(w, map[string]interface{}{"ok": false, "msg": "测速进行中，请稍后再试"})
		return
	}
	s.mu.Unlock()
	go task.FetchIPRanges()
	writeJSON(w, map[string]interface{}{"ok": true, "msg": "IP 源拉取已启动"})
}

func preflight(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodOptions {
		return false
	}
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusNoContent)
	return true
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "msg": msg})
}
