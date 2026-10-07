package agent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"sync"
)

// LaunchSpec 描述要启动的子进程。
type LaunchSpec struct {
	Name string   // 可执行文件
	Args []string // 参数
	Dir  string   // 工作目录
	Env  []string // 环境变量；只放这里写的，不继承服务进程的环境
}

// ExitInfo 是子进程退出的信息。
type ExitInfo struct {
	Code       int    // 退出码，被杀掉是 -1
	Err        error  // 等待进程时的错误
	StderrTail string // 标准错误的最后一小段，排查崩溃用
}

// Proc 是一个运行中的子进程。
type Proc interface {
	// Send 向进程的标准输入写一行。
	Send(line []byte) error
	// Kill 强制结束进程。
	Kill()
	// Done 在进程退出后收到一次退出信息。
	Done() <-chan ExitInfo
}

// Launcher 启动子进程。生产用 ExecLauncher，测试用假实现。
type Launcher interface {
	// Launch 启动进程并立即返回，不等它结束。
	Launch(ctx context.Context, spec LaunchSpec) (Proc, error)
}

// ExecLauncher 用 os/exec 启动真正的子进程。
type ExecLauncher struct{}

// stderrTail 只保留标准错误的最后 4KB。
type stderrTail struct {
	mu  sync.Mutex
	buf []byte
}

const maxStderrTail = 4 << 10

func (t *stderrTail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > maxStderrTail {
		t.buf = t.buf[len(t.buf)-maxStderrTail:]
	}
	return len(p), nil
}

func (t *stderrTail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(bytes.ToValidUTF8(t.buf, nil))
}

type execProc struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	mu    sync.Mutex
	done  chan ExitInfo
}

// Launch 启动进程：标准输出丢弃，标准错误保留最后一段，标准输入留给 Send。
func (ExecLauncher) Launch(ctx context.Context, spec LaunchSpec) (Proc, error) {
	// 进程要比发起它的 HTTP 请求活得久（一个运行片段可能跑好几分钟），所以不能随请求取消
	//nolint:gosec // G204: 可执行文件和参数来自服务端配置，不含任何用户输入
	cmd := exec.CommandContext(context.WithoutCancel(ctx), spec.Name, spec.Args...)
	cmd.Dir, cmd.Env = spec.Dir, spec.Env
	tail := &stderrTail{}
	cmd.Stderr = tail
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &execProc{cmd: cmd, stdin: stdin, done: make(chan ExitInfo, 1)}
	go func() {
		err := cmd.Wait()
		code := 0
		var ee *exec.ExitError
		switch {
		case errors.As(err, &ee):
			code = ee.ExitCode()
		case err != nil:
			code = -1
		}
		p.done <- ExitInfo{Code: code, Err: err, StderrTail: tail.String()}
	}()
	return p, nil
}

// Send 向进程的标准输入写一行（自动补换行）。
func (p *execProc) Send(line []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, err := p.stdin.Write(append(bytes.Clone(line), '\n'))
	return err
}

// Kill 强制结束进程。
func (p *execProc) Kill() { _ = p.cmd.Process.Kill() } // 进程已经退出时 Kill 会报错，不需要处理

// Done 返回退出通知通道。
func (p *execProc) Done() <-chan ExitInfo { return p.done }
