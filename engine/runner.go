package engine

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// RunStatus 表示运行器当前的状态。
type RunStatus int

const (
	StatusIdle RunStatus = iota
	StatusRunning
	StatusCompleted
	StatusFailed
	StatusStopped
)

func (s RunStatus) String() string {
	switch s {
	case StatusIdle:
		return "就绪"
	case StatusRunning:
		return "运行中..."
	case StatusCompleted:
		return "已完成"
	case StatusFailed:
		return "失败"
	case StatusStopped:
		return "已停止"
	default:
		return "未知"
	}
}

// OutputCallback 为 CodeQL 进程输出的每一行调用。
type OutputCallback func(line string, isError bool)

// Runner 管理 CodeQL 进程的整个生命周期。
type Runner struct {
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	cmd      *exec.Cmd
	status   RunStatus
	statusCb func(RunStatus)
}

// NewRunner 创建一个新的 Runner 实例。
func NewRunner() *Runner {
	return &Runner{
		status: StatusIdle,
	}
}

// SetStatusCallback 设置状态变更的回调函数。
func (r *Runner) SetStatusCallback(cb func(RunStatus)) {
	r.statusCb = cb
}

func (r *Runner) setStatus(s RunStatus) {
	r.status = s
	if r.statusCb != nil {
		r.statusCb(s)
	}
}

// readOutput 带 context 感知的读取，可被取消。
func readOutput(ctx context.Context, reader io.Reader, callback func(string)) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for scanner.Scan() {
			callback(scanner.Text())
		}
	}()

	select {
	case <-done:
		// 正常结束
	case <-ctx.Done():
		// context 被取消，scanner 可能还在阻塞，但我们不等了
	}
}

// RunAnalyze 使用给定配置启动 codeql database analyze 命令。
func (r *Runner) RunAnalyze(config CodeQLConfig, outputCb OutputCallback) error {
	r.mu.Lock()
	if r.status == StatusRunning {
		r.mu.Unlock()
		return fmt.Errorf("已有运行正在进行中。")
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.ctx = ctx
	r.cancel = cancel

	args := r.buildAnalyzeArgs(config)
	r.cmd = exec.CommandContext(ctx, config.BinaryPath, args...)

	// 设置进程组，确保 Kill 能终止所有子进程
	r.cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	stdout, err := r.cmd.StdoutPipe()
	if err != nil {
		r.setStatus(StatusFailed)
		r.mu.Unlock()
		return fmt.Errorf("创建标准输出管道失败。: %w", err)
	}

	stderr, err := r.cmd.StderrPipe()
	if err != nil {
		r.setStatus(StatusFailed)
		r.mu.Unlock()
		return fmt.Errorf("创建标准错误管道失败。: %w", err)
	}

	if err := r.cmd.Start(); err != nil {
		r.setStatus(StatusFailed)
		r.mu.Unlock()
		outputCb(fmt.Sprintf("启动失败。: %v", err), true)
		return fmt.Errorf("启动 CodeQL 失败。: %w", err)
	}

	r.setStatus(StatusRunning)
	r.mu.Unlock()

	outputCb(fmt.Sprintf("$ %s %s", config.BinaryPath, strings.Join(args, " ")), false)
	outputCb("---", false)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		readOutput(ctx, stdout, func(line string) {
			outputCb(line, false)
		})
	}()

	go func() {
		defer wg.Done()
		readOutput(ctx, stderr, func(line string) {
			outputCb(line, true)
		})
	}()

	// 等待 scanner goroutine 结束
	wg.Wait()

	// 等待进程退出（带超时，防止死等）
	done := make(chan error, 1)
	go func() {
		done <- r.cmd.Wait()
	}()

	r.mu.Lock()
	// var err error
	select {
	case err = <-done:
		// 正常退出
	case <-time.After(5 * time.Second):
		// 超时，强制 kill
		if r.cmd.Process != nil {
			syscall.Kill(-r.cmd.Process.Pid, syscall.SIGKILL)
		}
		err = <-done
	}

	if err != nil {
		if ctx.Err() != nil {
			r.setStatus(StatusStopped)
		} else {
			r.setStatus(StatusFailed)
		}
	} else {
		r.setStatus(StatusCompleted)
	}
	r.mu.Unlock()

	if err != nil {
		if ctx.Err() != nil {
			outputCb("---", false)
			outputCb("分析已被用户中止。", false)
		} else {
			outputCb("---", false)
			if exitErr, ok := err.(*exec.ExitError); ok {
				outputCb(fmt.Sprintf("进程退出 %d", exitErr.ExitCode()), true)
			}
			outputCb(fmt.Sprintf("分析失败。: %v", err), true)
		}
		return err
	}

	outputCb("---", false)
	outputCb("分析成功完成。", false)
	return nil
}

// RunDatabaseCreate 运行 codeql database create 并实时输出。
func (r *Runner) RunDatabaseCreate(config CodeQLConfig, outputCb OutputCallback) error {
	r.mu.Lock()
	if r.status == StatusRunning {
		r.mu.Unlock()
		return fmt.Errorf("当前已有运行正在进行，请先停止现有任务后再启动新的分析。")
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.ctx = ctx
	r.cancel = cancel

	args := r.buildDatabaseCreateArgs(config)
	r.cmd = exec.CommandContext(ctx, config.BinaryPath, args...)

	// 设置进程组
	r.cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	stdout, err := r.cmd.StdoutPipe()
	if err != nil {
		r.setStatus(StatusFailed)
		r.mu.Unlock()
		return err
	}

	stderr, err := r.cmd.StderrPipe()
	if err != nil {
		r.setStatus(StatusFailed)
		r.mu.Unlock()
		return err
	}

	if err := r.cmd.Start(); err != nil {
		r.setStatus(StatusFailed)
		r.mu.Unlock()
		outputCb(fmt.Sprintf("启动失败。: %v", err), true)
		return err
	}

	r.setStatus(StatusRunning)
	r.mu.Unlock()

	outputCb(fmt.Sprintf("$ %s %s", config.BinaryPath, strings.Join(args, " ")), false)
	outputCb("---", false)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		readOutput(ctx, stdout, func(line string) {
			outputCb(line, false)
		})
	}()

	go func() {
		defer wg.Done()
		readOutput(ctx, stderr, func(line string) {
			outputCb(line, true)
		})
	}()

	wg.Wait()

	done := make(chan error, 1)
	go func() {
		done <- r.cmd.Wait()
	}()

	r.mu.Lock()
	// var err error
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		if r.cmd.Process != nil {
			syscall.Kill(-r.cmd.Process.Pid, syscall.SIGKILL)
		}
		err = <-done
	}

	if err != nil {
		if ctx.Err() != nil {
			r.setStatus(StatusStopped)
		} else {
			r.setStatus(StatusFailed)
		}
	} else {
		r.setStatus(StatusCompleted)
	}
	r.mu.Unlock()

	if err != nil {
		if ctx.Err() != nil {
			outputCb("---", false)
			outputCb("数据库创建已被用户中止。", false)
		} else {
			outputCb("---", false)
			outputCb(fmt.Sprintf("数据库创建失败。: %v", err), true)
		}
		return err
	}

	outputCb("---", false)
	outputCb("数据库创建成功。", false)
	return nil
}

// Stop 取消正在运行的 CodeQL 进程，必要时强制终止。
func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cancel != nil {
		r.cancel()
	}
	if r.cmd != nil && r.cmd.Process != nil {
		// 先尝试优雅终止进程组
		syscall.Kill(-r.cmd.Process.Pid, syscall.SIGINT)
		// 给 500ms 优雅退出时间
		go func(pid int) {
			time.Sleep(500 * time.Millisecond)
			syscall.Kill(-pid, syscall.SIGKILL)
		}(r.cmd.Process.Pid)
	}
}

// Status 返回当前运行器状态。
func (r *Runner) Status() RunStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

// IsRunning 如果运行正在进行中，则返回 true。
func (r *Runner) IsRunning() bool {
	return r.Status() == StatusRunning
}

func (r *Runner) buildAnalyzeArgs(config CodeQLConfig) []string {
	args := []string{"database", "analyze"}
	if config.DatabasePath != "" {
		args = append(args, config.DatabasePath)
	}
	if config.QueryPath != "" {
		args = append(args, config.QueryPath)
	}
	if config.OutputFormat != "" {
		args = append(args, "--format="+config.OutputFormat)
	}
	if config.OutputFile != "" {
		args = append(args, "--output="+config.OutputFile)
	}
	if config.Threads > 0 {
		args = append(args, fmt.Sprintf("--threads=%d", config.Threads))
	}
	if config.RamLimit > 0 {
		args = append(args, fmt.Sprintf("--ram=%d", config.RamLimit))
	}
	if config.ExtraArgs != "" {
		for _, arg := range strings.Fields(config.ExtraArgs) {
			args = append(args, arg)
		}
	}
	return args
}

// splitArgs 将参数字符串按空格分割，但双引号内的空格不会分割。 2026.7.15
func (r *Runner) splitArgs(s string) []string {
	var args []string
	var current strings.Builder
	inQuote := false

	for _, ch := range s {
		switch ch {
		case '"':
			inQuote = !inQuote
		case ' ':
			if inQuote {
				current.WriteRune(ch)
			} else {
				if current.Len() > 0 {
					args = append(args, current.String())
					current.Reset()
				}
			}
		default:
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

func (r *Runner) buildDatabaseCreateArgs(config CodeQLConfig) []string {
	args := []string{"database", "create"}
	if config.DatabasePath != "" {
		args = append(args, config.DatabasePath)
	}
	args = append(args, "--language="+config.Language)
	if config.SourceRoot != "" {
		args = append(args, "--source-root="+config.SourceRoot)
	}
	if config.Threads > 0 {
		args = append(args, fmt.Sprintf("--threads=%d", config.Threads))
	}
	if config.RamLimit > 0 {
		args = append(args, fmt.Sprintf("--ram=%d", config.RamLimit))
	}
	args = append(args, "--overwrite")

	if config.ExtraArgs != "" {
		extra := r.splitArgs(config.ExtraArgs)
		args = append(args, extra...)
	}

	return args
}
