package updater_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/updater"
)

const token = "test-token"

// fakeRun 模拟 update.sh：写进度文件，可以被测试按住不让结束。
type fakeRun struct {
	mu      sync.Mutex
	targets []string
	release chan struct{}
	state   string
}

// block 让之后的升级停在执行中，直到 unblock。
func (f *fakeRun) block() {
	f.mu.Lock()
	f.release = make(chan struct{})
	f.mu.Unlock()
}

// unblock 放行被 block 住的升级；可重复调用。
func (f *fakeRun) unblock() {
	f.mu.Lock()
	if f.release != nil {
		close(f.release)
		f.release = nil
	}
	f.mu.Unlock()
}

func (f *fakeRun) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.targets)
}

func (f *fakeRun) run(dir string) func(ctx context.Context, target string, log io.Writer) error {
	return func(ctx context.Context, target string, log io.Writer) error {
		f.mu.Lock()
		f.targets = append(f.targets, target)
		release := f.release
		f.mu.Unlock()
		fmt.Fprintf(log, "==> 升级到 %s\n", target)
		if release != nil {
			<-release
		}
		st := f.state
		if st == "" {
			st = model.UpgradeStateSucceeded
		}
		body := fmt.Sprintf(`{"target":%q,"previous":"0.3.2","phase":"finish","state":%q,"message":%q,"backup":%q,"updated_at":"x"}`, target, st, "否则用备份 "+filepath.ToSlash(filepath.Join(dir, "backups", "x.dump"))+" 恢复", filepath.Join(dir, "backups", "pre-"+target+".dump"))
		if err := os.WriteFile(filepath.Join(dir, "backups", "upgrade-status.json"), []byte(body), 0o600); err != nil {
			return err
		}
		if st != model.UpgradeStateSucceeded {
			return errors.New("exit status 1")
		}
		return nil
	}
}

type env struct {
	dir    string
	server *updater.Server
	client *updater.Client
	fake   *fakeRun
	addr   string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "backups"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("POSTGRES_PASSWORD=x\nIMAGE_TAG=0.3.2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeRun{}
	srv, err := updater.NewServer(updater.ServerOptions{Token: token, DeployDir: dir, Run: fake.run(dir)})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: time.Second}
	go func() { _ = hs.Serve(ln) }()
	t.Cleanup(func() {
		fake.unblock()
		srv.Wait()
		_ = hs.Close()
	})
	return &env{dir: dir, server: srv, client: updater.NewClient("tcp", ln.Addr().String(), token), fake: fake, addr: ln.Addr().String()}
}

func waitIdle(t *testing.T, c *updater.Client) *model.UpdaterStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, err := c.Status(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !st.Running {
			return st
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("升级一直没有结束")
	return nil
}

func TestNewServerRequiresToken(t *testing.T) {
	if _, err := updater.NewServer(updater.ServerOptions{DeployDir: t.TempDir()}); err == nil {
		t.Fatal("没有 token 时应拒绝启动")
	}
}

func TestStatusBeforeAnyRun(t *testing.T) {
	e := newEnv(t)
	if err := os.WriteFile(filepath.Join(e.dir, "backups", "pre-0.3.2-20261014.dump"), []byte("PGDMP"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dir, "backups", "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := e.client.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Running || st.ImageTag != "0.3.2" || st.Last != nil {
		t.Fatalf("%+v", st)
	}
	if len(st.Backups) != 1 || st.Backups[0].Name != "pre-0.3.2-20261014.dump" || st.Backups[0].Size != 5 {
		t.Fatalf("只列出 .dump 备份：%+v", st.Backups)
	}
}

func TestUpgradeRunsScriptAndReportsStatus(t *testing.T) {
	e := newEnv(t)
	if err := e.client.Upgrade(context.Background(), "0.4.0"); err != nil {
		t.Fatal(err)
	}
	st := waitIdle(t, e.client)
	if st.Last == nil || st.Last.Target != "0.4.0" || st.Last.State != model.UpgradeStateSucceeded || st.Last.Previous != "0.3.2" {
		t.Fatalf("%+v", st.Last)
	}
	if st.Last.Backup != "backups/pre-0.4.0.dump" {
		t.Fatalf("备份路径应换成相对部署目录的路径（容器里的 /deploy 对管理员没有意义）：%q", st.Last.Backup)
	}
	if st.Last.Message != "否则用备份 backups/x.dump 恢复" {
		t.Fatalf("说明里的容器路径也应去掉：%q", st.Last.Message)
	}
	if !strings.Contains(st.LogTail, "升级到 0.4.0") {
		t.Fatalf("应带上运行输出：%q", st.LogTail)
	}
}

func TestUpgradeRejectsConcurrentRun(t *testing.T) {
	e := newEnv(t)
	e.fake.block()
	if err := e.client.Upgrade(context.Background(), "0.4.0"); err != nil {
		t.Fatal(err)
	}
	st, _ := e.client.Status(context.Background())
	if !st.Running {
		t.Fatal("运行中应报告 running")
	}
	err := e.client.Upgrade(context.Background(), "0.4.1")
	if !errors.Is(err, errcode.ErrUpgradeRunning) {
		t.Fatalf("运行中再次升级应返回 ErrUpgradeRunning，得到 %v", err)
	}
	e.fake.unblock()
	waitIdle(t, e.client)
	if e.fake.count() != 1 {
		t.Fatalf("只应执行一次：%v", e.fake.targets)
	}
}

func TestUpgradeFailureKeepsStatus(t *testing.T) {
	e := newEnv(t)
	e.fake.state = model.UpgradeStateFailedRolledBack
	if err := e.client.Upgrade(context.Background(), "0.4.0"); err != nil {
		t.Fatal(err)
	}
	st := waitIdle(t, e.client)
	if st.Last == nil || st.Last.State != model.UpgradeStateFailedRolledBack {
		t.Fatalf("%+v", st.Last)
	}
}

func TestUpgradeValidatesTarget(t *testing.T) {
	e := newEnv(t)
	for _, target := range []string{"", "latest", "0.4", "v0.4.0", "0.4.0; rm -rf /", "../0.4.0"} {
		if err := e.client.Upgrade(context.Background(), target); err == nil {
			t.Fatalf("目标版本 %q 应被拒绝", target)
		}
	}
	if e.fake.count() != 0 {
		t.Fatal("不合法的版本不应执行脚本")
	}
}

func TestRequiresToken(t *testing.T) {
	e := newEnv(t)
	bad := updater.NewClient("tcp", e.addr, "wrong")
	if _, err := bad.Status(context.Background()); err == nil {
		t.Fatal("token 错误应被拒绝")
	}
	if err := bad.Upgrade(context.Background(), "0.4.0"); err == nil || e.fake.count() != 0 {
		t.Fatal("token 错误不应执行升级")
	}
}

func TestClientUnreachable(t *testing.T) {
	c := updater.NewClient("tcp", "127.0.0.1:1", token)
	if _, err := c.Status(context.Background()); err == nil {
		t.Fatal("连不上应报错")
	}
}
