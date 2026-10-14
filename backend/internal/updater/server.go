// Package updater 是一键升级的执行端（跑在挂载了 docker.sock 的 updater 容器里）和 backend 调用它的客户端。
//
// updater 只做一件事：收到升级请求后在后台执行 update.sh，并把 update.sh 写的进度文件、
// 部署目录 .env 里的 IMAGE_TAG、backups/ 下的备份列表报告给 backend。
// 升级步骤（备份、拉镜像、重建、等就绪、失败回退）全部在 update.sh 里，这里不重复实现。
// 能不能升级到某个版本由 backend 判断；updater 只校验版本号格式，防止拼出意外的命令参数。
package updater

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"video-canvas/internal/model"
)

const (
	statusFileName = "upgrade-status.json"
	logFileName    = "upgrade.log"
	// logTailLines 是 GET /status 带回的运行输出行数，失败时给管理员排查用。
	logTailLines = 100
	// maxBodyBytes 限制请求体：只有一个版本号。
	maxBodyBytes = 1 << 10
)

// targetPattern 是允许的目标版本：x.y.z，可带 -后缀；不允许 latest，用 latest 无法确认版本也无法回退。
var targetPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)

// RunFunc 执行一次升级（真实实现是运行 update.sh），输出写进 log。返回错误表示脚本以非 0 退出，结果以进度文件为准。
type RunFunc func(ctx context.Context, target string, log io.Writer) error

// ServerOptions 是 updater 服务的配置。
type ServerOptions struct {
	Token     string  // backend 调用时必须带的共享密钥；为空拒绝启动
	DeployDir string  // 部署目录（容器里挂载在 /deploy）：含 docker-compose.yml、.env、backups/
	Script    string  // update.sh 路径；Run 为空时用它
	Run       RunFunc // 测试注入；为空时运行 Script
}

// Server 是 updater 的 HTTP 服务，同一时间只执行一次升级。
type Server struct {
	opts    ServerOptions
	mu      sync.Mutex
	running bool
	wg      sync.WaitGroup
}

// NewServer 创建 updater 服务；token 为空时报错，部署目录必须存在。
func NewServer(opts ServerOptions) (*Server, error) {
	if strings.TrimSpace(opts.Token) == "" {
		return nil, errors.New("没有配置 UPDATER_TOKEN，updater 拒绝启动：在部署目录的 .env 里加一行 UPDATER_TOKEN=<随机字符串>")
	}
	if st, err := os.Stat(opts.DeployDir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("部署目录 %s 不存在：启动 updater 时要把部署目录挂载进来", opts.DeployDir)
	}
	if opts.Run == nil {
		if opts.Script == "" {
			return nil, errors.New("没有指定 update.sh 路径")
		}
		opts.Run = scriptRunner(opts.Script, opts.DeployDir)
	}
	if err := os.MkdirAll(filepath.Join(opts.DeployDir, "backups"), 0o750); err != nil {
		return nil, fmt.Errorf("创建 backups 目录失败: %w", err)
	}
	return &Server{opts: opts}, nil
}

// scriptRunner 用 bash 执行 update.sh：备份、进度文件、运行日志都放在部署目录的 backups/ 下。
func scriptRunner(script, deployDir string) RunFunc {
	return func(ctx context.Context, target string, log io.Writer) error {
		cmd := exec.CommandContext(ctx, "bash", script, // #nosec G204 -- target 已按 targetPattern 校验，脚本路径来自配置
			"--dir", deployDir, "--tag", target,
			"--status-file", filepath.Join(deployDir, "backups", statusFileName),
			// 回退到没有 updater 服务的旧版本时，旧 compose 文件里没有 updater，不能把正在执行脚本的自己当孤儿删掉
			"--keep-orphans")
		cmd.Stdout, cmd.Stderr = log, log
		cmd.Dir = deployDir
		return cmd.Run()
	}
}

// Handler 返回 updater 的路由：GET /status、POST /upgrade，都要求 Authorization: Bearer <token>。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", s.handleStatus)
	mux.HandleFunc("POST /upgrade", s.handleUpgrade)
	return s.auth(mux)
}

// Wait 等正在执行的升级结束（退出和测试用）。
func (s *Server) Wait() { s.wg.Wait() }

func (s *Server) auth(next http.Handler) http.Handler {
	want := []byte("Bearer " + s.opts.Token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			writeError(w, http.StatusUnauthorized, "token 不正确")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()
	st := &model.UpdaterStatus{
		Running:  running,
		ImageTag: s.imageTag(),
		Last:     s.lastRun(),
		Backups:  s.backups(),
		LogTail:  s.logTail(),
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Target string `json:"target"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes)).Decode(&req); err != nil || !targetPattern.MatchString(req.Target) {
		writeError(w, http.StatusBadRequest, "目标版本应为 x.y.z")
		return
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "已有升级在进行中")
		return
	}
	s.running = true
	s.mu.Unlock()

	logFile, err := os.Create(filepath.Join(s.opts.DeployDir, "backups", logFileName))
	if err != nil {
		s.finish()
		writeError(w, http.StatusInternalServerError, "创建日志文件失败: "+err.Error())
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.finish()
		defer logFile.Close()
		// 不用请求的 context：请求返回后升级还要继续跑；升级中 backend 会被重建，连接本来就会断
		if err := s.opts.Run(context.Background(), req.Target, logFile); err != nil {
			fmt.Fprintf(logFile, "\nupdate.sh 退出：%v\n", err)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"target": req.Target})
}

func (s *Server) finish() {
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
}

// imageTag 读 .env 里最后一行 IMAGE_TAG；没有时返回 latest（compose 的默认值）。
func (s *Server) imageTag() string {
	data, err := os.ReadFile(filepath.Join(s.opts.DeployDir, ".env"))
	if err != nil {
		return ""
	}
	tag := "latest"
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		if v, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "IMAGE_TAG="); ok && v != "" {
			tag = strings.Trim(v, `"'`)
		}
	}
	return tag
}

func (s *Server) lastRun() *model.UpgradeRunStatus {
	data, err := os.ReadFile(filepath.Join(s.opts.DeployDir, "backups", statusFileName))
	if err != nil {
		return nil
	}
	var st model.UpgradeRunStatus
	if json.Unmarshal(data, &st) != nil {
		return nil
	}
	// update.sh 写的是容器里的绝对路径（/deploy/backups/...），换成相对部署目录的路径，管理员在服务器上才找得到
	if rel, err := filepath.Rel(s.opts.DeployDir, st.Backup); err == nil && st.Backup != "" && !strings.HasPrefix(rel, "..") {
		st.Backup = filepath.ToSlash(rel)
	}
	st.Message = strings.ReplaceAll(st.Message, strings.TrimRight(filepath.ToSlash(s.opts.DeployDir), "/")+"/", "")
	return &st
}

// backups 列出 backups/ 下的 .dump 文件，新的在前。
func (s *Server) backups() []model.UpgradeBackup {
	entries, err := os.ReadDir(filepath.Join(s.opts.DeployDir, "backups"))
	if err != nil {
		return []model.UpgradeBackup{}
	}
	out := []model.UpgradeBackup{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".dump") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, model.UpgradeBackup{Name: e.Name(), Size: info.Size(), ModTime: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out
}

func (s *Server) logTail() string {
	data, err := os.ReadFile(filepath.Join(s.opts.DeployDir, "backups", logFileName))
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > logTailLines {
		lines = lines[len(lines)-logTailLines:]
	}
	return strings.Join(lines, "\n")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
