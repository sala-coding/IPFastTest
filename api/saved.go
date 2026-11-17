package api

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
)

const savedFile = "saved_ips.txt"

var savedMu sync.Mutex

func readSaved() ([]string, error) {
	f, err := os.Open(savedFile)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ips := make([]string, 0)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if ip := strings.TrimSpace(scanner.Text()); ip != "" {
			ips = append(ips, ip)
		}
	}
	return ips, scanner.Err()
}

func writeSaved(ips []string) error {
	f, err := os.Create(savedFile)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, ip := range ips {
		if _, err := w.WriteString(ip + "\n"); err != nil {
			return err
		}
	}
	return w.Flush()
}

func handleSaved(w http.ResponseWriter, r *http.Request) {
	savedMu.Lock()
	defer savedMu.Unlock()

	if r.Method == http.MethodGet {
		ips, err := readSaved()
		if err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "msg": err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{"count": len(ips), "ips": ips})
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req testRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeJSON(w, map[string]interface{}{"ok": false, "msg": "请求格式错误"})
		return
	}
	ip := strings.TrimSpace(req.IP)
	parsed := net.ParseIP(ip)
	if parsed == nil {
		writeJSON(w, map[string]interface{}{"ok": false, "msg": "IP 格式无效: " + ip})
		return
	}
	ip = parsed.String() // 规范化 IPv6 写法

	ips, err := readSaved()
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	found := -1
	for i, v := range ips {
		if v == ip {
			found = i
			break
		}
	}
	if r.Method == http.MethodPost {
		if found < 0 {
			ips = append(ips, ip)
		}
	} else {
		if found < 0 {
			writeJSON(w, map[string]interface{}{"ok": false, "msg": "IP 未保存"})
			return
		}
		ips = append(ips[:found], ips[found+1:]...)
	}
	if err := writeSaved(ips); err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "ip": ip, "count": len(ips)})
}
