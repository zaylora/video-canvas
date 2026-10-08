package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/model"
	"video-canvas/pkg/pagination"
)

// 素材库列表的分页规则。
const (
	// defaultShowcaseLibraryPageSize 是素材库每页默认条数：前端按 6 列网格展示，48 恰好是 8 整行。
	defaultShowcaseLibraryPageSize = 48
	// maxShowcaseLibraryPageSize 是素材库每页上限，挡住一次拉太多行把后台打慢。
	maxShowcaseLibraryPageSize = 100
)

// Library 返回后台“从素材库添加”用的素材库：全平台所有用户生成的视频（kind=video 且 source=generated），
// 按创建时间倒序分页；每项带上提示词、模型名、作者，以及是否已被展示条目引用。
func (s *ShowcaseService) Library(ctx context.Context, req *model.ListShowcaseLibraryReq) (*model.ShowcaseLibraryView, error) {
	// 1. 修正分页：page<1 取 1；page_size<1 取默认 48，超过 100 收敛到 100（非法值回落而不是报错，列表页不该因为一个多余的参数打不开）
	q := pagination.Query{Page: req.Page, PageSize: req.PageSize}
	if q.Page < 1 {
		q.Page = pagination.DefaultPage
	}
	if q.PageSize < 1 {
		q.PageSize = defaultShowcaseLibraryPageSize
	}
	q.PageSize = min(q.PageSize, maxShowcaseLibraryPageSize)

	// 2. 取当前页的平台生成视频。这里不带用户条件：素材库是后台管理员挑选展示作品用的，要看到全平台的产物（需要显示作者）
	assets, total, err := s.assets.ListByKindSource(ctx, model.KindVideo, model.AssetSourceGenerated, q.Offset(), q.Limit())
	if err != nil {
		return nil, err
	}
	out := &model.ShowcaseLibraryView{Items: make([]model.ShowcaseLibraryItem, 0, len(assets)), Total: total, Page: q.Page, PageSize: q.PageSize}
	if len(assets) == 0 {
		return out, nil
	}

	// 3. 一次性批量取出这一页要用的全部关联数据（任务、作者、已引用标记），每类各一次查询，避免 N+1
	tasks, err := s.libraryTasks(ctx, assets)
	if err != nil {
		return nil, err
	}
	owners, err := s.libraryOwners(ctx, assets)
	if err != nil {
		return nil, err
	}
	added, err := s.libraryAdded(ctx, assets)
	if err != nil {
		return nil, err
	}

	// 4. 逐条组装。任务或用户查不到都不算错误：素材可能是历史数据、任务被清理、用户被删除，列表要能照常展示
	for i := range assets {
		a := &assets[i]
		v, err := s.views.ViewOf(ctx, a)
		if err != nil {
			return nil, err
		}
		item := model.ShowcaseLibraryItem{
			AssetID: a.ID, VideoURL: v.URL, CreatedAt: a.CreatedAt, Width: v.Width, Height: v.Height,
			ByteSize: v.ByteSize, DurationMs: v.DurationMs, Added: added[a.ID], Owner: owners[a.UserID],
		}
		if a.TaskID != nil {
			if t, ok := tasks[*a.TaskID]; ok {
				item.Prompt, item.ModelLabel = libraryTaskPrompt(t), libraryTaskModelLabel(t)
			}
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

// libraryTasks 批量取这一页素材对应的生成任务，返回 任务 id -> 任务；素材都没有 task_id 时不发查询。
func (s *ShowcaseService) libraryTasks(ctx context.Context, assets []model.Asset) (map[uint64]*model.GenerationTask, error) {
	seen := map[uint64]struct{}{}
	var ids []uint64
	for _, a := range assets {
		if a.TaskID == nil {
			continue
		}
		if _, dup := seen[*a.TaskID]; !dup {
			seen[*a.TaskID] = struct{}{}
			ids = append(ids, *a.TaskID)
		}
	}
	m := make(map[uint64]*model.GenerationTask, len(ids))
	if len(ids) == 0 {
		return m, nil
	}
	rows, err := s.tasks.ListBriefByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		m[rows[i].ID] = &rows[i]
	}
	return m, nil
}

// libraryOwners 批量取这一页素材的作者名，返回 用户 id -> 展示名；查不到用户名的用 "用户#<id>"，保证每个作者都有可显示的名字。
func (s *ShowcaseService) libraryOwners(ctx context.Context, assets []model.Asset) (map[uint64]string, error) {
	seen := map[uint64]struct{}{}
	var ids []uint64
	for _, a := range assets {
		if _, dup := seen[a.UserID]; !dup {
			seen[a.UserID] = struct{}{}
			ids = append(ids, a.UserID)
		}
	}
	names, err := s.users.ListUsernames(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[uint64]string, len(ids))
	for _, id := range ids {
		if name := names[id]; name != "" {
			out[id] = name
		} else {
			out[id] = "用户#" + strconv.FormatUint(id, 10)
		}
	}
	return out, nil
}

// libraryAdded 返回这一页素材里已被未删除展示条目引用的素材 id 集合（禁用的条目也算已添加）。
func (s *ShowcaseService) libraryAdded(ctx context.Context, assets []model.Asset) (map[uint64]bool, error) {
	ids := make([]uint64, 0, len(assets))
	for _, a := range assets {
		ids = append(ids, a.ID)
	}
	used, err := s.items.ReferencedAssetIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	m := make(map[uint64]bool, len(used))
	for _, id := range used {
		m[id] = true
	}
	return m, nil
}

// libraryTaskPrompt 取任务输入里的提示词文本，去首尾空白后按字符数截到 80（展示作品的文案上限）。
// 输入不是合法 JSON、没有 prompt 或 prompt 不是字符串都返回空串：这是辅助信息，脏数据不应让整个素材库列表失败。
func libraryTaskPrompt(t *model.GenerationTask) string {
	var in struct {
		Prompt any `json:"prompt"`
	}
	if err := json.Unmarshal(t.InputJSON, &in); err != nil {
		return ""
	}
	text, _ := in.Prompt.(string)
	return truncateRunes(strings.TrimSpace(text), maxShowcasePromptRunes)
}

// libraryTaskModelLabel 取任务创建时快照里的模型展示名（config_snapshot.model.label）；
// 快照缺失或没有展示名时退回模型 key，再没有就是空串。结果截到 40 个字符（展示条目的模型标注上限），前端可以直接带去新增条目。
func libraryTaskModelLabel(t *model.GenerationTask) string {
	var snap struct {
		Model struct {
			Label string `json:"label"`
		} `json:"model"`
	}
	label := ""
	// 快照损坏时忽略解析错误，直接退回模型 key：同样是辅助信息
	if err := json.Unmarshal(t.ConfigSnapshot, &snap); err == nil {
		label = strings.TrimSpace(snap.Model.Label)
	}
	if label == "" {
		label = strings.TrimSpace(t.ModelKey)
	}
	return truncateRunes(label, maxShowcaseLabelRunes)
}

// truncateRunes 把 s 按字符数（而不是字节数）截到最多 n 个字符。
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
