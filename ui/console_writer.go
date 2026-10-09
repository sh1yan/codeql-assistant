package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"codeql-assistant/engine"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// 控制台输出的两个关键参数：
//
//	uiUpdateInterval 是重绘节流间隔。codeql database create 在大型项目上每秒可输出成千上万行，
//	如果每一行都单独刷新一次 Label，Fyne 主线程会被 SetText + 布局 + ScrollToBottom 淹没
//	（ScrollToBottom 内部会计算 Content.MinSize()，对整段文本重新测量，单次开销随文本长度线性增长），
//	而 fyne.Do 的函数队列是无界的，于是队列越积越多：进程其实早已退出，
//	但“运行中”的状态更新排在几万条日志刷新后面，表现为程序一直卡在运行中，
//	直到手动清空输出让每次重绘变便宜，队列才被排空。这里改为按帧合并刷新。
//
//	consoleDisplayLines 是控制台最多渲染的行数。Label 文本越长每次布局越慢，
//	因此只保留最近的一段用于显示；完整日志另有文件记录，不会丢失。
const (
	uiUpdateInterval    = 120 * time.Millisecond
	consoleDisplayLines = 3000
	// maxLogFileSize 限制单个日志文件的体积，超出后停止写入，避免磁盘被写满。
	maxLogFileSize = 64 << 20
	// logRetention 之后的历史日志会在新建日志时被清理。
	logRetention = 3 * 24 * time.Hour
	// droppedNoticeLines 是“已省略前面 N 行”提示行占用的窗口位置。
	droppedNoticeLines = 1
)

// consoleRing 是一个只保留最近 N 行的滚动窗口，用于控制 Label 的渲染规模。
type consoleRing struct {
	max     int
	lines   []string
	dropped int
}

func newConsoleRing(max int) *consoleRing {
	return &consoleRing{max: max}
}

// add 追加一行，必要时淘汰最旧的行。
// 淘汰判断用有效上限（预留一行给省略提示），这样窗口稳定后总行数不会超过 r.max。
func (r *consoleRing) add(line string) {
	r.lines = append(r.lines, line)
	if len(r.lines) > r.limit() {
		r.trim()
	}
}

// limit 是除省略提示之外可容纳的行数。
func (r *consoleRing) limit() int {
	limit := r.max - droppedNoticeLines
	if limit < 1 {
		limit = 1
	}
	return limit
}

// trim 把窗口收缩到容量上限，并累计被淘汰的行数。
func (r *consoleRing) trim() {
	limit := r.limit()
	if len(r.lines) <= limit {
		return
	}
	drop := len(r.lines) - limit
	// 整体重建切片，避免反复 copy 造成的抖动。
	kept := make([]string, limit)
	copy(kept, r.lines[drop:])
	r.lines = kept
	r.dropped += drop
}

// content 把窗口内容拼成一段文本。
func (r *consoleRing) content() string {
	if len(r.lines) == 0 {
		return ""
	}
	if r.dropped > 0 {
		return fmt.Sprintf("…… 已省略前面 %d 行（完整日志请点「打开日志」）……\n%s",
			r.dropped, strings.Join(r.lines, "\n"))
	}
	return strings.Join(r.lines, "\n")
}

// reset 清空窗口，用于新一轮运行。
func (r *consoleRing) reset() {
	r.lines = nil
	r.dropped = 0
}

// runTimer 只暴露 consoleWriter 需要的能力，便于在测试中观察延迟刷新是否被取消。
type runTimer interface {
	Stop() bool
}

// consoleWriter 把 CodeQL 的海量输出合并成低频的界面刷新，同时把完整输出写入日志文件。
// append 可以被任意 goroutine 调用，且不会阻塞调用方。
type consoleWriter struct {
	label     *widget.Label
	scroll    *container.Scroll
	onLogPath func(string)

	ring        *consoleRing
	mu          sync.Mutex
	pending     []string
	updateInFly bool
	log         *os.File
	logPath     string
	timer       runTimer

	// schedule 把一次刷新投递到 Fyne 主线程，默认使用 fyne.Do；
	// after 用于排定延迟刷新，默认使用 time.AfterFunc。
	// 抽成字段是为了让单元测试可以精确观察刷新合并的行为。
	schedule func(func())
	after    func(time.Duration, func()) runTimer
}

func newConsoleWriter(label *widget.Label, scroll *container.Scroll) *consoleWriter {
	return &consoleWriter{
		label:    label,
		scroll:   scroll,
		ring:     newConsoleRing(consoleDisplayLines),
		schedule: fyne.Do,
		after: func(d time.Duration, fn func()) runTimer {
			return time.AfterFunc(d, fn)
		},
	}
}

func (c *consoleWriter) post(fn func()) {
	if c.schedule != nil {
		c.schedule(fn)
		return
	}
	fyne.Do(fn)
}

func (c *consoleWriter) afterFunc(d time.Duration, fn func()) runTimer {
	if c.after != nil {
		return c.after(d, fn)
	}
	return time.AfterFunc(d, fn)
}

// start 开始新一轮输出：清空界面内容，并新建一个日志文件保存完整输出。
func (c *consoleWriter) start() {
	c.clear()

	path, f, err := createRunLog()
	if err != nil {
		// 日志文件不可用时不影响控制台输出本身。
		path = ""
		f = nil
	}

	c.mu.Lock()
	c.log = f
	c.logPath = path
	c.mu.Unlock()

	if c.onLogPath != nil {
		c.onLogPath(path)
	}
}

// append 接收一行输出。日志文件立即写入，界面刷新被合并到稍后的帧。
func (c *consoleWriter) append(line string) {
	c.mu.Lock()
	c.pending = append(c.pending, line)
	needSchedule := !c.updateInFly
	c.updateInFly = true
	c.mu.Unlock()

	// 先把日志落盘，保证即使界面刷新被节流，完整输出也不会丢失。
	c.writeLog(line)

	if needSchedule {
		// 注意：这里必须立刻投递，不能只依赖定时器。
		// 输出停止后不会再有新的行触发调度，若全靠延迟，
		// 最后一批日志和“已完成”的收尾输出就会一直留在缓冲区里。
		c.post(c.flush)
	}
}

// flush 把缓冲区里的日志合并成一次界面刷新（必须在 Fyne 主线程执行）。
func (c *consoleWriter) flush() {
	// 本次刷新已经处理了缓冲区，取消上一次排定的延迟刷新。
	c.stopTimer()

	c.mu.Lock()
	lines := c.pending
	c.pending = nil
	if len(lines) == 0 {
		c.updateInFly = false
		c.mu.Unlock()
		return
	}
	for _, l := range lines {
		c.ring.add(l)
	}
	content := c.ring.content()
	c.mu.Unlock()

	c.render(content)

	// 输出仍在持续时，隔一个时间片再合并刷新一次。
	// append 只会在“当前没有在途刷新”时才重新投递，因此输出停止后这里不会无限空转。
	var timer runTimer
	timer = c.afterFunc(uiUpdateInterval, func() {
		c.mu.Lock()
		if c.timer == timer {
			c.timer = nil
		}
		c.mu.Unlock()
		c.post(c.flush)
	})

	c.mu.Lock()
	c.timer = timer
	c.mu.Unlock()
}

func (c *consoleWriter) stopTimer() {
	c.mu.Lock()
	t := c.timer
	c.timer = nil
	// 没有在途刷新了，下一次 append 需要重新投递。
	c.updateInFly = false
	c.mu.Unlock()

	if t != nil {
		t.Stop()
	}
}

// finish 结束本轮输出：把剩余日志补写出去并关闭日志文件。可重复调用。
func (c *consoleWriter) finish() {
	c.flush()
	c.stopTimer()

	c.mu.Lock()
	f := c.log
	c.log = nil
	c.mu.Unlock()

	if f != nil {
		f.Close()
	}
}

// clear 立即清空控制台内容。
func (c *consoleWriter) clear() {
	c.stopTimer()

	c.mu.Lock()
	c.pending = nil
	c.ring.reset()
	// 必须复位在途标记：如果当前缓冲区恰好是空的，flush 会直接返回而不复位，
	// 之后 append 就会认为“已有刷新在途”而不再调度，界面将永久停止更新。
	c.updateInFly = false
	c.mu.Unlock()

	c.label.SetText("")
	c.label.Refresh()
	if c.scroll != nil {
		c.scroll.Refresh()
	}
}

// content 返回当前控制台显示的文本。
func (c *consoleWriter) content() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ring.content()
}

// render 在 Fyne 主线程上更新 Label。
func (c *consoleWriter) render(content string) {
	if c.label.Text == content {
		return
	}
	c.label.SetText(content)
	if c.scroll != nil {
		c.scroll.ScrollToBottom()
	}
}

func (c *consoleWriter) writeLog(line string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.log == nil {
		return
	}
	if _, err := c.log.WriteString(line + "\n"); err != nil {
		return
	}
	if info, err := c.log.Stat(); err == nil && info.Size() > maxLogFileSize {
		_, _ = c.log.WriteString("…… 日志文件已达体积上限，后续输出不再写入 ……\n")
		c.log.Close()
		c.log = nil
	}
}

// openLog 用系统默认程序打开最新的日志文件，方便查看被控制台省略的输出。
func (c *consoleWriter) openLog() error {
	c.mu.Lock()
	path := c.logPath
	c.mu.Unlock()

	if path == "" {
		return fmt.Errorf("本次运行还没有生成日志文件")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("日志文件不存在: %s", path)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// createRunLog 在应用数据目录下新建一个日志文件，并顺带清理过期日志。
func createRunLog() (string, *os.File, error) {
	dir := filepath.Join(engine.AppDataDir(), "logs")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", nil, err
	}

	cleanOldLogs(dir)

	path := filepath.Join(dir, time.Now().Format("20060102-150405")+".log")
	f, err := os.Create(path)
	if err != nil {
		return "", nil, err
	}
	return path, f, nil
}

func cleanOldLogs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-logRetention)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".log" {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		os.Remove(filepath.Join(dir, e.Name()))
	}
}
