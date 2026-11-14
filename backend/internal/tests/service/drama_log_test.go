package service_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
)

// serviceLog 把日志写进临时文件，返回读取全部行的函数。
func serviceLog(t *testing.T) func() []map[string]any {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.log")
	if err := logger.Init(config.Log{Level: "debug", Format: "json", Filename: path, MaxSize: 10}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(logger.Close)
	return func() []map[string]any {
		logger.Close()
		f, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		var out []map[string]any
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
		for sc.Scan() {
			var m map[string]any
			if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
				t.Fatalf("不是合法 JSON：%s", sc.Text())
			}
			out = append(out, m)
		}
		return out
	}
}

func logEvents(lines []map[string]any, ev string) []map[string]any {
	var out []map[string]any
	for _, m := range lines {
		if m["event"] == ev {
			out = append(out, m)
		}
	}
	return out
}

func wantFields(t *testing.T, what string, got map[string]any, want map[string]any) {
	t.Helper()
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s 的 %s = %v，想要 %v（整行：%v）", what, k, got[k], v, got)
		}
	}
}

// 两次输出都不合格：每次不合格打一行 run.step（带校验原因和 call_id），失败收尾带完整原因和最后一次调用。
func TestDramaLog_InvalidOutputTwice(t *testing.T) {
	read := serviceLog(t)
	r := newDramaMemory()
	dramaAddEpisode(r, 1, "")
	s, _ := dramaTestService(r, &dramaTextStreamer{outputs: []string{`{bad`, `{"script_md":""}`}}, nil)
	j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts", Episodes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	lines := read()
	runID := idcodec.Encode(uint64(j.ID))

	creates := logEvents(lines, logger.EventRunCreate)
	if len(creates) != 1 {
		t.Fatalf("应有 1 行 run.create：%v", lines)
	}
	wantFields(t, "run.create", creates[0], map[string]any{"run_kind": "drama", "run_id": runID, "user_id": float64(7), "step": "scripts"})

	starts := logEvents(lines, logger.EventRunStart)
	if len(starts) != 1 {
		t.Fatalf("应有 1 行 run.start：%v", lines)
	}
	wantFields(t, "run.start", starts[0], map[string]any{"run_id": runID, "step": "scripts", "resumed": false})

	steps := logEvents(lines, logger.EventRunStep)
	if len(steps) != 2 {
		t.Fatalf("两次不合格应各打一行 run.step：%v", steps)
	}
	for i, st := range steps {
		wantFields(t, "run.step", st, map[string]any{"level": "warn", "call": "episode=1", "try": float64(i + 1), "error_type": "invalid_output", "run_id": runID})
		if v, _ := st["validation_error"].(string); v == "" {
			t.Errorf("run.step 应带校验原因：%v", st)
		}
		if id, _ := st["call_id"].(string); len(id) != 12 {
			t.Errorf("run.step 应带 call_id：%v", st)
		}
	}

	fins := logEvents(lines, logger.EventRunFinish)
	if len(fins) != 1 {
		t.Fatalf("应有 1 行 run.finish：%v", lines)
	}
	wantFields(t, "run.finish", fins[0], map[string]any{"status": "failed", "level": "warn", "error_type": "invalid_output",
		"error_code": float64(67008), "last_call_id": steps[1]["call_id"], "run_id": runID})
	if _, ok := fins[0]["refunded"]; !ok {
		t.Errorf("失败收尾应带退回的积分：%v", fins[0])
	}
}

func TestDramaLog_Success(t *testing.T) {
	read := serviceLog(t)
	r := newDramaMemory()
	dramaAddEpisode(r, 1, "")
	s, _ := dramaTestService(r, &dramaTextStreamer{outputs: []string{`{"script_md":"剧本"}`}}, nil)
	j, _ := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts", Episodes: []int{1}})
	if err := s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	fins := logEvents(read(), logger.EventRunFinish)
	if len(fins) != 1 {
		t.Fatalf("应有 1 行 run.finish：%v", fins)
	}
	wantFields(t, "run.finish", fins[0], map[string]any{"status": "succeeded", "level": "info", "credits": float64(5)})
	if _, ok := fins[0]["last_call_id"]; ok {
		t.Errorf("成功收尾不带 last_call_id：%v", fins[0])
	}
}

// 模型调用本身失败（非上游的普通错误）：原始错误原样进日志，不再只剩「服务内部错误」。
func TestDramaLog_CallFailureKeepsCause(t *testing.T) {
	read := serviceLog(t)
	r := newDramaMemory()
	dramaAddEpisode(r, 1, "")
	s, _ := dramaTestService(r, &dramaTextStreamer{failAt: 1, failErr: errors.New("connection reset by peer")}, nil)
	j, _ := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts", Episodes: []int{1}})
	if err := s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	fin := logEvents(read(), logger.EventRunFinish)[0]
	wantFields(t, "run.finish", fin, map[string]any{"status": "failed", "error_type": "network"})
	if e, _ := fin["error"].(string); !strings.Contains(e, "connection reset by peer") {
		t.Errorf("失败收尾应带原始错误：%v", fin)
	}
	if r.jobs[0].ErrorMessage == "" || strings.Contains(r.jobs[0].ErrorMessage, "connection reset") {
		t.Errorf("给用户看的文案不变（不透出内部错误）：%q", r.jobs[0].ErrorMessage)
	}
}

func TestDramaLog_CancelByUser(t *testing.T) {
	read := serviceLog(t)
	r := newDramaMemory()
	dramaAddEpisode(r, 1, "")
	s, _ := dramaTestService(r, &dramaTextStreamer{}, nil)
	j, _ := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts", Episodes: []int{1}})
	if _, err := s.CancelJob(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	fins := logEvents(read(), logger.EventRunFinish)
	if len(fins) != 1 {
		t.Fatalf("取消应打一行 run.finish：%v", fins)
	}
	wantFields(t, "run.finish", fins[0], map[string]any{"status": "canceled", "by": "user", "run_id": idcodec.Encode(uint64(j.ID))})
}

// 合成失败：用户只看到固定文案，日志里有 ffmpeg 的真实原因，并且是 Error 级（需要人处理）。
func TestDramaLog_ComposeFailureKeepsCauseForOps(t *testing.T) {
	read := serviceLog(t)
	r := newComposeMemory()
	p := &composeProcessor{err: errors.New("private ffmpeg path")}
	s := composeService(r, &composeFiles{r: r}, p)
	job, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "compose", Episodes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.HandleDrama(context.Background(), 7, 1, uint64(job.ID)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.jobs[0].ErrorMessage, "private") {
		t.Fatalf("给用户的文案不能带内部原因：%q", r.jobs[0].ErrorMessage)
	}
	fin := logEvents(read(), logger.EventRunFinish)[0]
	wantFields(t, "run.finish", fin, map[string]any{"status": "failed", "level": "error", "error_type": "compose_transcode", "needs_attention": true})
	if e, _ := fin["error"].(string); !strings.Contains(e, "private ffmpeg path") || !strings.Contains(e, "转码失败") {
		t.Errorf("日志应带固定文案和底层原因：%v", fin)
	}
}
