package service

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/drama"
	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// DramaRepo 短剧查询和事务边界。
type DramaRepo interface {
	// WithTx 执行项目、任务和积分的原子变更。
	WithTx(context.Context, func(repository.DramaTx) error) error
	// List 返回当前用户分页项目。
	List(context.Context, uint64, string, int, int) ([]model.DramaProject, int64, error)
	// Project 按用户查询项目。
	Project(context.Context, uint64, uint64, bool) (*model.DramaProject, error)
	// Episodes 按集号读取项目剧本。
	Episodes(context.Context, uint64, uint64) ([]model.DramaEpisode, error)
	// Jobs 返回项目全部或活动任务。
	Jobs(context.Context, uint64, uint64, bool) ([]model.DramaJob, error)
	// Job 按用户项目读取任务。
	Job(context.Context, uint64, uint64, uint64, bool) (*model.DramaJob, error)
}

// DramaCanvasWriter 写入拆解资产和提示词，画布与锁集由其原子执行。
type DramaCanvasWriter interface {
	// ApplyBreakdown 合并资产、创建角色卡，并锁定本次集。
	ApplyBreakdown(context.Context, uint64, uint64, uint64, uint64, []int, []drama.Asset) ([]DramaCanvasAsset, error)
	// ApplyPrompts 更新本次新建节点的提示词。
	ApplyPrompts(context.Context, uint64, uint64, uint64, []drama.AssetPrompt) error
}

// DramaCreditPolicy 提供与现有生成任务一致的新账户初始积分。
type DramaCreditPolicy interface {
	// InitialCredits 返回后台当前生效的新账户初始积分。
	InitialCredits(context.Context) (int, error)
}

// DramaStoryboardCanvasReader 提供按用户隔离的分镜资产画布。
type DramaStoryboardCanvasReader interface {
	// GetByID 按用户和画布ID读取当前资产节点。
	GetByID(context.Context, uint64, uint64) (*model.CanvasProject, error)
}

// DramaSegmentSummary 提供片段工作区的实时聚合。
type DramaSegmentSummary interface {
	// Summary 返回集的片段数、总秒数和已有采用视频的片段数。
	Summary(context.Context, uint64, uint64) (count, seconds, done int, err error)
}

// DramaModelStreamer 向 Agent 模型的渠道发一次对话请求（*llmgateway.Gateway 实现它）。
type DramaModelStreamer interface {
	Stream(ctx context.Context, tg llmgateway.Target, req llmgateway.Request, emit func(llmgateway.Event)) (*llmgateway.Result, error)
}

// DramaDeps 短剧服务依赖，文本步骤用后台的 Agent 模型：Registry 选模型，Streamer 经 LLM 网关请求渠道，Secrets 取渠道 Key。
type DramaDeps struct {
	Repo         DramaRepo
	Registry     provider.Registry
	Streamer     DramaModelStreamer
	Secrets      provider.SecretResolver
	Canvas       DramaCanvasWriter
	Broadcaster  ws.Broadcaster
	Credits      DramaCreditPolicy
	CanvasAssets DramaStoryboardCanvasReader
	Segments     DramaSegmentSummary
	Compose      DramaVideoComposer
	Assets       DramaComposeAssets
}

// DramaService 短剧项目和固定流水线的业务入口。
type DramaService struct {
	repo         DramaRepo
	registry     provider.Registry
	streamer     DramaModelStreamer
	secrets      provider.SecretResolver
	canvas       DramaCanvasWriter
	bc           ws.Broadcaster
	credits      DramaCreditPolicy
	canvasAssets DramaStoryboardCanvasReader
	segments     DramaSegmentSummary
	compose      DramaVideoComposer
	assets       DramaComposeAssets
}

// NewDramaService 创建短剧服务，不启动后台任务。
func NewDramaService(d DramaDeps) *DramaService {
	return &DramaService{repo: d.Repo, registry: d.Registry, streamer: d.Streamer, secrets: d.Secrets, canvas: d.Canvas, bc: d.Broadcaster, credits: d.Credits, canvasAssets: d.CanvasAssets, segments: d.Segments, compose: d.Compose, assets: d.Assets}
}

// Create 校验原文和风格，在同一事务建立项目、画布与首个研读任务。
func (s *DramaService) Create(ctx context.Context, userID uint64, req *model.CreateDramaReq) (*model.DramaView, error) {
	// 1. 文件和粘贴文本只能选一个，提取失败转换为契约错误。
	if req.Filename != "" && strings.TrimSpace(req.Text) != "" {
		return nil, errcode.ErrDramaSourceInvalid.WithMsg("文件和文本只能选一个")
	}
	filename, data := req.Filename, req.Data
	if filename == "" {
		filename = "source.txt"
		data = []byte(req.Text)
	}
	source, err := drama.ParseSource(filename, data)
	if err != nil {
		if errors.Is(err, drama.ErrSourceTooLong) {
			return nil, errcode.ErrDramaSourceTooLong
		}
		return nil, errcode.ErrDramaSourceInvalid.WithMsg(err.Error())
	}
	if err := checkDramaStyle(req.StyleKey, req.StyleCustom); err != nil {
		return nil, err
	}
	// 2. 冻结已启用的 Agent 模型；项目创建时即可发现没有可用模型。
	snap, err := s.resolveModel(ctx, "")
	if err != nil {
		return nil, err
	}
	initial, err := s.dramaInitialCredits(ctx)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(req.Title)
	if title == "" && req.Filename != "" {
		base := filepath.Base(req.Filename)
		title = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if title == "" {
		title = "未命名短剧"
	}
	seconds := req.EpisodeSeconds
	if seconds == 0 {
		seconds = 60
	}
	p := &model.DramaProject{UserID: userID, Title: title, SourceText: source, Genre: req.Genre, StyleKey: req.StyleKey, StyleCustom: req.StyleCustom, Ratio: req.Ratio, TargetEpisodes: req.TargetEpisodes, EpisodeSeconds: seconds, Status: "analyzing", Revision: 1, TextModelID: &snap.ModelRevisionID}
	var j *model.DramaJob
	// 3. 项目、画布、积分冻结与River入队一起提交。
	err = s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		if err := tx.InsertProject(ctx, p); err != nil {
			return err
		}
		var e error
		j, e = s.enqueue(ctx, tx, p, &model.DramaJobReq{Step: "analyze"}, snap, nil, initial, nil)
		return e
	})
	if err != nil {
		return nil, dramaError(err)
	}
	s.publish(ctx, j)
	return s.Get(ctx, userID, p.ID)
}

// Get 读取项目详情，其他用户资源统一视为不存在。
func (s *DramaService) Get(ctx context.Context, userID, id uint64) (*model.DramaView, error) {
	// 1. 首先校验归属，再读取集和任务，防止跨用户泄露。
	p, err := s.repo.Project(ctx, userID, id, false)
	if err != nil {
		return nil, dramaError(err)
	}
	eps, err := s.repo.Episodes(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	jobs, err := s.repo.Jobs(ctx, userID, id, true)
	if err != nil {
		return nil, err
	}
	v := &model.DramaView{ID: idcodec.ID(p.ID), Title: p.Title, CanvasID: idcodec.ID(p.CanvasID), Status: p.Status, SourceKind: p.SourceKind, Genre: p.Genre, StyleKey: p.StyleKey, StyleCustom: p.StyleCustom, Ratio: p.Ratio, TargetEpisodes: p.TargetEpisodes, EpisodeSeconds: p.EpisodeSeconds, TextModel: p.TextModel, OutlineMD: p.OutlineMD, CharactersMD: p.CharactersMD, Revision: p.Revision, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, Episodes: []model.DramaEpisodeSummary{}, ActiveJobs: []model.DramaJobView{}}
	for _, e := range eps {
		summary, err := s.dramaEpisodeView(ctx, e)
		if err != nil {
			return nil, err
		}
		v.Episodes = append(v.Episodes, summary.DramaEpisodeSummary)
	}
	for _, j := range jobs {
		v.ActiveJobs = append(v.ActiveJobs, jobView(j))
	}
	return v, nil
}

// List 返回用户项目及集数，页大小限制与其他列表一致。
func (s *DramaService) List(ctx context.Context, userID uint64, keyword string, page, size int) ([]model.DramaSummary, int64, error) {
	// 1. 统一分页边界，避免无限制读取。
	page = max(page, 1)
	if size <= 0 {
		size = 20
	}
	size = min(size, 100)
	rows, n, err := s.repo.List(ctx, userID, strings.TrimSpace(keyword), page, size)
	if err != nil {
		return nil, 0, err
	}
	out := make([]model.DramaSummary, 0, len(rows))
	for _, p := range rows {
		eps, e := s.repo.Episodes(ctx, userID, p.ID)
		if e != nil {
			return nil, 0, e
		}
		v := model.DramaSummary{ID: idcodec.ID(p.ID), Title: p.Title, Status: p.Status, EpisodeCount: len(eps), UpdatedAt: p.UpdatedAt}
		for _, ep := range eps {
			if ep.LockedAt != nil {
				v.LockedCount++
			}
		}
		out = append(out, v)
	}
	return out, n, nil
}

// Update 带版本更新项目，不改变已锁定集正文。
func (s *DramaService) Update(ctx context.Context, userID, id uint64, req *model.UpdateDramaReq) (*model.DramaView, error) {
	// 1. 事务中锁项目并执行revision比较；PATCH遗漏字段保持原值。
	err := s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		p, err := tx.Project(ctx, userID, id, true)
		if err != nil {
			return err
		}
		if p.Revision != req.Revision {
			return errcode.ErrCanvasConflict
		}
		fields, err := dramaProjectPatch(p, req)
		if err != nil {
			return err
		}
		return tx.UpdateProject(ctx, userID, id, req.Revision, fields)
	})
	if err != nil {
		return nil, dramaError(err)
	}
	return s.Get(ctx, userID, id)
}

// dramaProjectPatch 仅应用请求中出现的字段，风格始终按最终组合校验。
func dramaProjectPatch(p *model.DramaProject, req *model.UpdateDramaReq) (map[string]any, error) {
	fields := map[string]any{}
	for key, value := range map[string]*string{"title": req.Title, "genre": req.Genre, "style_key": req.StyleKey, "style_custom": req.StyleCustom, "ratio": req.Ratio, "text_model": req.TextModel, "outline_md": req.OutlineMD, "characters_md": req.CharactersMD} {
		if value != nil {
			fields[key] = *value
		}
	}
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			return nil, errcode.ErrInvalidParams.WithMsg("标题不能为空")
		}
		fields["title"] = title
	}
	if req.StyleKey != nil {
		p.StyleKey = *req.StyleKey
	}
	if req.StyleCustom != nil {
		p.StyleCustom = *req.StyleCustom
	}
	if err := checkDramaStyle(p.StyleKey, p.StyleCustom); err != nil {
		return nil, err
	}
	return fields, nil
}

// Delete 取消活动任务并软删项目、短剧画布与角色卡。
func (s *DramaService) Delete(ctx context.Context, userID, id uint64) error {
	// 1. 锁项目和任务，退款与软删除同事务完成。
	return dramaError(s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		if _, e := tx.Project(ctx, userID, id, true); e != nil {
			return e
		}
		jobs, e := tx.Jobs(ctx, userID, id, true)
		if e != nil {
			return e
		}
		for i := range jobs {
			if e := s.finishTx(ctx, tx, &jobs[i], "canceled", 0, "", 0, nil); e != nil {
				return e
			}
		}
		return tx.DeleteProject(ctx, userID, id)
	}))
}

// checkDramaStyle 验证内置或自定义画风，拒绝无法应用的风格。
func checkDramaStyle(key, custom string) error {
	if key == "custom" {
		if strings.TrimSpace(custom) == "" || utf8.RuneCountInString(custom) > 500 {
			return errcode.ErrInvalidParams.WithMsg("自定义画风须为1至500字")
		}
		return nil
	}
	for _, style := range drama.Styles().Styles {
		if style.Key == key {
			return nil
		}
	}
	return errcode.ErrInvalidParams.WithMsg("画风不存在")
}

// episodeView 映射集状态，正文只在单集接口返回。
func episodeView(e model.DramaEpisode) model.DramaEpisodeView {
	status := e.StoryboardStatus
	if status == "" {
		status = "empty"
	}
	return model.DramaEpisodeView{DramaEpisodeSummary: model.DramaEpisodeSummary{No: e.No, Title: e.Title, Synopsis: e.Synopsis, ScriptStatus: e.ScriptStatus, StoryboardStatus: status, Locked: e.LockedAt != nil, Revision: e.Revision, UpdatedAt: e.UpdatedAt}, ScriptMD: e.ScriptMD}
}

// dramaEpisodeView 在基础状态上映射实时片段聚合，避免持久化重复计数。
func (s *DramaService) dramaEpisodeView(ctx context.Context, ep model.DramaEpisode) (*model.DramaEpisodeView, error) {
	v := episodeView(ep)
	if s.segments != nil {
		var err error
		v.SegmentCount, v.SegmentSeconds, v.DoneSegmentCount, err = s.segments.Summary(ctx, ep.UserID, ep.ID)
		if err != nil {
			return nil, err
		}
	}
	if ep.FinalAssetID != nil && s.assets != nil {
		var err error
		v.Final, err = s.assets.View(ctx, ep.UserID, *ep.FinalAssetID)
		if err != nil {
			return nil, err
		}
		v.FinalStale = ep.SegmentVersion != ep.FinalSegmentVersion
	}
	return &v, nil
}

// jobView 映射任务状态并保证空范围为[]。
func jobView(j model.DramaJob) model.DramaJobView {
	v := model.DramaJobView{ID: idcodec.ID(j.ID), Step: j.Step, Episodes: append([]int{}, j.Episodes...), Status: j.Status, Progress: j.Progress, Credits: j.Credits, ErrorCode: j.ErrorCode, ErrorMessage: j.ErrorMessage, CreatedAt: j.CreatedAt, FinishedAt: j.FinishedAt}
	if j.Step == "compose" && j.Status == "succeeded" {
		var result model.DramaComposeResult
		if json.Unmarshal(j.Output, &result) == nil && result.Asset.ID != 0 {
			v.Result = &result
		}
	}
	return v
}

// dramaError 把仓储哨兵转换为公开业务码。
func dramaError(e error) error {
	if errors.Is(e, repository.ErrNotFound) {
		return errcode.ErrDramaNotFound
	}
	if errors.Is(e, repository.ErrStateConflict) {
		return errcode.ErrCanvasConflict
	}
	if errors.Is(e, repository.ErrDuplicate) {
		return errcode.ErrDramaJobBusy
	}
	return e
}

// dramaActive 判断任务是否允许继续写入。
func dramaActive(status string) bool { return status == "queued" || status == "running" }

// publish 向用户频道推送任务状态，采用现有WebSocket广播契约。
func (s *DramaService) publish(ctx context.Context, j *model.DramaJob) {
	if s.bc != nil && j != nil {
		s.bc.Publish(ctx, ws.UserChannel(j.UserID), ws.Message{Type: ws.TypeDramaJob, Data: struct {
			DramaID idcodec.ID         `json:"drama_id"`
			Job     model.DramaJobView `json:"job"`
		}{idcodec.ID(j.DramaID), jobView(*j)}})
	}
}

// selectedEpisodes 按任务持久化范围选择集。
func selectedEpisodes(eps []model.DramaEpisode, scope []int) []model.DramaEpisode {
	out := []model.DramaEpisode{}
	for _, ep := range eps {
		if slices.Contains(scope, ep.No) {
			out = append(out, ep)
		}
	}
	return out
}

// dramaFailure 将未知上游错误转换为安全错误文案。
func dramaFailure(err error) (int, string) {
	var e *errcode.Error
	if errors.As(err, &e) {
		return e.Code, e.Msg
	}
	return errcode.ErrInternal.Code, errcode.ErrInternal.Msg
}

// dramaInitialCredits 只在业务事务外读取后台策略。
func (s *DramaService) dramaInitialCredits(ctx context.Context) (int, error) {
	if s.credits != nil {
		return s.credits.InitialCredits(ctx)
	}
	return DefaultInitialCredits, nil
}
