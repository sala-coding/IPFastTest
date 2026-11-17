package api

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/XIU2/CloudflareSpeedTest/task"
)

type rangesRequest struct {
	File   string `json:"file"`
	Ranges string `json:"ranges"`
}

// validRange 校验是否为合法 IP 段（CIDR）或单个 IP
func validRange(item string) bool {
	if strings.Contains(item, "/") {
		_, _, err := net.ParseCIDR(item)
		return err == nil
	}
	return net.ParseIP(item) != nil
}

// allowedRangeFile 校验文件路径：仅允许当前目录下的 .txt 文件，防止路径遍历
func allowedRangeFile(file string) bool {
	if file == "" || filepath.IsAbs(file) || strings.Contains(file, "..") {
		return false
	}
	return filepath.Ext(file) == ".txt"
}

func handleRanges(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	if file == "" {
		file = task.IPFile
	}
	if !allowedRangeFile(file) {
		writeError(w, http.StatusBadRequest, "非法文件路径: "+file)
		return
	}
	if r.Method == http.MethodGet {
		task.RangesMu.Lock()
		ranges, err := task.LoadRanges(file)
		task.RangesMu.Unlock()
		if err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "msg": err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{"file": file, "count": len(ranges), "ranges": ranges})
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req rangesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeJSON(w, map[string]interface{}{"ok": false, "msg": "请求格式错误"})
		return
	}
	if req.File != "" {
		file = req.File
		if !allowedRangeFile(file) {
			writeError(w, http.StatusBadRequest, "非法文件路径: "+file)
			return
		}
	}
	items := make([]string, 0)
	for _, item := range strings.Split(req.Ranges, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !validRange(item) {
			writeJSON(w, map[string]interface{}{"ok": false, "msg": "IP 段格式无效: " + item})
			return
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		writeJSON(w, map[string]interface{}{"ok": false, "msg": "ranges 不能为空"})
		return
	}

	task.RangesMu.Lock()
	defer task.RangesMu.Unlock()
	ranges, err := task.LoadRanges(file)
	if err != nil {
		writeJSON(w, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	if r.Method == http.MethodPost {
		added, err := task.AppendRanges(file, items)
		if err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "msg": err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "file": file, "added": added})
		return
	}

	remove := make(map[string]bool, len(items))
	for _, item := range items {
		remove[item] = true
	}
	kept := make([]string, 0, len(ranges))
	deleted := 0
	for _, v := range ranges {
		if remove[v] {
			deleted++
		} else {
			kept = append(kept, v)
		}
	}
	if deleted > 0 {
		if err := task.SaveRanges(file, kept); err != nil {
			writeJSON(w, map[string]interface{}{"ok": false, "msg": err.Error()})
			return
		}
	}
	writeJSON(w, map[string]interface{}{"ok": true, "file": file, "deleted": deleted})
}
