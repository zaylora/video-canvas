package agent

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"video-canvas/internal/agent/skillpkg"
	"video-canvas/internal/agent/skills"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
	"video-canvas/internal/storage"
)

// SkillAuditWriter 是技能管理写审计的依赖，由 repository.AdminAuditRepository 实现（同后台运营审计）。
type SkillAuditWriter interface {
	// Insert 写一条审计日志（只增不改不删）。
	Insert(ctx context.Context, l *model.AdminAuditLog) error
}

// SkillOptions 是 SkillService 的可选项；零值取默认，测试用它注入时钟和 id。
type SkillOptions struct {
	Now          func() time.Time // 时钟，默认 time.Now
	NewID        func() string    // 生成 uuid，默认 uuid.NewString
	ImportTTL    time.Duration    // 导入暂存有效期，默认 1 小时
	PendingTTL   time.Duration    // pending 版本超过多久没转正就回收，默认 10 分钟
	CatalogTTL   time.Duration    // 目录缓存时长，默认 30 秒
	PkgCacheSize int              // 进程内缓存的整包个数，默认 8
}

// maxStagedZipBytes 是读回暂存包时的上限：规范化包不会超过上传限额，超出说明对象被替换过。
const maxStagedZipBytes = skillpkg.MaxZipBytes + 1

// SkillService 管理后台导入的 Agent 技能：导入（两步）、版本、启停、删除，以及给 Agent 运行时读取。
// 内置技能随二进制发布、只读展示，不经过这里的写路径。
type SkillService struct {
	repo   SkillRepo
	stores SkillStores
	audit  SkillAuditWriter
	opt    SkillOptions

	cacheMu   sync.Mutex
	catalog   []SkillBrief
	catalogAt time.Time
	pkgs      *pkgCache
}

// NewSkillService 创建技能服务；audit 为 nil 时不写审计。
func NewSkillService(repo SkillRepo, stores SkillStores, audit SkillAuditWriter, opt SkillOptions) *SkillService {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.NewID == nil {
		opt.NewID = uuid.NewString
	}
	if opt.ImportTTL <= 0 {
		opt.ImportTTL = time.Hour
	}
	if opt.PendingTTL <= 0 {
		opt.PendingTTL = 10 * time.Minute
	}
	if opt.CatalogTTL <= 0 {
		opt.CatalogTTL = 30 * time.Second
	}
	if opt.PkgCacheSize <= 0 {
		opt.PkgCacheSize = 8
	}
	return &SkillService{repo: repo, stores: stores, audit: audit, opt: opt, pkgs: newPkgCache(opt.PkgCacheSize)}
}

// SkillImportInput 是一次上传：要么是一个 zip，要么是文件夹 / 单个 SKILL.md 展开的文件列表。
type SkillImportInput struct {
	Zip   []byte
	Files []skillpkg.Input
}

// Import 上传并预检一个技能包：解压校验 → 叠加「与内置同名」「与已有版本同内容」两项检查 → 把规范化包写入暂存 → 返回预检结果。
// 预检不通过也是正常结果（返回 nil 错误），问题在 Issues 里，CanConfirm 为 false。
func (s *SkillService) Import(ctx context.Context, actorID uint64, in SkillImportInput) (*SkillImportView, error) {
	// 1. 解析：zip 与文件列表二选一；预检本身不返回 error，只有不可能发生的内部故障才会
	var res *skillpkg.Result
	var err error
	switch {
	case len(in.Zip) > 0:
		res, err = skillpkg.FromZip(in.Zip)
	case len(in.Files) > 0:
		res, err = skillpkg.FromFiles(in.Files)
	default:
		return nil, errcode.ErrInvalidParams.WithMsg("请上传 zip 压缩包、文件夹或 SKILL.md")
	}
	if err != nil {
		return nil, fmt.Errorf("技能包预检失败：%w", err)
	}

	// 2. 叠加需要内置库和数据库的检查，并算出确认之后会发生什么
	issues, plan, err := s.crossChecks(ctx, res)
	if err != nil {
		return nil, err
	}
	view := newImportView(res, issues, plan)

	// 3. 有规范化包就暂存（含有错误的包：界面还要预览它的文件）；先写行再写对象，
	//    这样对象写成功后一定有行能被过期清理找到，不会留下谁也找不到的孤儿对象
	if len(res.Package) > 0 {
		imp, err := s.stage(ctx, actorID, res, view)
		if err != nil {
			return nil, err
		}
		view.ID, view.ExpiresAt = imp.ID, &imp.ExpiresAt
	}
	return view, nil
}

// crossChecks 在包自身的预检结果之上，加两项需要查库的检查：与内置技能同名、与已有版本同内容；并给出导入计划。
// 名字都不合法时不查库（后面的检查没有意义）。
func (s *SkillService) crossChecks(ctx context.Context, res *skillpkg.Result) ([]SkillIssue, SkillImportPlan, error) {
	issues := make([]SkillIssue, 0, len(res.Issues)+2)
	for _, is := range res.Issues {
		si := SkillIssue{Level: is.Level, Code: is.Code, Path: is.Path, Message: is.Message}
		// 脚本执行暂不支持（第 2 期已搁置）：含脚本的技能导入后只能当文档读，用起来是残缺的，所以直接拦下
		if is.Code == skillpkg.CodeHasScripts {
			si.Level, si.Message = skillpkg.LevelError, "暂不支持含脚本的技能（当前不能执行脚本），请移除脚本文件后再导入"
		}
		issues = append(issues, si)
	}
	plan := SkillImportPlan{Action: "blocked"}
	if res.Name == "" || hasIssue(issues, skillpkg.CodeBadName) {
		return issues, plan, nil
	}
	// 内置技能同名不可覆盖：内置的优先，导入的同名技能永远读不到，所以直接拦下
	if _, ok := skills.Read(res.Name); ok {
		issues = append([]SkillIssue{{Level: skillpkg.LevelError, Code: "BUILTIN_NAME", Path: "SKILL.md", Message: fmt.Sprintf("「%s」是内置技能的名字，不能导入同名技能", res.Name)}}, issues...)
		return issues, plan, nil
	}
	sk, err := s.repo.GetSkill(ctx, res.Name)
	if errors.Is(err, repository.ErrNotFound) {
		plan = SkillImportPlan{Action: "create", Version: 1}
		return issues, withBlocked(issues, plan), nil
	}
	if err != nil {
		return nil, plan, err
	}
	plan = SkillImportPlan{Action: "new_version", Version: sk.LatestVersion + 1, ActiveVersion: sk.ActiveVersion, Enabled: sk.Enabled}
	if res.SHA256 != "" {
		same, err := s.repo.FindVersionBySHA(ctx, sk.ID, res.SHA256)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return nil, plan, err
		}
		if same != nil {
			issues = append([]SkillIssue{{Level: skillpkg.LevelError, Code: "SAME_AS_VERSION", Path: "SKILL.md", Message: fmt.Sprintf("内容与已有的 v%d 完全相同，不需要重复导入", same.Version)}}, issues...)
		}
	}
	return issues, withBlocked(issues, plan), nil
}

// withBlocked 有阻断错误时把计划标成 blocked。
func withBlocked(issues []SkillIssue, plan SkillImportPlan) SkillImportPlan {
	if hasErrorIssue(issues) {
		plan.Action = "blocked"
	}
	return plan
}

func hasIssue(issues []SkillIssue, code string) bool {
	for _, is := range issues {
		if is.Code == code {
			return true
		}
	}
	return false
}

func hasErrorIssue(issues []SkillIssue) bool {
	for _, is := range issues {
		if is.Level == skillpkg.LevelError {
			return true
		}
	}
	return false
}

// newImportView 把预检结果转成接口响应。
func newImportView(res *skillpkg.Result, issues []SkillIssue, plan SkillImportPlan) *SkillImportView {
	unsupported := res.Unsupported
	if unsupported == nil {
		unsupported = []string{}
	}
	fm := res.Frontmatter
	if fm == nil {
		fm = map[string]any{}
	}
	return &SkillImportView{
		Name: res.Name, Title: res.Title, Description: res.Description, Frontmatter: fm, Unsupported: unsupported,
		Files: fileViews(res.Files), HasScripts: res.HasScripts, TotalBytes: res.TotalBytes,
		Issues: issues, CanConfirm: !hasErrorIssue(issues), Plan: plan,
	}
}

func fileViews(files []skillpkg.File) []SkillFileView {
	out := make([]SkillFileView, len(files))
	for i, f := range files {
		out[i] = SkillFileView{Path: f.Path, Size: f.Size, SHA256: f.SHA256, Kind: f.Kind, Lang: f.Lang, Text: f.Text}
	}
	return out
}

// stagingKey 是暂存包的对象 key。
func stagingKey(id string) string { return "agent-skills/_staging/" + id + ".zip" }

// stage 登记并写入暂存：先写数据库行，再写对象；对象写失败就删掉行。
func (s *SkillService) stage(ctx context.Context, actorID uint64, res *skillpkg.Result, view *SkillImportView) (*model.AgentSkillImport, error) {
	h, err := s.stores.Default(ctx)
	if err != nil {
		return nil, fmt.Errorf("取默认存储失败：%w", err)
	}
	raw, err := json.Marshal(view)
	if err != nil {
		return nil, fmt.Errorf("序列化预检结果失败：%w", err)
	}
	now := s.opt.Now()
	imp := &model.AgentSkillImport{ID: s.opt.NewID(), UserID: actorID, ResultJSON: raw, SHA256: res.SHA256,
		StorageID: h.ID, PackageKey: "", ExpiresAt: now.Add(s.opt.ImportTTL), CreatedAt: now}
	imp.PackageKey = stagingKey(imp.ID)
	if err := s.repo.CreateImport(ctx, imp); err != nil {
		return nil, fmt.Errorf("登记导入暂存失败：%w", err)
	}
	if err := h.Storage.Put(ctx, imp.PackageKey, bytes.NewReader(res.Package), int64(len(res.Package)), "application/zip"); err != nil {
		if _, derr := s.repo.DeleteImport(ctx, imp.ID); derr != nil {
			logger.Warn("回退导入暂存行失败，等过期清理", zap.Error(derr), zap.String("import_id", imp.ID))
		}
		return nil, fmt.Errorf("写入暂存包失败：%w", err)
	}
	return imp, nil
}

// loadImport 取回属于 actorID 的、未过期的暂存；不存在、过期、不是本人的统一返回 61001（不泄露他人暂存是否存在）。
func (s *SkillService) loadImport(ctx context.Context, actorID uint64, id string) (*model.AgentSkillImport, error) {
	imp, err := s.repo.GetImport(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrSkillImportGone
	}
	if err != nil {
		return nil, err
	}
	if imp.UserID != actorID || !s.opt.Now().Before(imp.ExpiresAt) {
		return nil, errcode.ErrSkillImportGone
	}
	return imp, nil
}

// readObject 读出一个对象的全部字节，超过 limit 视为异常。
func (s *SkillService) readObject(ctx context.Context, storageID uint64, key string, limit int64) ([]byte, error) {
	h, err := s.stores.Get(ctx, storageID)
	if err != nil {
		return nil, fmt.Errorf("取存储 %d 失败：%w", storageID, err)
	}
	rc, err := h.Storage.Open(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("读取技能包失败：%w", err)
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, fmt.Errorf("读取技能包失败：%w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("技能包 %s 超过大小上限", key)
	}
	return data, nil
}

// ImportFile 读暂存包里的一个文件，给导入对话框预览用（确认之前文件还没有版本）。
func (s *SkillService) ImportFile(ctx context.Context, actorID uint64, id, path string) (*SkillFileContent, error) {
	// 1. 暂存必须是本人的、未过期的
	imp, err := s.loadImport(ctx, actorID, id)
	if err != nil {
		return nil, err
	}
	var view SkillImportView
	if err := json.Unmarshal(imp.ResultJSON, &view); err != nil {
		return nil, fmt.Errorf("解析暂存预检结果失败：%w", err)
	}
	// 2. 只读清单里有的路径，再从包里取
	pkg, err := s.readObject(ctx, imp.StorageID, imp.PackageKey, maxStagedZipBytes)
	if err != nil {
		return nil, err
	}
	return readPackageFile(pkg, view.Files, path)
}

// DiscardImport 放弃一个暂存：删行和对象。不存在、已过期或不是本人的都当作已经放弃（幂等）。
func (s *SkillService) DiscardImport(ctx context.Context, actorID uint64, id string) error {
	imp, err := s.repo.GetImport(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if imp.UserID != actorID {
		return nil
	}
	if _, err := s.repo.DeleteImport(ctx, id); err != nil {
		return err
	}
	s.deleteObject(ctx, imp.StorageID, imp.PackageKey)
	return nil
}

// ConfirmImport 确认入库：以暂存的包为准重新预检（防止暂存期间别人导入了同名/同内容）→ 预留版本号（pending）→
// 把包写到正式 key → 认领暂存行（只有一个并发确认能成功）→ 转为 ready → 写审计。
// 新技能默认停用；已有技能的新版本不会自动成为生效版本（用户要求先停用再启用，新版本等价于重新安装）。
func (s *SkillService) ConfirmImport(ctx context.Context, actorID uint64, id string) (*SkillItem, error) {
	// 1. 暂存必须是本人的、未过期的
	imp, err := s.loadImport(ctx, actorID, id)
	if err != nil {
		return nil, err
	}
	// 2. 读回暂存包并重新预检：不信任暂存行里的结果，包本身才是准绳
	pkg, err := s.readObject(ctx, imp.StorageID, imp.PackageKey, maxStagedZipBytes)
	if errors.Is(err, storage.ErrNotFound) {
		// 并发的赢家已认领暂存并删掉了对象，输家按"暂存已失效"处理
		return nil, errcode.ErrSkillImportGone
	}
	if err != nil {
		return nil, err
	}
	res, err := skillpkg.FromZip(pkg)
	if err != nil {
		return nil, fmt.Errorf("技能包预检失败：%w", err)
	}
	issues, _, err := s.crossChecks(ctx, res)
	if err != nil {
		return nil, err
	}
	if err := confirmBlocker(res, issues); err != nil {
		return nil, err
	}
	// 3. 预留版本号并写入正式对象；key 在预留时就定下，失败重试复用同一个，不用 sha256（公开桶下会被推测）
	h, err := s.stores.Default(ctx)
	if err != nil {
		return nil, fmt.Errorf("取默认存储失败：%w", err)
	}
	row, err := s.versionRow(res, actorID, h.ID)
	if err != nil {
		return nil, err
	}
	_, ver, err := s.repo.ReserveVersion(ctx, repository.ReserveSkillInput{Name: res.Name, Title: clipRunes(res.Title, 128), CreatedBy: actorID, Version: *row})
	if err != nil {
		return nil, fmt.Errorf("预留版本号失败：%w", err)
	}
	if err := h.Storage.Put(ctx, ver.PackageKey, bytes.NewReader(res.Package), int64(len(res.Package)), "application/zip"); err != nil {
		if derr := s.repo.DiscardVersion(ctx, ver.ID); derr != nil {
			logger.Warn("回退 pending 版本失败，等清理任务回收", zap.Error(derr), zap.Uint64("version_id", ver.ID))
		}
		return nil, fmt.Errorf("写入技能包失败：%w", err)
	}
	// 4. 认领暂存行：并发的第二次确认拿不到，回退自己预留的版本和对象
	claimed, err := s.repo.DeleteImport(ctx, imp.ID)
	if err != nil || !claimed {
		s.rollbackReserved(ctx, h.ID, ver)
		if err != nil {
			return nil, err
		}
		return nil, errcode.ErrSkillImportGone
	}
	// 5. 转为 ready；首个版本同时成为生效版本。失败时 pending 行连同对象由清理任务回收
	sk, err := s.repo.PromoteVersion(ctx, ver.ID)
	if err != nil {
		return nil, fmt.Errorf("版本转正失败：%w", err)
	}
	s.deleteObject(ctx, imp.StorageID, imp.PackageKey)
	s.invalidateCatalog()
	s.auditSkill(ctx, actorID, model.AdminAuditSkillImport, sk.ID, map[string]any{"name": sk.Name, "version": ver.Version, "sha256": res.SHA256})
	return s.itemFor(ctx, sk)
}

// confirmBlocker 把预检里的阻断错误翻译成接口错误码：内置同名 61003、同内容 61006，其余 61002（原因写进 Msg）。
func confirmBlocker(res *skillpkg.Result, issues []SkillIssue) error {
	var first *SkillIssue
	for i := range issues {
		if issues[i].Level != skillpkg.LevelError {
			continue
		}
		switch issues[i].Code {
		case "BUILTIN_NAME":
			return errcode.ErrSkillBuiltinName
		case "SAME_AS_VERSION":
			return errcode.ErrSkillSameContent.WithMsg(issues[i].Message)
		}
		if first == nil {
			first = &issues[i]
		}
	}
	if first != nil {
		return errcode.ErrSkillPrecheckFailed.WithMsg("预检未通过：" + first.Message)
	}
	if len(res.Package) == 0 {
		return errcode.ErrSkillPrecheckFailed
	}
	return nil
}

// versionRow 把预检结果装成待入库的版本行（不含 skill_id / version / state，由仓储预留时填）。
func (s *SkillService) versionRow(res *skillpkg.Result, actorID, storageID uint64) (*model.AgentSkillVersion, error) {
	fm, err := json.Marshal(res.Frontmatter)
	if err != nil {
		return nil, fmt.Errorf("序列化 frontmatter 失败：%w", err)
	}
	unsup := res.Unsupported
	if unsup == nil {
		unsup = []string{}
	}
	files, _ := json.Marshal(fileViews(res.Files)) // 全是字符串和数字，编码不会失败
	unsupJSON, _ := json.Marshal(unsup)
	var keep []SkillIssue
	for _, is := range res.Issues {
		if is.Level != skillpkg.LevelError {
			keep = append(keep, SkillIssue{Level: is.Level, Code: is.Code, Path: is.Path, Message: is.Message})
		}
	}
	if keep == nil {
		keep = []SkillIssue{}
	}
	issuesJSON, _ := json.Marshal(keep)
	return &model.AgentSkillVersion{
		SHA256: res.SHA256, Description: res.Description, FrontmatterJSON: fm, UnsupportedFields: unsupJSON,
		BodyText: strings.TrimSpace(res.Body), FilesJSON: files, FileCount: len(res.Files), TotalBytes: res.TotalBytes, HasScripts: res.HasScripts,
		IssuesJSON: issuesJSON, StorageID: storageID, PackageKey: "agent-skills/" + res.Name + "/" + s.opt.NewID() + ".zip", CreatedBy: actorID,
	}, nil
}

// rollbackReserved 回退一个已预留的版本：先删行（读路径本来就看不到 pending，删行是为了不让清理任务多跑），再删对象。
func (s *SkillService) rollbackReserved(ctx context.Context, storageID uint64, ver *model.AgentSkillVersion) {
	if err := s.repo.DiscardVersion(ctx, ver.ID); err != nil {
		logger.Warn("回退 pending 版本失败，等清理任务回收", zap.Error(err), zap.Uint64("version_id", ver.ID))
		return
	}
	s.deleteObject(ctx, storageID, ver.PackageKey)
}

// deleteObject 尽力删除一个对象；失败只记日志（行已经没了的对象没有别的清理途径，所以这里先于行删除的场景要谨慎调用）。
func (s *SkillService) deleteObject(ctx context.Context, storageID uint64, key string) {
	h, err := s.stores.Get(ctx, storageID)
	if err == nil {
		err = h.Storage.Delete(ctx, key)
	}
	if err != nil {
		logger.Warn("删除技能包对象失败", zap.Error(err), zap.String("key", key))
	}
}

// auditSkill 写一条技能审计，失败只记日志（变更已生效，不能回滚）。
func (s *SkillService) auditSkill(ctx context.Context, actorID uint64, action string, targetID uint64, detail map[string]any) {
	if s.audit == nil {
		return
	}
	entry := &model.AdminAuditLog{ActorID: actorID, Action: action, TargetType: model.AdminAuditTargetAgentSkill, TargetID: targetID}
	if b, err := json.Marshal(detail); err == nil {
		entry.DetailJSON = model.JSONText(b)
	}
	if err := s.audit.Insert(ctx, entry); err != nil {
		logger.Error("写技能审计失败", zap.Error(err), zap.String("action", action), zap.Uint64("actor_id", actorID))
	}
}

// clipRunes 去掉首尾空白后按字符数截断（不加省略号，用于写库的名字类字段）。
func clipRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n])
}

// readPackageFile 按清单从整包里取一个文件：路径必须在清单里；二进制只返回大小；文本超过预览上限时截断。
func readPackageFile(pkg []byte, files []SkillFileView, path string) (*SkillFileContent, error) {
	var meta *SkillFileView
	for i := range files {
		if files[i].Path == path {
			meta = &files[i]
			break
		}
	}
	if meta == nil {
		return nil, errcode.ErrInvalidParams.WithMsg("包里没有这个文件：" + clipRunes(path, 120))
	}
	if !meta.Text {
		return &SkillFileContent{Path: meta.Path, Size: meta.Size, Binary: true}, nil
	}
	zr, err := zip.NewReader(bytes.NewReader(pkg), int64(len(pkg)))
	if err != nil {
		return nil, fmt.Errorf("技能包损坏：%w", err)
	}
	for _, f := range zr.File {
		if f.Name != meta.Path {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("技能包损坏：%w", err)
		}
		defer func() { _ = rc.Close() }()
		data, err := io.ReadAll(io.LimitReader(rc, skillpkg.MaxPreviewBytes+1))
		if err != nil {
			return nil, fmt.Errorf("技能包损坏：%w", err)
		}
		out := &SkillFileContent{Path: meta.Path, Size: meta.Size, Text: string(data)}
		if len(data) > skillpkg.MaxPreviewBytes {
			out.Text, out.Truncated = strings.ToValidUTF8(string(data[:skillpkg.MaxPreviewBytes]), ""), true
		}
		return out, nil
	}
	return nil, fmt.Errorf("技能包里缺少清单中的文件 %s", meta.Path)
}

// pkgCache 是进程内的小型整包缓存：Agent 一次运行会连续读同一技能的多个文件，每次都从对象存储取整包太慢。
// 版本不可变，所以缓存永远不会过期，只会被挤出去；容量按个数计，包上限 50MB，默认 8 个。
type pkgCache struct {
	mu    sync.Mutex
	cap   int
	order []uint64
	items map[uint64][]byte
}

func newPkgCache(capacity int) *pkgCache {
	return &pkgCache{cap: capacity, items: map[uint64][]byte{}}
}

func (c *pkgCache) get(id uint64) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.items[id]
	return b, ok
}

func (c *pkgCache) put(id uint64, b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[id]; !ok {
		c.order = append(c.order, id)
	}
	c.items[id] = b
	for len(c.order) > c.cap {
		delete(c.items, c.order[0])
		c.order = c.order[1:]
	}
}

func (c *pkgCache) drop(id uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, id)
}
