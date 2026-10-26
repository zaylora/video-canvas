package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 一键升级只会换成目标版本的 docker-compose.yml，而跑的 update.sh 是 updater 镜像里的旧版本，不会补 .env 里的 Agent 令牌。
// 所以令牌缺失必须在 compose 解析时就报错：这样升级停在拉取镜像之前，运行中的服务不会被重建、更不会短暂中断。

func composeConfig(t *testing.T, extraEnv ...string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("没有 docker，跳过 compose 解析检查")
	}
	if err := exec.Command("docker", "compose", "version").Run(); err != nil {
		t.Skip("没有 docker compose，跳过 compose 解析检查")
	}
	compose, err := filepath.Abs("../../../../docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("docker", "compose", "-f", compose, "config")
	cmd.Env = append(os.Environ(),
		"POSTGRES_PASSWORD=test-password",
		"APP_JWT_SECRET=test-jwt-secret",
		"APP_AI_SECRET_KEY=test-ai-secret",
		"AGENT_SERVICE_TOKEN=", // 先清空，再由 extraEnv 决定
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestCompose_MissingAgentTokenFailsAtParseTime(t *testing.T) {
	out, err := composeConfig(t)
	if err == nil {
		t.Fatalf("没有 AGENT_SERVICE_TOKEN 时 compose 应该解析失败，实际通过了：\n%s", out)
	}
	if !strings.Contains(out, "AGENT_SERVICE_TOKEN") {
		t.Fatalf("报错应该点名 AGENT_SERVICE_TOKEN，实际：\n%s", out)
	}
}

func TestCompose_WithAgentTokenParses(t *testing.T) {
	token := strings.Repeat("a", 64)
	out, err := composeConfig(t, "AGENT_SERVICE_TOKEN="+token)
	if err != nil {
		t.Fatalf("有令牌时 compose 应该能解析：%v\n%s", err, out)
	}
	if got := strings.Count(out, token); got != 2 {
		t.Fatalf("令牌应该同时传给 backend 和 agent 两个服务，实际出现 %d 次", got)
	}
}
