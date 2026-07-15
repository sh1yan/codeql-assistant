package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// HistoryEntry 表示历史记录中的一次 CodeQL 运行。
type HistoryEntry struct {
	Timestamp  time.Time    `json:"timestamp"`
	Config     CodeQLConfig `json:"config"`
	Status     string       `json:"status"`
	OutputFile string       `json:"output_file"`
	DurationMs int64        `json:"duration_ms"`
}

// History 存储一组运行历史记录条目。mu 用于保护并发访问。
type History struct {
	mu       sync.Mutex
	Entries  []HistoryEntry `json:"entries"`
	filePath string
}

// NewHistory 从默认位置创建或加载历史记录。
func NewHistory() *History {
	h := &History{
		Entries:  []HistoryEntry{},
		filePath: filepath.Join(AppDataDir(), "history.json"),
	}
	h.load()
	return h
}

// AddEntry 在历史记录开头添加一条新条目。线程安全。
func (h *History) AddEntry(config CodeQLConfig, status string, duration time.Duration, outputFile string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	entry := HistoryEntry{
		Timestamp:  time.Now(),
		Config:     config,
		Status:     status,
		DurationMs: duration.Milliseconds(),
		OutputFile: outputFile,
	}
	h.Entries = append([]HistoryEntry{entry}, h.Entries...)
	if len(h.Entries) > 100 {
		h.Entries = h.Entries[:100]
	}
	h.save()
}

// Clear 清除所有历史记录条目。
func (h *History) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Entries = nil
	h.save()
}

func (h *History) save() {
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(h.filePath), 0755)
	os.WriteFile(h.filePath, data, 0644)
}

func (h *History) load() {
	data, err := os.ReadFile(h.filePath)
	if err != nil {
		return
	}
	json.Unmarshal(data, h)
}

// AppDataDir 返回应用程序的基础数据目录。
func AppDataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codeql-assistant")
}
