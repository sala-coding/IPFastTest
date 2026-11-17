package utils

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/cheggaaa/pb/v3"
)

type Bar struct {
	pb    *pb.ProgressBar
	count int
	cur   int64
}

// Progress 全局测速进度，线程安全
var Progress struct {
	mu    sync.RWMutex
	Stage string `json:"stage"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

// SetStage 设置当前阶段（延迟测速 / 下载测速 / 完成）
func SetStage(stage string) {
	Progress.mu.Lock()
	Progress.Stage = stage
	Progress.mu.Unlock()
}

// SetProgress 更新进度计数
func SetProgress(done, total int) {
	Progress.mu.Lock()
	Progress.Done, Progress.Total = done, total
	Progress.mu.Unlock()
}

// ProgressInfo 返回进度快照
func ProgressInfo() (stage string, done, total int) {
	Progress.mu.RLock()
	defer Progress.mu.RUnlock()
	return Progress.Stage, Progress.Done, Progress.Total
}

func NewBar(count int, MyStrStart, MyStrEnd string) *Bar {
	tmpl := fmt.Sprintf(`{{counters . }} {{ bar . "[" "-" (cycle . "↖" "↗" "↘" "↙" ) "_" "]"}} %s {{string . "MyStr" | green}} %s `, MyStrStart, MyStrEnd)
	pb := pb.ProgressBarTemplate(tmpl).Start(count)
	SetProgress(0, count)
	return &Bar{pb: pb, count: count}
}

func (b *Bar) Grow(num int, MyStrVal string) {
	b.pb.Set("MyStr", MyStrVal).Add(num)
	SetProgress(int(atomic.AddInt64(&b.cur, int64(num))), b.count)
}

func (b *Bar) Done() {
	b.pb.Finish()
	SetProgress(b.count, b.count)
}
