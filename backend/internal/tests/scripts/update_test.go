package scripts_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// update.sh 是升级和回退的唯一入口，出错时直接影响线上数据，所以用一个假的 docker 命令把它整条流程跑一遍：
// 假 docker 记下每次调用，按测试设定的文件决定拉镜像、备份、健康检查的结果，不需要 Docker 环境。

const fakeDocker = `#!/usr/bin/env bash
echo "$*" >> "$FAKE_DIR/calls.log"
rc() { cat "$FAKE_DIR/$1" 2>/dev/null || echo "${2:-0}"; }
case "$1" in
  info) exit 0 ;;
  compose)
    shift
    case "$1" in
      version) exit 0 ;;
      pull) exit "$(rc pull_rc)" ;;
      ps)
        case "$*" in
          *--format*) cat "$FAKE_DIR/health" ;;
          *"--status running --services"*) printf 'web\n' ;;
          *) echo ps ;;
        esac ;;
      exec)
        case "$*" in
          *pg_dump*) printf 'PGDMP-fake-dump' ;;
          *wget*) cat "$FAKE_DIR/health_json" ;;
        esac ;;
      run)
        code="$(rc agent_rc)"
        [ "$code" = 0 ] && cat "$FAKE_DIR/agent_dump"
        exit "$code" ;;
      up) exit 0 ;;
      logs) cat "$FAKE_DIR/logs_${@: -1}" 2>/dev/null || true ;;
    esac ;;
esac
exit 0
`

const sqliteDump = "SQLite format 3\x00fake-agent-data"

type env struct {
	t    *testing.T
	root string // 部署目录
	fake string // 假 docker 的状态目录
	bin  string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, root: t.TempDir(), fake: t.TempDir(), bin: t.TempDir()}
	write := func(path, content string, mode os.FileMode) {
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(e.bin, "docker"), fakeDocker, 0o755)
	write(filepath.Join(e.bin, "curl"), "#!/bin/sh\nexit 22\n", 0o755) // 取不到新版 compose 文件：沿用本地的
	envFile := "IMAGE_TAG=1.0.0\nPOSTGRES_USER=postgres\nPOSTGRES_DB=video_canvas\nAGENT_SERVICE_TOKEN=" + strings.Repeat("a", 64) + "\n"
	write(filepath.Join(e.root, ".env"), envFile, 0o644)
	write(filepath.Join(e.root, "docker-compose.yml"), "services: {}\n", 0o644)
	e.set("health", "backend healthy\nagent healthy\nweb\n")
	e.set("health_json", `{"status":"ok","version":"v1.1.0"}`)
	e.set("agent_dump", sqliteDump)
	return e
}

func (e *env) set(name, content string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.fake, name), []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// run 执行 update.sh，返回合并的输出、退出码和状态文件的内容。
func (e *env) run(args ...string) (string, int, map[string]string) {
	e.t.Helper()
	script, _ := filepath.Abs("../../../../scripts/update.sh")
	status := filepath.Join(e.root, "status.json")
	cmd := exec.Command("bash", append([]string{script, "--dir", e.root, "--status-file", status}, args...)...)
	cmd.Env = []string{"PATH=" + e.bin + ":" + os.Getenv("PATH"), "FAKE_DIR=" + e.fake, "HOME=" + e.root}
	out, err := cmd.CombinedOutput()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		e.t.Fatal(err)
	}
	st := map[string]string{}
	if b, err := os.ReadFile(status); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	return string(out), code, st
}

func (e *env) calls() string {
	b, _ := os.ReadFile(filepath.Join(e.fake, "calls.log"))
	return string(b)
}

func (e *env) backups(pattern string) []string {
	m, _ := filepath.Glob(filepath.Join(e.root, "backups", pattern))
	return m
}

func (e *env) envValue(key string) string {
	b, _ := os.ReadFile(filepath.Join(e.root, ".env"))
	for _, l := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(l, key+"="); ok {
			return v
		}
	}
	return ""
}

func TestUpdate_BacksUpAgentDataOnline(t *testing.T) {
	e := newEnv(t)
	out, code, st := e.run("--tag", "1.1.0")
	if code != 0 || st["state"] != "succeeded" {
		t.Fatalf("应成功：%d %v\n%s", code, st, out)
	}
	dump := e.backups("pre-1.1.0-*.dump")
	agent := e.backups("pre-1.1.0-*.agent.sqlite")
	if len(dump) != 1 || len(agent) != 1 {
		t.Fatalf("数据库和 Agent 数据各一份备份：%v %v\n%s", dump, agent, out)
	}
	if b, _ := os.ReadFile(agent[0]); string(b) != sqliteDump {
		t.Errorf("备份内容应原样落盘：%q", b)
	}
	if len(e.backups(".agent-backup.tmp")) != 0 {
		t.Error("不能留下临时文件")
	}
	calls := e.calls()
	// 用目标版本的镜像起一次性容器备份：服务在不在运行都行；必须在重建（up）之前
	runAt, upAt := strings.Index(calls, "compose run --rm --no-deps -T --entrypoint sh agent"), strings.Index(calls, "compose up")
	if runAt < 0 || upAt < 0 || runAt > upAt {
		t.Errorf("Agent 备份应在重建之前：\n%s", calls)
	}
	if !strings.Contains(calls, "node dist/backup-cli.js") {
		t.Errorf("应调用镜像里的备份命令：\n%s", calls)
	}
}

func TestUpdate_NoBackupFlagSkipsBothBackups(t *testing.T) {
	e := newEnv(t)
	if _, code, _ := e.run("--tag", "1.1.0", "--no-backup"); code != 0 || strings.Contains(e.calls(), "compose run") || len(e.backups("*")) != 0 {
		t.Errorf("--no-backup 什么都不备份：\n%s", e.calls())
	}
}

// 从没有独立 Agent 服务的旧版本升上来：.env 里还没有服务令牌，update.sh 要生成，否则 backend 启用 Agent 时拒绝启动。
func TestUpdate_GeneratesAgentTokenWhenMissing(t *testing.T) {
	for name, envLine := range map[string]string{"没有这一项": "", "太短": "AGENT_SERVICE_TOKEN=short\n"} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			raw, _ := os.ReadFile(filepath.Join(e.root, ".env"))
			stripped := regexp.MustCompile(`(?m)^AGENT_SERVICE_TOKEN=.*\n`).ReplaceAllString(string(raw), "")
			if err := os.WriteFile(filepath.Join(e.root, ".env"), []byte(stripped+envLine+"APP_AGENT_RUNTIME=http\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if out, code, _ := e.run("--tag", "1.1.0"); code != 0 {
				t.Fatalf("应成功：%s", out)
			}
			tok := e.envValue("AGENT_SERVICE_TOKEN")
			if len(tok) < 32 {
				t.Errorf("应生成至少 32 个字符的令牌：%q", tok)
			}
			if got := strings.Count(mustRead(t, filepath.Join(e.root, ".env")), "AGENT_SERVICE_TOKEN="); got != 1 {
				t.Errorf(".env 里只该有一行令牌：%d", got)
			}
		})
	}
}

// 已有合格的令牌就不动：backend 和 agent 两边都靠它，换掉会让正在运行的服务和新配置对不上。
func TestUpdate_KeepsExistingAgentToken(t *testing.T) {
	e := newEnv(t)
	before := e.envValue("AGENT_SERVICE_TOKEN")
	if out, code, _ := e.run("--tag", "1.1.0"); code != 0 {
		t.Fatalf("%s", out)
	}
	if got := e.envValue("AGENT_SERVICE_TOKEN"); got != before {
		t.Errorf("已有的令牌不能被改：%q → %q", before, got)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUpdate_FirstEnableHasNoAgentDataYet(t *testing.T) {
	e := newEnv(t)
	e.set("agent_rc", "3") // 卷里还没有数据库
	out, code, st := e.run("--tag", "1.1.0")
	if code != 0 || st["state"] != "succeeded" {
		t.Fatalf("第一次启用不该被备份卡住：%d %v\n%s", code, st, out)
	}
	if len(e.backups("*.agent.sqlite")) != 0 || len(e.backups("pre-1.1.0-*.dump")) != 1 {
		t.Errorf("只有数据库的备份：%v", e.backups("*"))
	}
	if !strings.Contains(out, "还没有数据") {
		t.Errorf("要说明为什么没备份：\n%s", out)
	}
}

func TestUpdate_AgentBackupFailureAbortsBeforeTouchingServices(t *testing.T) {
	for name, set := range map[string]func(e *env){
		"备份命令失败":      func(e *env) { e.set("agent_rc", "1") },
		"备份是空文件":      func(e *env) { e.set("agent_dump", "") },
		"备份不是 SQLite": func(e *env) { e.set("agent_dump", "<html>oops</html>") },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			set(e)
			out, code, st := e.run("--tag", "1.1.0")
			if code == 0 || st["state"] != "failed" {
				t.Fatalf("应中止：%d %v\n%s", code, st, out)
			}
			if strings.Contains(e.calls(), "compose up") {
				t.Errorf("备份失败不能重建服务：\n%s", e.calls())
			}
			if got := e.envValue("IMAGE_TAG"); got != "1.0.0" {
				t.Errorf("IMAGE_TAG 应退回更新前的版本：%q", got)
			}
			if len(e.backups("*.agent.sqlite")) != 0 || len(e.backups(".agent-backup.tmp")) != 0 {
				t.Errorf("不能留下坏备份：%v", e.backups("*"))
			}
			if !strings.Contains(st["message"], "备份 Agent 数据失败") {
				t.Errorf("要说清楚原因：%q", st["message"])
			}
		})
	}
}

func TestUpdate_KeepsOnlyTheNewestBackupsOfEachKind(t *testing.T) {
	e := newEnv(t)
	if err := os.MkdirAll(filepath.Join(e.root, "backups"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 4; i++ {
		for _, ext := range []string{"dump", "agent.sqlite"} {
			f := filepath.Join(e.root, "backups", fmt.Sprintf("pre-0.%d.0-20260101000000.%s", i, ext))
			if err := os.WriteFile(f, []byte("old"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if out, code, _ := e.run("--tag", "1.1.0", "--keep-backups", "3"); code != 0 {
		t.Fatalf("%s", out)
	}
	for _, pat := range []string{"*.dump", "*.agent.sqlite"} {
		got := e.backups(pat)
		if len(got) != 3 {
			t.Errorf("%s 应只留最近 3 份：%v", pat, got)
		}
		for _, g := range got {
			if strings.Contains(g, "pre-0.1.0") || strings.Contains(g, "pre-0.2.0") {
				t.Errorf("最旧的应被清掉：%s", g)
			}
		}
	}
}

// 新版本起不来、退回旧版本后 agent 因为「数据已被新版本升级」拒绝启动：不能假装回退成功，要指到 Agent 的备份。
func TestUpdate_RollbackExplainsAgentDataNewerThanOldImage(t *testing.T) {
	e := newEnv(t)
	e.set("health", "backend healthy\nagent starting\nweb\n") // 一直不就绪
	e.set("logs_agent", `{"msg":"数据库的版本 4 比当前代码（最新是 3）新：可能回退了镜像。请换回新版镜像，或从升级前的备份恢复 agent_data 卷"}`)
	out, code, st := e.run("--tag", "1.1.0", "--timeout", "1")
	if code == 0 || st["state"] != "failed_needs_attention" {
		t.Fatalf("应报需要人工处理：%d %v\n%s", code, st, out)
	}
	agent := e.backups("pre-1.1.0-*.agent.sqlite")
	if len(agent) != 1 || !strings.Contains(st["message"], agent[0]) || !strings.Contains(st["message"], "agent 拒绝启动") {
		t.Errorf("要指到 Agent 备份：%q（备份 %v）", st["message"], agent)
	}
	if got := e.envValue("IMAGE_TAG"); got != "1.0.0" {
		t.Errorf("回退后 IMAGE_TAG 应是旧版本：%q", got)
	}
}

// 回退时看到的新版本日志里要带上 agent 的（agent 起不来是最常见的原因之一）。
func TestUpdate_RollbackShowsAgentLogs(t *testing.T) {
	e := newEnv(t)
	e.set("health", "backend healthy\nagent starting\nweb\n")
	e.set("logs_agent", "AGENT-LOG-MARKER")
	out, _, _ := e.run("--tag", "1.1.0", "--timeout", "1")
	if !strings.Contains(out, "AGENT-LOG-MARKER") {
		t.Errorf("回退时应打印 agent 的日志：\n%s", out)
	}
}
