package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	. "video-canvas/internal/provider/worker"
)

// ---------------------------------------------------------------------------
// 异步：提交 → 轮询 → 转存 → 完成
// ---------------------------------------------------------------------------

func TestWorker_FullFlow_SubmitPollFinalizeSucceed(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, nil)
	env.exec.queryFn = func(n int, ref provider.TaskRef) (*provider.QueryResult, error) {
		switch n {
		case 1:
			return &provider.QueryResult{Status: provider.StatusQueued}, nil
		case 2:
			return &provider.QueryResult{Status: provider.StatusRunning, Progress: intPtr(40)}, nil
		case 3:
			return &provider.QueryResult{Status: provider.StatusRunning, Progress: intPtr(80)}, nil
		default: // 第 4 次是上游出片，第 5 次（finalizing 里重新取产物地址）也返回产物
			return succeededResult("https://p.example.com/a.mp4", "https://p.example.com/cover.mp4"), nil
		}
	}

	// 1. pending → 提交 → queued，第一次查询安排在 +10s
	if n := env.runOnce(t); n != 1 {
		t.Fatalf("应领取 1 个任务：%d", n)
	}
	got := env.store.get(1)
	if got.Status != model.TaskQueued || got.ProviderTaskID != "pt-1" || !got.NextPollAt.Equal(env.clock.Now().Add(10*time.Second)) {
		t.Fatalf("提交后状态不对：%+v", got)
	}
	in := env.exec.submitIns[0]
	if in.Task.ID != 1 || in.Task.UserID != 7 || in.Input["prompt"] != "猫" || in.OnPrepared == nil {
		t.Fatalf("提交参数不对：%+v", in)
	}

	// 2. 没到点不会被领取
	if n := env.runOnce(t); n != 0 {
		t.Fatalf("未到期不应领取：%d", n)
	}

	// 3. +10s：查询到 queued，下一次 +5s（attempts=1）
	env.clock.Advance(10 * time.Second)
	env.runOnce(t)
	got = env.store.get(1)
	if got.Status != model.TaskQueued || got.PollAttempts != 1 || !got.NextPollAt.Equal(env.clock.Now().Add(5*time.Second)) {
		t.Fatalf("第一次查询后状态不对：%+v", got)
	}

	// 4. +5s：running 40%，下一次 +10s（间隔翻倍）
	env.clock.Advance(5 * time.Second)
	env.runOnce(t)
	got = env.store.get(1)
	if got.Status != model.TaskRunning || got.Progress == nil || *got.Progress != 40 || !got.NextPollAt.Equal(env.clock.Now().Add(10*time.Second)) {
		t.Fatalf("第二次查询后状态不对：%+v", got)
	}

	// 5. +10s：running 80%，下一次 +15s（封顶 maxInterval）
	env.clock.Advance(10 * time.Second)
	env.runOnce(t)
	got = env.store.get(1)
	if *got.Progress != 80 || !got.NextPollAt.Equal(env.clock.Now().Add(15*time.Second)) {
		t.Fatalf("第三次查询后状态不对：%+v", got)
	}

	// 6. +15s：上游出片 → finalizing，立即到期
	env.clock.Advance(15 * time.Second)
	env.runOnce(t)
	got = env.store.get(1)
	if got.Status != model.TaskFinalizing || !got.NextPollAt.Equal(env.clock.Now()) {
		t.Fatalf("应进入 finalizing 且立即可处理：%+v", got)
	}

	// 7. finalizing：重新查询一次拿产物地址，转存 2 个产物，一次性 Complete
	env.runOnce(t)
	got = env.store.get(1)
	if got.Status != model.TaskSucceeded {
		t.Fatalf("应成功：%+v", got)
	}
	if _, q, _ := env.exec.counts(); q != 5 {
		t.Fatalf("异步任务转存时应重新查询一次，共 5 次查询：%d", q)
	}
	outs := env.store.completedOf(1)
	if len(outs) != 2 || outs[0].AssetID != 1001 || outs[1].AssetID != 1002 || outs[0].MediaType != model.KindVideo ||
		outs[0].URL != "https://cdn.example.com/1001" || outs[0].Width != 1280 || outs[0].DurationMs != 5000 {
		t.Fatalf("转存产物不对：%+v", outs)
	}
	s0 := env.saver.saves[0]
	if s0.UserID != 7 || s0.TaskID != 1 || s0.Kind != model.KindVideo || s0.MimeType != "video/mp4" || s0.MaxBytes != 1<<20 || s0.FileName != "task-1-1.mp4" {
		t.Fatalf("转存参数不对：%+v", s0)
	}
	if env.saver.bodies[0] != "video-bytes:https://p.example.com/a.mp4" {
		t.Fatalf("应保存下载到的内容：%q", env.saver.bodies[0])
	}
	if len(env.store.fails) != 0 {
		t.Fatalf("不应有失败：%+v", env.store.fails)
	}
	// 终态之后不会再被领取
	env.clock.Advance(time.Hour)
	if n := env.runOnce(t); n != 0 {
		t.Fatalf("终态任务不应再被领取：%d", n)
	}
}

// ---------------------------------------------------------------------------
// 提交失败的分类映射
// ---------------------------------------------------------------------------

func TestWorker_Submit_Failures(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantCode     string
		wantMsg      string
		wantNotInMsg string
		wantAlert    string // 期望的 Error 级告警文案，空表示不要求
	}{
		{"审核未通过", &provider.Error{Class: provider.ClassModeration, Message: "raw: nsfw detected id=abc"}, "moderation", "内容未通过审核", "nsfw", ""},
		{"上游余额不足对用户显示服务繁忙并告警", &provider.Error{Class: provider.ClassProviderBalance, Message: "raw: insufficient balance 0.00"}, "provider_balance", "服务繁忙，请稍后再试", "balance", "余额不足"},
		{"提交结果未知", &provider.Error{Class: provider.ClassSubmitUnknown, Message: "raw: read timeout after 30s"}, "submit_unknown", "提交结果未知，积分已退回", "timeout after", ""},
		{"终态错误", &provider.Error{Class: provider.ClassTerminal, Message: "raw: node 12 missing"}, "provider_error", "平台繁忙，请稍后重试", "node 12", ""},
		{"参数错误", &provider.Error{Class: provider.ClassTerminal, Code: "invalid_param", Message: "raw: bad"}, "invalid_param", "参数不合法，请调整后重试", "raw", ""},
		{"SSRF 拦截按终态处理", &provider.Error{Class: provider.ClassTerminal, Code: provider.CodeSSRFBlocked, Message: "raw: 10.0.0.1"}, "provider_error", "平台繁忙，请稍后重试", "10.0.0.1", ""},
		{"未分类的普通错误按终态处理", errors.New("raw: boom secret-detail"), "provider_error", "平台繁忙，请稍后重试", "secret-detail", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newWkEnv(nil)
			env.task(1, model.TaskPending, nil)
			env.exec.submitFn = func(int, provider.SubmitInput) (*provider.SubmitResult, error) { return nil, tt.err }
			env.runOnce(t)

			got := env.store.get(1)
			if got.Status != model.TaskFailed || got.ErrorCode != tt.wantCode || got.ErrorMessage != tt.wantMsg {
				t.Fatalf("期望 failed/%s/%s，实际 %+v", tt.wantCode, tt.wantMsg, got)
			}
			if strings.Contains(got.ErrorMessage, tt.wantNotInMsg) {
				t.Fatalf("不能把上游原始信息透给用户：%s", got.ErrorMessage)
			}
			if tt.wantAlert != "" && len(env.logs.entries("error", tt.wantAlert)) != 1 {
				t.Fatalf("应记一条 Error 告警：%s", tt.wantAlert)
			}
			// 失败后不会再被处理
			env.clock.Advance(time.Hour)
			if n := env.runOnce(t); n != 0 {
				t.Fatalf("失败任务不应再被领取：%d", n)
			}
			if s, _, _ := env.exec.counts(); s != 1 {
				t.Fatalf("不应重试提交：%d 次", s)
			}
		})
	}

	others := []struct {
		name     string
		task     func(t *model.GenerationTask)
		submit   func(int, provider.SubmitInput) (*provider.SubmitResult, error)
		wantCode string
		wantCall int // 期望的提交次数
	}{
		{
			name:     "上游返回空任务 id 视为失败",
			submit:   func(int, provider.SubmitInput) (*provider.SubmitResult, error) { return &provider.SubmitResult{}, nil },
			wantCode: "provider_error", wantCall: 1,
		},
		{
			name:     "宿主返回空结果视为失败",
			submit:   func(int, provider.SubmitInput) (*provider.SubmitResult, error) { return nil, nil },
			wantCode: "provider_error", wantCall: 1,
		},
		{
			name:     "输入损坏直接失败退款且不调用上游",
			task:     func(t *model.GenerationTask) { t.InputJSON = []byte(`not json`) },
			wantCode: "invalid_param", wantCall: 0,
		},
		{
			name:     "快照损坏直接失败退款且不调用上游",
			task:     func(t *model.GenerationTask) { t.ConfigSnapshot = []byte(`{`) },
			wantCode: "provider_error", wantCall: 0,
		},
	}
	for _, tt := range others {
		t.Run(tt.name, func(t *testing.T) {
			env := newWkEnv(nil)
			env.task(1, model.TaskPending, tt.task)
			env.exec.submitFn = tt.submit
			env.runOnce(t)
			if got := env.store.get(1); got.Status != model.TaskFailed || got.ErrorCode != tt.wantCode {
				t.Fatalf("应失败 %s：%+v", tt.wantCode, got)
			}
			if s, _, _ := env.exec.counts(); s != tt.wantCall {
				t.Fatalf("提交次数不对：%d", s)
			}
		})
	}
}

func TestWorker_Submit_RetryableBackoff(t *testing.T) {
	env := newWkEnv(func(o *Options) { o.MaxSubmitRetries = 3 })
	env.task(1, model.TaskPending, nil)
	env.exec.submitFn = func(int, provider.SubmitInput) (*provider.SubmitResult, error) {
		return nil, &provider.Error{Class: provider.ClassRetryable, Message: "connection refused"}
	}

	// 前 3 次失败：指数退避 2s / 4s / 8s，任务保持 pending，不失败
	wantDelays := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}
	for i, d := range wantDelays {
		env.runOnce(t)
		got := env.store.get(1)
		if got.Status != model.TaskPending || got.PollAttempts != i+1 || !got.NextPollAt.Equal(env.clock.Now().Add(d)) {
			t.Fatalf("第 %d 次失败后状态不对：%+v（期望退避 %v）", i+1, got, d)
		}
		// 没到点不会重试
		if n := env.runOnce(t); n != 0 {
			t.Fatalf("退避期间不应领取：%d", n)
		}
		env.clock.Advance(d)
	}

	// 第 4 次失败：超过最多重试次数，直接失败退款
	env.runOnce(t)
	got := env.store.get(1)
	if got.Status != model.TaskFailed || got.ErrorCode != "provider_error" {
		t.Fatalf("重试耗尽后应失败：%+v", got)
	}
	if s, _, _ := env.exec.counts(); s != 4 {
		t.Fatalf("应提交 4 次（1 次 + 3 次重试）：%d", s)
	}
}

func TestWorker_Submit_RetryThenSuccess(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, nil)
	env.exec.submitFn = func(n int, in provider.SubmitInput) (*provider.SubmitResult, error) {
		if n == 1 {
			return nil, &provider.Error{Class: provider.ClassRetryable, Message: "429"}
		}
		return &provider.SubmitResult{ProviderTaskID: "pt-ok"}, nil
	}
	env.runOnce(t)
	env.clock.Advance(2 * time.Second)
	env.runOnce(t)
	got := env.store.get(1)
	if got.Status != model.TaskQueued || got.ProviderTaskID != "pt-ok" || got.PollAttempts != 0 {
		t.Fatalf("重试成功后应进入 queued 且计数清零：%+v", got)
	}
}

func TestWorker_Submit_CanceledDuringSubmitCancelsProviderTask(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, nil)
	env.exec.submitFn = func(int, provider.SubmitInput) (*provider.SubmitResult, error) {
		return &provider.SubmitResult{ProviderTaskID: "pt-1", State: json.RawMessage(`{"k":1}`)}, nil
	}
	// 提交返回后、写库之前，用户把任务取消了
	env.store.beforeMarkSubmitted = func(id uint64) { env.store.setStatus(id, model.TaskCanceled) }
	env.runOnce(t)

	if got := env.store.get(1); got.Status != model.TaskCanceled || got.ProviderTaskID != "" {
		t.Fatalf("任务应保持取消状态：%+v", got)
	}
	cancels := env.exec.cancelList()
	if len(cancels) != 1 || cancels[0].ProviderTaskID != "pt-1" || string(cancels[0].State) != `{"k":1}` {
		t.Fatalf("应尽力取消刚创建的上游任务（带上插件 state）：%+v", cancels)
	}
}

// ---------------------------------------------------------------------------
// 同步：immediate
// ---------------------------------------------------------------------------

// 文本产物带的 Token 用量跟着 provider_result 落库，转存完成时汇总交给 store.Complete 结算。
func TestWorker_Submit_ImmediateUsagePassedToComplete(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, func(t *model.GenerationTask) {
		t.Kind = model.KindText
		t.ConfigSnapshot = wkSnapshotJSON(model.KindText, nil)
	})
	env.exec.submitFn = func(int, provider.SubmitInput) (*provider.SubmitResult, error) {
		return &provider.SubmitResult{ProviderTaskID: "sync-1", Immediate: &provider.QueryResult{
			Status: provider.StatusSucceeded,
			Outputs: []provider.Output{
				{Type: provider.OutputText, Text: "一", Usage: &modelcfg.Usage{InputTokens: 10, OutputTokens: 20}},
				{Type: provider.OutputText, Text: "二", Usage: &modelcfg.Usage{InputTokens: 1, OutputTokens: 2}},
			},
		}}, nil
	}
	env.runOnce(t)
	env.runOnce(t)
	if got := env.store.get(1); got.Status != model.TaskSucceeded {
		t.Fatalf("应成功：%+v", got)
	}
	if u := env.store.usageOf(1); u == nil || u.InputTokens != 11 || u.OutputTokens != 22 {
		t.Fatalf("应汇总用量交给 Complete：%+v", u)
	}
}

// 没有任何产物带用量时交给 Complete 的是 nil（由任务服务按冻结额结算）。
func TestWorker_Submit_NoUsageIsNil(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, func(t *model.GenerationTask) {
		t.Kind = model.KindText
		t.ConfigSnapshot = wkSnapshotJSON(model.KindText, nil)
	})
	env.exec.submitFn = func(int, provider.SubmitInput) (*provider.SubmitResult, error) {
		return &provider.SubmitResult{ProviderTaskID: "sync-1", Immediate: &provider.QueryResult{
			Status: provider.StatusSucceeded, Outputs: []provider.Output{{Type: provider.OutputText, Text: "一"}},
		}}, nil
	}
	env.runOnce(t)
	env.runOnce(t)
	if env.store.get(1).Status != model.TaskSucceeded || env.store.usageOf(1) != nil {
		t.Fatalf("没有用量时应传 nil：%+v", env.store.usageOf(1))
	}
}

func TestWorker_Submit_ImmediateSucceeded(t *testing.T) {
	tests := []struct {
		name      string
		kind      string
		outputs   []provider.Output
		wantSaves int
		want      model.TaskOutput
	}{
		{
			name:    "文本产物不转存直接写正文",
			kind:    model.KindText,
			outputs: []provider.Output{{Type: provider.OutputText, Text: "你好，世界"}},
			want:    model.TaskOutput{MediaType: model.KindText, Text: "你好，世界"},
		},
		{
			name:      "url 产物下载转存",
			kind:      model.KindImage,
			outputs:   []provider.Output{{Type: provider.OutputURL, URL: "https://p.example.com/x.png", Mime: "image/png"}},
			wantSaves: 1,
			want:      model.TaskOutput{AssetID: 1001, URL: "https://cdn.example.com/1001", MediaType: model.KindImage, DurationMs: 5000, Width: 1280, Height: 720},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newWkEnv(nil)
			env.task(1, model.TaskPending, func(t *model.GenerationTask) {
				t.Kind = tt.kind
				t.ConfigSnapshot = wkSnapshotJSON(tt.kind, nil)
			})
			env.exec.submitFn = func(int, provider.SubmitInput) (*provider.SubmitResult, error) {
				return &provider.SubmitResult{
					ProviderTaskID: "sync-1", State: json.RawMessage(`{"outer":1}`),
					Immediate: &provider.QueryResult{Status: provider.StatusSucceeded, Outputs: tt.outputs, State: json.RawMessage(`{"inner":1}`)},
				}, nil
			}

			// 1. 提交即成功：MarkImmediate 写入 provider_result，迁到 finalizing 并立即到期；Immediate 的 state 优先
			env.runOnce(t)
			got := env.store.get(1)
			if got.Status != model.TaskFinalizing || got.ProviderTaskID != "sync-1" || !got.NextPollAt.Equal(env.clock.Now()) {
				t.Fatalf("应进入 finalizing：%+v", got)
			}
			var saved []provider.Output
			if err := json.Unmarshal(got.ProviderResult, &saved); err != nil || len(saved) != len(tt.outputs) || saved[0] != tt.outputs[0] {
				t.Fatalf("provider_result 应是即时产物：%s %v", got.ProviderResult, err)
			}
			if st := env.store.state(1); string(st.Plugin) != `{"inner":1}` {
				t.Fatalf("应优先保存 Immediate 带的 state：%s", st.Plugin)
			}
			if env.store.count("MarkSubmitted") != 0 {
				t.Fatalf("同步结果不应走 MarkSubmitted：%v", env.store.callList())
			}

			// 2. 转存：直接用 provider_result，不调用上游 Query
			env.runOnce(t)
			if got := env.store.get(1); got.Status != model.TaskSucceeded {
				t.Fatalf("应成功：%+v", got)
			}
			if _, q, _ := env.exec.counts(); q != 0 {
				t.Fatalf("同步结果不应再查询上游：%d", q)
			}
			outs := env.store.completedOf(1)
			if len(outs) != 1 || outs[0] != tt.want {
				t.Fatalf("产物不对：%+v", outs)
			}
			if n := env.saver.saveCount(); n != tt.wantSaves {
				t.Fatalf("保存次数不对：%d", n)
			}
		})
	}
}

func TestWorker_Submit_ImmediateFailed(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, nil)
	env.exec.submitFn = func(int, provider.SubmitInput) (*provider.SubmitResult, error) {
		return &provider.SubmitResult{Immediate: &provider.QueryResult{
			Status: provider.StatusFailed, ErrorClass: provider.ClassModeration, ErrorCode: "1501", ErrorMessage: "raw: nsfw",
		}}, nil
	}
	env.runOnce(t)
	got := env.store.get(1)
	if got.Status != model.TaskFailed || got.ErrorCode != "moderation" || got.ErrorMessage != "内容未通过审核" {
		t.Fatalf("应按 Immediate 的分类失败：%+v", got)
	}
	if env.store.count("MarkSubmitted") != 0 || env.store.count("MarkImmediate") != 0 {
		t.Fatalf("失败不应记录提交：%v", env.store.callList())
	}
}

func TestWorker_Submit_ImmediateRunningIsTreatedAsAsync(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, nil)
	env.exec.submitFn = func(int, provider.SubmitInput) (*provider.SubmitResult, error) {
		return &provider.SubmitResult{
			ProviderTaskID: "pt-x", State: json.RawMessage(`{"outer":1}`),
			Immediate: &provider.QueryResult{Status: provider.StatusRunning, State: json.RawMessage(`{"inner":1}`)},
		}, nil
	}
	env.runOnce(t)
	got := env.store.get(1)
	if got.Status != model.TaskQueued || got.ProviderTaskID != "pt-x" || !got.NextPollAt.Equal(env.clock.Now().Add(10*time.Second)) {
		t.Fatalf("running 的 Immediate 应按异步受理：%+v", got)
	}
	if st := env.store.state(1); string(st.Plugin) != `{"inner":1}` {
		t.Fatalf("应优先保存 Immediate 带的 state：%s", st.Plugin)
	}
}

func TestWorker_Submit_ImmediateCanceledDuringSubmitCancelsUpstream(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, nil)
	env.exec.submitFn = func(int, provider.SubmitInput) (*provider.SubmitResult, error) {
		return &provider.SubmitResult{ProviderTaskID: "sync-1", Immediate: &provider.QueryResult{
			Status: provider.StatusSucceeded, Outputs: []provider.Output{{Type: provider.OutputURL, URL: "https://p.example.com/a.mp4"}},
		}}, nil
	}
	env.store.beforeMarkSubmitted = func(id uint64) { env.store.setStatus(id, model.TaskCanceled) }
	env.runOnce(t)
	if got := env.store.get(1); got.Status != model.TaskCanceled || len(got.ProviderResult) != 0 {
		t.Fatalf("取消状态不应被覆盖：%+v", got)
	}
	if c := env.exec.cancelList(); len(c) != 1 || c[0].ProviderTaskID != "sync-1" {
		t.Fatalf("应尽力取消上游：%+v", c)
	}
	if _, _, d := env.exec.counts(); d != 0 {
		t.Fatalf("不应转存：%d", d)
	}
}

// ---------------------------------------------------------------------------
// 准备阶段与 provider_state
// ---------------------------------------------------------------------------

func TestWorker_Submit_OnPreparedPersistsAndRetryReusesPrepared(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, func(t *model.GenerationTask) {
		t.ProviderState = []byte(`{"plugin":{"p":1}}`)
	})
	env.exec.submitFn = func(n int, in provider.SubmitInput) (*provider.SubmitResult, error) {
		if n == 1 {
			// 第一次：准备阶段（上传）成功并落库，随后提交请求遇到 502
			if in.Task.Prepared != nil {
				t.Errorf("第一次提交不应有 prepared：%s", in.Task.Prepared)
			}
			if err := in.OnPrepared(context.Background(), json.RawMessage(`{"image":"https://rh/u1"}`)); err != nil {
				t.Errorf("OnPrepared 不应失败：%v", err)
			}
			return nil, &provider.Error{Class: provider.ClassRetryable, Message: "502"}
		}
		// 重试：宿主拿到已落库的 prepared，跳过上传
		if string(in.Task.Prepared) != `{"image":"https://rh/u1"}` || string(in.Task.State) != `{"p":1}` {
			t.Errorf("重试时应带上 prepared 与插件 state：%+v", in.Task)
		}
		return &provider.SubmitResult{ProviderTaskID: "pt-1"}, nil
	}

	env.runOnce(t)
	st := env.store.state(1)
	if string(st.Prepared) != `{"image":"https://rh/u1"}` || string(st.Plugin) != `{"p":1}` {
		t.Fatalf("prepared 应并进 provider_state 且保留插件 state：%+v", st)
	}
	if got := env.store.get(1); got.Status != model.TaskPending || got.PollAttempts != 1 {
		t.Fatalf("提交失败应保持 pending 等待重试：%+v", got)
	}

	env.clock.Advance(2 * time.Second)
	env.runOnce(t)
	if got := env.store.get(1); got.Status != model.TaskQueued {
		t.Fatalf("重试应成功：%+v", got)
	}
	if st := env.store.state(1); string(st.Prepared) != `{"image":"https://rh/u1"}` {
		t.Fatalf("提交成功后 prepared 应保留：%+v", st)
	}
}

func TestWorker_Submit_OnPreparedStoreErrorIsReturned(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, nil)
	storeErr := errors.New("db down")
	env.store.saveStateErr = storeErr
	var cbErr error
	env.exec.submitFn = func(n int, in provider.SubmitInput) (*provider.SubmitResult, error) {
		cbErr = in.OnPrepared(context.Background(), json.RawMessage(`{"x":1}`))
		// 宿主约定：回调失败按 retryable 处理
		return nil, &provider.Error{Class: provider.ClassRetryable, Message: "保存 prepared 失败", Cause: cbErr}
	}
	env.runOnce(t)
	if !errors.Is(cbErr, storeErr) {
		t.Fatalf("回调应返回包装后的存储错误：%v", cbErr)
	}
	if got := env.store.get(1); got.Status != model.TaskPending || got.PollAttempts != 1 {
		t.Fatalf("应按可重试失败处理：%+v", got)
	}
}

func TestWorker_Submit_CorruptProviderStateTreatedAsEmpty(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, func(t *model.GenerationTask) { t.ProviderState = []byte(`{`) })
	env.runOnce(t)
	if in := env.exec.submitIns[0]; in.Task.State != nil || in.Task.Prepared != nil {
		t.Fatalf("损坏的 provider_state 应当空：%+v", in.Task)
	}
	if got := env.store.get(1); got.Status != model.TaskQueued {
		t.Fatalf("应照常提交：%+v", got)
	}
	if len(env.logs.entries("warn", "provider_state 损坏")) != 1 {
		t.Fatal("应记警告")
	}
}
