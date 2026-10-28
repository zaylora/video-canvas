package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"

	"video-canvas/internal/config"
	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/migration"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/queue"
	"video-canvas/internal/provider/worker"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

type registry struct{}
type creditPolicy struct{}

func (creditPolicy) InitialCredits(context.Context) (int, error) { return 100, nil }
func (registry) ListModels(context.Context, string) ([]provider.ModelInfo, error) {
	return []provider.ModelInfo{{Key: "smoke-text", Kind: "text"}}, nil
}
func (registry) Snapshot(_ context.Context, key string) (*provider.Snapshot, error) {
	snap := &provider.Snapshot{ModelRevisionID: 1, Model: provider.ModelSnapshot{Key: key, Kind: "text", Pricing: modelcfg.Pricing{Billing: modelcfg.BillingPerCall, Unit: 1}, Capabilities: modelcfg.Capabilities{Prompt: modelcfg.PromptSpec{MaxLength: 100000}, Context: &modelcfg.ContextSpec{Window: 128000, Output: 4000}}}}
	if key == "smoke-video" || key == "smoke-image" {
		snap.Model.Kind = strings.TrimPrefix(key, "smoke-")
		snap.Model.Capabilities.Context = nil
		snap.Model.Capabilities.Ops = []string{modelcfg.OpT2V}
		if snap.Model.Kind == model.KindImage {
			snap.Model.Capabilities.Ops = []string{modelcfg.OpT2I}
		}
	}
	return snap, nil
}

type executor struct {
	provider.Executor
	db *gorm.DB
}

func (e executor) Submit(ctx context.Context, snap *provider.Snapshot, in provider.SubmitInput) (*provider.SubmitResult, error) {
	if snap.Model.Kind != model.KindText {
		return smokeMediaOutput(ctx, e.db, snap.Model.Kind, in.Task)
	}
	prompt := in.Input["prompt"].(string)
	start := strings.Index(prompt, "```json\n") + len("```json\n")
	end := strings.Index(prompt[start:], "\n```") + start
	var vars map[string]json.RawMessage
	must(json.Unmarshal([]byte(prompt[start:end]), &vars))
	output := `{"source_kind":"script","summary":"母亲回家并找到玉佩。","characters_md":"母亲：短发中年女性。"}`
	switch {
	case strings.Contains(prompt, `"required":["segments"]`):
		output = smokeStoryboardOutput(vars)
	case strings.Contains(prompt, `"required": ["prompts"]`):
		var assets []struct {
			NodeID string `json:"node_id"`
		}
		must(json.Unmarshal(vars["assets"], &assets))
		prompts := []map[string]string{}
		for _, a := range assets {
			prompts = append(prompts, map[string]string{"node_id": a.NodeID, "prompt": "统一电影质感，清晰完整参考构图"})
		}
		raw, e := json.Marshal(map[string]any{"prompts": prompts})
		must(e)
		output = string(raw)
	case strings.Contains(prompt, `"required": ["assets"]`):
		var eps []struct{ No int }
		must(json.Unmarshal(vars["episodes"], &eps))
		if len(eps) != 1 {
			panic("smoke范围错误")
		}
		raw, e := json.Marshal(map[string]any{"assets": []map[string]any{
			{"type": "character", "name": "母亲", "look": "", "aliases": []string{"妈妈"}, "episodes": []int{eps[0].No}, "description": "短发中年女性，白衬衫", "voice_prompt": "温柔低声"},
			{"type": "scene", "name": "老宅", "look": "", "aliases": []string{}, "episodes": []int{eps[0].No}, "description": "木窗与旧桌的客厅", "voice_prompt": ""},
			{"type": "prop", "name": "玉佩", "look": "", "aliases": []string{}, "episodes": []int{eps[0].No}, "description": "青色双鱼玉佩", "voice_prompt": ""},
		}})
		must(e)
		output = string(raw)
	}
	return &provider.SubmitResult{Immediate: &provider.QueryResult{Status: provider.StatusSucceeded, Outputs: []provider.Output{{Type: provider.OutputText, Text: output}}}}, nil
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
func require(ok bool, msg string) {
	if !ok {
		panic(msg)
	}
}

var client = &http.Client{Timeout: 10 * time.Second}

func request(base, method, path string, body any) (int, map[string]any) {
	var data bytes.Buffer
	if body != nil {
		must(json.NewEncoder(&data).Encode(body))
	}
	req, e := http.NewRequestWithContext(context.Background(), method, base+path, &data)
	must(e)
	req.Header.Set("Content-Type", "application/json")
	return send(req)
}
func send(req *http.Request) (int, map[string]any) {
	resp, e := client.Do(req)
	must(e)
	defer resp.Body.Close()
	var body map[string]any
	if resp.StatusCode != 204 {
		must(json.NewDecoder(resp.Body).Decode(&body))
	}
	return resp.StatusCode, body
}
func data(body map[string]any) map[string]any { return body["data"].(map[string]any) }
func awaitReady(base, id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, b := request(base, "GET", "/dramas/"+id, nil)
		require(status == 200, fmt.Sprint(b))
		v := data(b)
		if v["status"] == "failed" {
			_, jobs := request(base, "GET", "/dramas/"+id+"/jobs", nil)
			panic(fmt.Sprint(jobs))
		}
		if len(v["active_jobs"].([]any)) == 0 {
			return
		}
		select {
		case <-ctx.Done():
			panic("等待流水线超时")
		case <-ticker.C:
		}
	}
}

// env 汇集验收所需的真实依赖：隔离 schema 的数据库、River 队列与 HTTP 服务。
type env struct {
	db         *gorm.DB
	canvasRepo *repository.CanvasProjectRepository
	url        string
	events     *smokeEvents
}

// openIsolatedDB 在专用测试库建隔离 schema 并迁移，返回连接与清理函数。
func openIsolatedDB(ctx context.Context, dsn string) (*gorm.DB, *sql.DB, string, func()) {
	cfg := &gorm.Config{Logger: gormlog.Default.LogMode(gormlog.Silent)}
	admin, e := gorm.Open(postgres.Open(dsn), cfg)
	must(e)
	schema := fmt.Sprintf("drama_smoke_%d", time.Now().UnixNano())
	must(admin.Exec("CREATE SCHEMA " + schema).Error)
	db, e := gorm.Open(postgres.Open(dsn+" search_path="+schema), cfg)
	must(e)
	sqlDB, e := db.DB()
	must(e)
	sqlDB.SetMaxOpenConns(30)
	must(migration.Run(ctx, db, migration.Options{Logger: zap.NewNop()}))
	must(queue.Migrate(ctx, sqlDB, schema))
	return db, sqlDB, schema, func() {
		must(sqlDB.Close())
		must(admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error)
		pool, _ := admin.DB()
		must(pool.Close())
	}
}

// startServer 装配服务、River 运行时与路由，返回 HTTP 地址和停止函数。
func startServer(ctx context.Context, db *gorm.DB, sqlDB *sql.DB, schema string, events *smokeEvents) (string, func()) {
	repo := repository.NewDramaRepository(db)
	canvases := repository.NewCanvasProjectRepository(db)
	assets := service.NewAssetService(repository.NewAssetRepository(db), nil, config.Storage{})
	exec := executor{db: db}
	tasksRepo := repository.NewGenerationTaskRepository(db)
	tasks := service.NewGenerationTaskService(service.GenerationTaskDeps{Repo: tasksRepo, Registry: registry{}, Executor: exec, Assets: assets, Broadcaster: events})
	segments := service.NewDramaSegmentService(service.DramaSegmentDeps{Repo: repo, Tasks: tasks, Assets: assets, Broadcaster: events})
	tasks.SetTaskObserver(segments)
	svc := service.NewDramaService(service.DramaDeps{Repo: repo, Registry: registry{}, Executor: exec, Canvas: service.NewDramaCanvasService(repository.NewDramaCanvasRepository(db), nil), Credits: creditPolicy{}, Segments: segments, CanvasAssets: canvases, Broadcaster: events})
	rt, e := queue.New(queue.Config{DB: sqlDB, Schema: schema, Repo: repository.NewQueueRepository(db), Drama: svc, Store: tasks, Handler: worker.New(tasks, exec, nil, worker.Options{Logger: zap.NewNop()}), Enabled: true, HardCap: 2, FetchPollInterval: 100 * time.Millisecond, Logger: zap.NewNop()})
	must(e)
	repo.SetEnqueuer(rt)
	tasksRepo.SetEnqueuer(rt)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- rt.Run(runCtx) }()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery(), func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(1)); c.Next() })
	h := handler.NewDramaHandler(svc)
	r.POST("/dramas", h.Create)
	r.GET("/dramas/:id", h.Get)
	r.DELETE("/dramas/:id", h.Delete)
	r.GET("/dramas/:id/episodes/:no", h.GetEpisode)
	r.PATCH("/dramas/:id/episodes/:no", h.UpdateEpisode)
	r.POST("/dramas/:id/jobs", h.CreateJob)
	r.GET("/dramas/:id/jobs", h.ListJobs)
	registerSegmentRoutes(r, segments)
	srv := httptest.NewServer(r)
	return srv.URL, func() { srv.Close(); cancel(); must(<-done) }
}

// createDrama 用带分集标记的原文创建短剧，返回短剧与画布 ID。
func createDrama(ctx context.Context, base string) (string, string) {
	var form bytes.Buffer
	fw := multipart.NewWriter(&form)
	must(fw.WriteField("text", "第1集 回家\n母亲走进老宅，拿起玉佩。\n第2集 真相\n母亲再次走进老宅，放下玉佩。"))
	must(fw.WriteField("title", "运行验收短剧"))
	must(fw.WriteField("style_key", "real_city_film"))
	must(fw.WriteField("ratio", "9:16"))
	must(fw.Close())
	req, e := http.NewRequestWithContext(ctx, "POST", base+"/dramas", &form)
	must(e)
	req.Header.Set("Content-Type", fw.FormDataContentType())
	status, b := send(req)
	require(status == 201, fmt.Sprint(status, b))
	return data(b)["id"].(string), data(b)["canvas_id"].(string)
}

// breakdownEachEpisode 先验证集修订冲突，再逐集拆解并等待自动接续的提示词步骤。
func breakdownEachEpisode(base, id string) {
	status, b := request(base, "GET", "/dramas/"+id+"/episodes/1", nil)
	require(status == 200 && strings.Contains(data(b)["script_md"].(string), "母亲"), fmt.Sprint(b))
	revision := data(b)["revision"]
	status, b = request(base, "PATCH", "/dramas/"+id+"/episodes/1", map[string]any{"revision": 999, "title": "冲突"})
	require(status == 409 && b["code"].(float64) == 30002, fmt.Sprint(b))
	status, b = request(base, "PATCH", "/dramas/"+id+"/episodes/1", map[string]any{"revision": revision, "title": "回家新标题"})
	require(status == 200, fmt.Sprint(b))
	for _, no := range []int{1, 2} {
		status, b = request(base, "POST", "/dramas/"+id+"/jobs", map[string]any{"step": "breakdown", "episodes": []int{no}})
		require(status == 202, fmt.Sprint(status, b))
		awaitReady(base, id)
	}
}

// verifyCanvas 校验画布合并结果、角色卡可见性与画布列表过滤，返回角色卡 ID。
func verifyCanvas(ctx context.Context, e env, canvasID string) (uint64, uint64) {
	cvID, ok := idcodec.Decode(canvasID)
	require(ok, "canvas id编码错误")
	cv, err := e.canvasRepo.GetByID(ctx, 1, cvID)
	must(err)
	var payload struct {
		Nodes []struct{ Data map[string]any }
	}
	must(json.Unmarshal(cv.PayloadJSON, &payload))
	require(len(payload.Nodes) == 3, fmt.Sprintf("资产重复: %s", cv.PayloadJSON))
	var charID string
	for _, n := range payload.Nodes {
		dr := n.Data["drama"].(map[string]any)
		require(len(dr["episodes"].([]any)) == 2, fmt.Sprint(n))
		require(n.Data["params"].(map[string]any)["prompt"] != "", fmt.Sprint(n))
		if n.Data["kind"] == "character" {
			charID = n.Data["characterId"].(string)
		}
	}
	cards := repository.NewCharacterCardRepository(e.db)
	personal, err := cards.List(ctx, 1, "")
	must(err)
	require(len(personal) == 0, "短剧角色泄漏个人列表")
	cid, ok := idcodec.Decode(charID)
	require(ok, "角色ID编码错误")
	owned, err := cards.ListByIDs(ctx, 1, []uint64{cid})
	must(err)
	require(len(owned) == 1 && owned[0].AvatarAssetID == nil, "短剧角色无主图读取失败")
	ordinary, total, err := e.canvasRepo.List(ctx, 1, "", 0, 20)
	must(err)
	require(total == 0 && len(ordinary) == 0, "短剧画布过滤失败")
	return cvID, cid
}

// verifyBilling 校验锁定后的 67005 与积分流水守恒。
func verifyBilling(ctx context.Context, e env, id string) {
	_, b := request(e.url, "GET", "/dramas/"+id+"/episodes/1", nil)
	status, b := request(e.url, "PATCH", "/dramas/"+id+"/episodes/1", map[string]any{"revision": data(b)["revision"], "script_md": "不应保存"})
	require(status == 409 && b["code"].(float64) == 67005, fmt.Sprint(b))
	var credit model.UserCredit
	must(e.db.First(&credit, "user_id = ?", 1).Error)
	require(credit.Balance == 96 && credit.Frozen == 0, fmt.Sprintf("计费错误: %+v", credit))
	rec, err := repository.NewGenerationTaskRepository(e.db).Reconcile(ctx, 1)
	must(err)
	require(rec.FrozenDiff() == 0 && rec.BalanceDiff() == 0, fmt.Sprintf("流水不守恒: %+v", rec))
}

// verifyCascade 删除短剧后短剧、角色卡和画布都不可再读。
func verifyCascade(ctx context.Context, e env, id string, cvID, cid uint64) {
	status, b := request(e.url, "DELETE", "/dramas/"+id, nil)
	require(status == 204, fmt.Sprint(b))
	status, b = request(e.url, "GET", "/dramas/"+id, nil)
	require(status == 404, fmt.Sprint(b))
	owned, err := repository.NewCharacterCardRepository(e.db).ListByIDs(ctx, 1, []uint64{cid})
	must(err)
	require(len(owned) == 0, "删除短剧未删角色卡")
	_, err = e.canvasRepo.GetByID(ctx, 1, cvID)
	require(err != nil, "删除短剧未软删画布")
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	require(dsn != "", "需要专用测试库")
	db, sqlDB, schema, cleanup := openIsolatedDB(ctx, dsn)
	defer cleanup()
	idcodec.Init("drama-smoke-test-key")
	events := &smokeEvents{}
	url, stop := startServer(ctx, db, sqlDB, schema, events)
	defer stop()
	e := env{db: db, canvasRepo: repository.NewCanvasProjectRepository(db), url: url, events: events}
	id, canvasID := createDrama(ctx, url)
	awaitReady(url, id)
	breakdownEachEpisode(url, id)
	cvID, cid := verifyCanvas(ctx, e, canvasID)
	verifyBilling(ctx, e, id)
	verifyStoryboardAndGeneration(ctx, e, id, canvasID)
	verifyCascade(ctx, e, id, cvID, cid)
	_, _ = os.Stdout.WriteString("PASS: HTTP201→River analyze/outline→breakdown+asset_prompts→storyboard→segment CRUD/order/revision→keyframe+video worker→auto adoption+WS→67013→billing reconciliation→cascade204/404\n")
}
