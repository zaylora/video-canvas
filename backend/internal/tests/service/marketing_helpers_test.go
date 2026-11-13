package service_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/marketing"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
	"video-canvas/internal/service/stepfake"
	"video-canvas/internal/stepkit"
)

// 本文件是营销工作室服务的集成测试环境：真实仓储（TEST_DATABASE_DSN 里的独立 schema）、真实生成任务服务，
// 文本模型调用、素材、画布用内存 fake。未设置 TEST_DATABASE_DSN 时整组用例跳过。

var (
	mktSchemaSeq atomic.Int64
	mktUserSeq   atomic.Uint64
)

// mktDB 为本用例建独立 schema 并迁移营销与任务相关的表。
func mktDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("未设置 TEST_DATABASE_DSN，跳过营销集成测试")
	}
	quiet := &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)}
	admin, err := gorm.Open(postgres.Open(dsn), quiet)
	if err != nil {
		t.Fatalf("连接测试库失败：%v", err)
	}
	schema := fmt.Sprintf("mkt_%d_%d", time.Now().UnixNano()%1_000_000_000, mktSchemaSeq.Add(1))
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("创建 schema 失败：%v", err)
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), quiet)
	if err != nil {
		t.Fatalf("连接测试 schema 失败：%v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(30)
	t.Cleanup(func() {
		_ = sqlDB.Close()
		_ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		if a, err := admin.DB(); err == nil {
			_ = a.Close()
		}
	})
	if err := db.AutoMigrate(&model.GenerationTask{}, &model.GenerationTaskEvent{}, &model.UserCredit{}, &model.CreditLedger{}, &model.Asset{},
		&model.MarketingProject{}, &model.MarketingRound{}, &model.MarketingStepRun{}, &model.MarketingScript{}); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	return db
}

// mktQueue 记录入队的轮次（不真正跑 River）。
type mktQueue struct {
	mu     sync.Mutex
	rounds []uint64
}

func (q *mktQueue) EnqueueMarketingRoundTx(_ context.Context, _ *sql.Tx, roundID uint64) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.rounds = append(q.rounds, roundID)
	return nil
}

func (q *mktQueue) count() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.rounds)
}

// mktAsset 是内存素材：元数据加内容。
type mktAsset struct {
	asset *model.Asset
	data  []byte
}

// mktAssets 实现 MarketingAssets 与 provider.AssetStore。
type mktAssets struct{ items map[uint64]*mktAsset }

func (a *mktAssets) put(userID, id uint64, kind, mime string, durationMs int64) {
	a.items[id] = &mktAsset{asset: &model.Asset{ID: id, UserID: userID, Kind: kind, MimeType: mime, DurationMs: durationMs, ByteSize: 8, FileName: fmt.Sprintf("a%d", id), StorageKey: fmt.Sprintf("k%d", id)}, data: []byte("12345678")}
}

func (a *mktAssets) Get(_ context.Context, userID, id uint64) (*model.Asset, error) {
	it, ok := a.items[id]
	if !ok || it.asset.UserID != userID {
		return nil, provider.ErrAssetNotFound
	}
	return it.asset, nil
}

func (a *mktAssets) Open(ctx context.Context, userID, id uint64) (*provider.AssetFile, error) {
	asset, err := a.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	return &provider.AssetFile{Asset: asset, Body: io.NopCloser(bytes.NewReader(a.items[id].data))}, nil
}

func (a *mktAssets) View(ctx context.Context, userID, id uint64) (*model.AssetView, error) {
	asset, err := a.Get(ctx, userID, id)
	if err != nil {
		return nil, errcode.ErrAssetNotFound
	}
	return &model.AssetView{ID: id, Kind: asset.Kind, URL: a.FileURL(asset.StorageKey), MimeType: asset.MimeType, DurationMs: asset.DurationMs, FileName: asset.FileName}, nil
}

func (a *mktAssets) FileURL(key string) string { return "https://files.test/" + key }

// mktCanvases 记录发到画布的请求。
type mktCanvases struct {
	req *model.CreateCanvasProjectReq
	err error
}

func (c *mktCanvases) Create(_ context.Context, _ uint64, req *model.CreateCanvasProjectReq) (*model.CanvasProject, error) {
	if c.err != nil {
		return nil, c.err
	}
	c.req = req
	return &model.CanvasProject{BaseModel: model.BaseModel{ID: 77}}, nil
}

// mktCaller 按提示词里的环节职责区分环节，给每个环节脚本化的回复；可让某个环节阻塞直到放行或被取消。
type mktCaller struct {
	mu      sync.Mutex
	replies map[string]func(call int) (string, error) // 环节 key → 回复
	calls   map[string]int
	block   map[string]chan struct{}
	started map[string]chan struct{}
}

// 提示词里能认出环节的职责片段。
var mktDuty = map[string]string{
	marketing.StepFacts:   "提取商品事实",
	marketing.StepReverse: "逐镜头的分镜表",
	marketing.StepFollow:  "跟拍脚本",
	marketing.StepCreate:  "按给定的创意角度各写",
}

func newMktCaller() *mktCaller {
	return &mktCaller{replies: map[string]func(int) (string, error){}, calls: map[string]int{}, block: map[string]chan struct{}{}, started: map[string]chan struct{}{}}
}

func (c *mktCaller) on(step string, fn func(call int) (string, error)) { c.replies[step] = fn }

func (c *mktCaller) count(step string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[step]
}

func (c *mktCaller) Complete(ctx context.Context, _ *provider.Snapshot, msgs []llmgateway.Message) (*llmgateway.Result, error) {
	prompt := msgs[0].Text
	for _, p := range msgs[0].Parts {
		prompt += p.Text
	}
	step := ""
	for k, duty := range mktDuty {
		if strings.Contains(prompt, duty) {
			step = k
		}
	}
	c.mu.Lock()
	c.calls[step]++
	n := c.calls[step]
	fn := c.replies[step]
	blk := c.block[step]
	started := c.started[step]
	c.mu.Unlock()
	if started != nil {
		close(started)
	}
	if blk != nil {
		select {
		case <-blk:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if fn == nil {
		return nil, fmt.Errorf("环节 %s 没有预设回复", step)
	}
	text, err := fn(n)
	if err != nil {
		return nil, err
	}
	return &llmgateway.Result{Text: text, Usage: llmgateway.Usage{InputTokens: 100, OutputTokens: 50}}, nil
}

// mktRegistry 给生成任务服务用：任何 key 都返回同一个视频模型快照。
type mktRegistry struct{ snap *provider.Snapshot }

func (r *mktRegistry) ListModels(context.Context, string) ([]provider.ModelInfo, error) {
	return nil, nil
}
func (r *mktRegistry) Snapshot(context.Context, string) (*provider.Snapshot, error) {
	return r.snap, nil
}

const mktVideoKey = "video-a"

// mktVideoSnapshot 是支持全能参考、参考视频和有声的视频模型：每次出片 20 积分。
func mktVideoSnapshot() *provider.Snapshot {
	one := 4
	maxDur, minDur := 15, 4
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: mktVideoKey, Kind: modelcfg.KindVideo, UpstreamModel: "up",
			Pricing: modelcfg.Pricing{Billing: modelcfg.BillingPerCall, Unit: 20},
			Capabilities: modelcfg.Capabilities{
				Ops:          []string{modelcfg.OpT2V, modelcfg.OpOmni},
				Refs:         modelcfg.Refs{Image: modelcfg.RefSpec{On: true, Max: 5, MaxMB: 10}, Video: modelcfg.RefSpec{On: true, Max: 1, MaxMB: 50}},
				Prompt:       modelcfg.PromptSpec{MaxLength: 6000},
				PromptFormat: modelcfg.PromptFormatSeedance,
				Params: modelcfg.ParamSet{
					{Name: "aspect_ratio", ParamField: modelcfg.ParamField{Type: modelcfg.ParamEnum, Open: true, Options: []any{"9:16", "16:9"}, Default: "9:16"}},
					{Name: "duration", ParamField: modelcfg.ParamField{Type: modelcfg.ParamNumber, Open: true, Min: &minDur, Max: &maxDur, Step: &one, Default: float64(15)}},
					{Name: "generate_audio", ParamField: modelcfg.ParamField{Type: modelcfg.ParamBoolean, Open: true, Default: true}},
				},
			}},
		Channel: provider.ChannelSnapshot{Key: "p1", PluginKey: "demo", PluginVersionID: 7, BaseURL: "https://up.example.com"},
		Plugin:  provider.PluginSnapshot{Key: "demo", Version: "1.0.0"},
	}
}

// mktEnv 汇总一次测试用到的服务与 fake。
type mktEnv struct {
	t        *testing.T
	db       *gorm.DB
	repo     *repository.MarketingRepository
	taskRepo *repository.GenerationTaskRepository
	svc      *MarketingService
	taskSvc  *GenerationTaskService
	models   *stepfake.Models
	caller   *mktCaller
	assets   *mktAssets
	canvases *mktCanvases
	queue    *mktQueue
	bc       *fakeTaskBroadcaster
	user     uint64
	initial  int
	reg      *stepkit.Registry
	resolver *stepkit.Resolver
}

// newMktEnv 组装环境：文本模型 text-a（能看图、能看视频，每次调用 2 积分），视频模型 video-a；用户初始积分 initial。
func newMktEnv(t *testing.T, initial int) *mktEnv {
	t.Helper()
	db := mktDB(t)
	env := &mktEnv{t: t, db: db, repo: repository.NewMarketingRepository(db), taskRepo: repository.NewGenerationTaskRepository(db),
		models: stepfake.NewModels(), caller: newMktCaller(), assets: &mktAssets{items: map[uint64]*mktAsset{}}, canvases: &mktCanvases{},
		queue: &mktQueue{}, bc: &fakeTaskBroadcaster{}, user: uint64(time.Now().UnixNano()/1000)*100 + mktUserSeq.Add(1)%100}
	env.models.Snapshots["text-a"] = stepfake.TextModel("text-a", true, true, 2)
	env.models.Snapshots[mktVideoKey] = mktVideoSnapshot()
	env.repo.SetEnqueuer(env.queue)
	limits := &fakeLimits{initial: initial, defMax: 20}
	env.taskSvc = NewGenerationTaskService(GenerationTaskDeps{Repo: env.taskRepo, Registry: &mktRegistry{snap: mktVideoSnapshot()}, Executor: &fakeTaskExecutor{},
		Assets: env.assets, Broadcaster: env.bc, Limits: limits})
	env.initial = initial
	env.reg = stepkit.NewRegistry()
	marketing.RegisterSteps(env.reg)
	env.resolver = stepkit.NewResolver(emptyBindings{}, stepfake.NewLibrary())
	env.rebuild(nil)
	// 素材：1、2 号商品图，3 号模特图，4 号 10 秒参考视频。
	env.assets.put(env.user, 1, "image", "image/jpeg", 0)
	env.assets.put(env.user, 2, "image", "image/jpeg", 0)
	env.assets.put(env.user, 3, "image", "image/jpeg", 0)
	env.assets.put(env.user, 4, "video", "video/mp4", 10000)
	for id, it := range env.assets.items {
		row := *it.asset
		row.ID = id
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("写入素材行失败：%v", err)
		}
	}
	env.replyAll()
	return env
}

// emptyBindings 是没有任何绑定的绑定库：全部环节用内置方法。
type emptyBindings struct{}

func (emptyBindings) GetBinding(context.Context, string) (stepkit.Binding, bool, error) {
	return stepkit.Binding{}, false, nil
}

// ---- 脚本化的合法输出 ----

// shotSpans 把 duration 均分成 n 个首尾相接的时间段。
func shotSpans(duration float64, n int) [][2]float64 {
	out := make([][2]float64, n)
	for i := range n {
		out[i] = [2]float64{duration * float64(i) / float64(n), duration * float64(i+1) / float64(n)}
	}
	return out
}

func mustJSONText(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// factsReply 是合法的事实卡输出（卖点来自画面）。
func factsReply() string {
	return mustJSONText(map[string]any{"name": "晨雾香水 50ml", "category": "香水", "anchors": []string{"方形磨砂玻璃瓶身", "金色金属瓶盖"},
		"selling_points": []map[string]any{{"text": "木质调", "proof": "清晨到傍晚的时间跳切", "source": "image"}},
		"prohibited":     []string{"不编造价格、销量、评价、认证与功效"}, "audience": "通勤女性", "cta": "点击下方链接"})
}

// reverseReply 是 10 秒参考片的合法拆解：4 个镜头，品牌 HERALIFE，背书「销量第一」。
func reverseReply() string {
	var shots []map[string]any
	for i, sp := range shotSpans(10, 4) {
		shots = append(shots, map[string]any{"id": i + 1, "start": sp[0], "end": sp[1], "is_opening_3s": sp[0] < 3,
			"shot_size": "特写", "camera": "固定", "subject": "手", "action": "右手拧开瓶盖，食指按下喷头，雾气散开", "scene": "浴室台面",
			"lighting": "柔光", "person": "", "product_in_frame": "商品居中占三分之一", "product_role": "dynamic", "dialogue": "", "onscreen_text": "", "key_colors": []string{"白色软管"}})
	}
	return mustJSONText(map[string]any{"overall": map[string]any{"style": "生活流", "narrative_arc": "痛点到解决", "why_viral": "节奏快", "transcript": "", "category": "护手霜"},
		"shots": shots, "brands": []string{"HERALIFE"}, "claims": []string{"销量第一"}})
}

// followReply 是合法的跟拍脚本：4 个镜头对应拆解的 4 个镜头；narration 可注入问题文案。
func followReply(duration float64, narration string) string {
	var shots []map[string]any
	var fm []map[string]any
	for i, sp := range shotSpans(duration, 4) {
		shots = append(shots, map[string]any{"start": sp[0], "end": sp[1], "purpose": "hook", "shot": "特写", "camera": "固定", "scene": "右手拧开瓶盖，食指按下喷头", "narration": narration})
		fm = append(fm, map[string]any{"ref_shot": i + 1, "shot": i})
	}
	return mustJSONText(map[string]any{"title": "跟拍 · 一抹就润", "hook": "手干？", "shots": shots, "music": "轻快", "voice": "女声", "follow_map": fm})
}

// createReply 是合法的创意脚本：按给定角度各一份，每份 3 个镜头。
func createReply(duration float64, angles ...string) string {
	var scripts []map[string]any
	for _, a := range angles {
		var shots []map[string]any
		for _, sp := range shotSpans(duration, 3) {
			shots = append(shots, map[string]any{"start": sp[0], "end": sp[1], "purpose": "hook", "shot": "中景", "camera": "推近", "scene": "手腕喷香水", "narration": "早上喷一下"})
		}
		scripts = append(scripts, map[string]any{"angle": a, "title": "创意 " + a, "hook": "钩子", "shots": shots, "music": "轻快", "voice": "女声"})
	}
	return mustJSONText(map[string]any{"scripts": scripts})
}

// replyAll 给四个环节挂上默认的合法回复（无参考：15 秒；有参考：10 秒，改编角度 pain）。
func (e *mktEnv) replyAll() {
	e.caller.on(marketing.StepFacts, func(int) (string, error) { return factsReply(), nil })
	e.caller.on(marketing.StepReverse, func(int) (string, error) { return reverseReply(), nil })
	e.caller.on(marketing.StepFollow, func(int) (string, error) { return followReply(10, "一抹就润"), nil })
	e.caller.on(marketing.StepCreate, func(int) (string, error) { return createReply(15, "pain", "lifestyle"), nil })
}

// settings 是默认轮次设置：抖音、中文、角度 pain + lifestyle、15 秒。
func mktSettings() model.MarketingSettings {
	return model.MarketingSettings{Platform: "douyin", Language: "zh", Angles: []string{"pain", "lifestyle"}, VideoModel: mktVideoKey,
		Params: map[string]any{"duration": float64(15), "aspect_ratio": "9:16"}, Description: "木质调，留香 8 小时"}
}

// refSettings 是带参考片的设置：10 秒，改编角度 pain 一份。
func mktRefSettings() model.MarketingSettings {
	st := mktSettings()
	st.Angles = []string{"pain"}
	st.Params = map[string]any{"duration": float64(10), "aspect_ratio": "9:16"}
	return st
}

func idp(v uint64) *uint64 { return &v }

// createProject 建项目（无参考）。
func (e *mktEnv) createProject(st model.MarketingSettings) *model.MarketingProjectView {
	e.t.Helper()
	v, err := e.svc.CreateProject(context.Background(), e.user, &model.CreateMarketingProjectReq{ProductAssetIDs: []uint64{1, 2}, Settings: st})
	if err != nil {
		e.t.Fatalf("建项目失败：%v", err)
	}
	return v
}

// createRefProject 建带参考视频和模特图的项目。
func (e *mktEnv) createRefProject() *model.MarketingProjectView {
	e.t.Helper()
	v, err := e.svc.CreateProject(context.Background(), e.user, &model.CreateMarketingProjectReq{ProductAssetIDs: []uint64{1}, ModelAssetID: idp(3), RefVideoAssetID: idp(4), Settings: mktRefSettings()})
	if err != nil {
		e.t.Fatalf("建项目失败：%v", err)
	}
	return v
}

// runRound 同步执行轮次的写脚本 job。
func (e *mktEnv) runRound(round *model.MarketingRoundView) {
	e.t.Helper()
	if err := e.svc.HandleMarketingRound(context.Background(), uint64(round.ID)); err != nil {
		e.t.Fatalf("执行写脚本失败：%v", err)
	}
}

// project 读项目详情。
func (e *mktEnv) project(id idcodec.ID) *model.MarketingProjectView {
	e.t.Helper()
	v, err := e.svc.GetProject(context.Background(), e.user, uint64(id))
	if err != nil {
		e.t.Fatalf("读项目失败：%v", err)
	}
	return v
}

// round 读项目里指定轮次的最新视图。
func (e *mktEnv) round(pid idcodec.ID, rid idcodec.ID) *model.MarketingRoundView {
	e.t.Helper()
	for _, r := range e.project(pid).Rounds {
		if r.ID == rid {
			rr := r
			return &rr
		}
	}
	e.t.Fatalf("没有找到轮次 %v", rid)
	return nil
}

// step 取轮次里某环节的最新运行视图。
func stepOf(r *model.MarketingRoundView, name string) *model.MarketingStepRunView {
	for i := range r.Steps {
		if r.Steps[i].Step == name {
			return &r.Steps[i]
		}
	}
	return nil
}

// credit 读账户。
func (e *mktEnv) credit() model.UserCredit {
	e.t.Helper()
	var c model.UserCredit
	if err := e.db.Where("user_id = ?", e.user).Take(&c).Error; err != nil {
		e.t.Fatalf("读账户失败：%v", err)
	}
	return c
}

// wantCode 断言错误是指定业务码。
func wantMktCode(t *testing.T, err error, code int) {
	t.Helper()
	assertTaskCode(t, err, code)
}

// rebuild 用当前依赖重新组装营销服务（换截帧能力时用），并重新挂上任务观察者。
func (e *mktEnv) rebuild(frames MarketingFrames) {
	e.svc = NewMarketingService(MarketingDeps{Repo: e.repo, Steps: e.reg, Resolver: e.resolver, Caller: e.caller, Models: e.models, Assets: e.assets,
		Tasks: e.taskSvc, Canvases: e.canvases, Frames: frames, Credits: stepfake.Policy{Initial: e.initial}, Broadcaster: e.bc})
	e.taskSvc.SetTaskObserver(e.svc)
}
