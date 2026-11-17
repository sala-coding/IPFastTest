package task

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// RangesMu 保护 IP 段文件的并发读写
var RangesMu sync.Mutex

// IPSource IP 段数据源：URL 拉取后追加到 File，新增源只需往 IPSources 追加一项
type IPSource struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	File string `json:"file"`
}

// IPSources 所有数据源，新增源在此追加即可
var IPSources = []IPSource{
	{Name: "Cloudflare IPv4", URL: "https://www.cloudflare.com/ips-v4", File: "ip.txt"},
	{Name: "Cloudflare IPv6", URL: "https://www.cloudflare.com/ips-v6", File: "ipv6.txt"},
}

// FetchIPRanges 拉取所有数据源并追加到本地文件（去重），失败时保留原文件
func FetchIPRanges() {
	for _, src := range IPSources {
		fetch(src.URL, src.File)
	}
}

// LoadRanges 读取 IP 段文件的所有行（去空、去重）
func LoadRanges(file string) ([]string, error) {
	f, err := os.Open(file)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ranges := make([]string, 0)
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !seen[line] {
			seen[line] = true
			ranges = append(ranges, line)
		}
	}
	return ranges, scanner.Err()
}

// SaveRanges 将 IP 段写入文件（覆盖）
func SaveRanges(file string, ranges []string) error {
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, line := range ranges {
		if _, err := w.WriteString(line + "\n"); err != nil {
			return err
		}
	}
	return w.Flush()
}

// AppendRanges 追加 IP 段到文件（去重），返回新增数量
func AppendRanges(file string, lines []string) (int, error) {
	ranges, err := LoadRanges(file)
	if err != nil {
		return 0, err
	}
	seen := make(map[string]bool, len(ranges))
	for _, r := range ranges {
		seen[r] = true
	}
	added := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !seen[line] {
			seen[line] = true
			ranges = append(ranges, line)
			added++
		}
	}
	if added > 0 {
		if err := SaveRanges(file, ranges); err != nil {
			return 0, err
		}
	}
	return added, nil
}

func fetch(url, file string) {
	client := http.Client{Timeout: 15 * time.Second}
	res, err := client.Get(url)
	if err != nil {
		fmt.Printf("[IP 源拉取失败] %s: %v\n", file, err)
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		fmt.Printf("[IP 源拉取失败] %s: HTTP %d\n", file, res.StatusCode)
		return
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Printf("[IP 源拉取失败] %s: %v\n", file, err)
		return
	}
	text := strings.TrimSpace(string(body))
	if text == "" || !strings.Contains(text, "/") {
		fmt.Printf("[IP 源拉取失败] %s: 数据异常\n", file)
		return
	}
	RangesMu.Lock()
	added, err := AppendRanges(file, strings.Split(text, "\n"))
	RangesMu.Unlock()
	if err != nil {
		fmt.Printf("[IP 源拉取失败] %s: %v\n", file, err)
		return
	}
	fmt.Printf("[IP 源已更新] %s (新增 %d 段)\n", file, added)
}
