package service

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"video-canvas/internal/agent/canvasgraph"
)

// archiveNodes 按画布位置排（上→下、同高左→右、最后按 id 定死），和画布上的阅读顺序一致；
// 组节点只是框，自己不带内容。
func archiveNodes(g *canvasgraph.Graph) []canvasgraph.Node {
	nodes := make([]canvasgraph.Node, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		if !n.IsGroup() {
			nodes = append(nodes, n)
		}
	}
	slices.SortStableFunc(nodes, archiveNodeOrder)
	return nodes
}

// archiveBuilder 把节点的内容收进「分组 + 节点」的桶里。同一个节点在同一分组只占一个桶，
// 桶的顺序就是遇到节点的顺序（节点已按画布位置排好）。
type archiveBuilder struct {
	ctx     context.Context
	userID  uint64
	assets  CanvasArchiveAssets
	a       *CanvasArchive
	buckets map[string][]*archiveBucket
	index   map[string]map[string]*archiveBucket
}

func newArchiveBuilder(ctx context.Context, s *CanvasArchiveService, userID uint64, a *CanvasArchive) *archiveBuilder {
	return &archiveBuilder{
		ctx:     ctx,
		userID:  userID,
		assets:  s.assets,
		a:       a,
		buckets: make(map[string][]*archiveBucket, len(archiveGroupOrder)),
		index:   make(map[string]map[string]*archiveBucket, len(archiveGroupOrder)),
	}
}

// collect 收一个节点的全部内容：先文字（剧本 / 分镜表 / 角色设定），再它引用的媒体。
func (b *archiveBuilder) collect(n canvasgraph.Node) {
	b.collectText(n)
	b.collectMedia(n)
}

// bucket 取（或新建）某节点在某分组下的桶，桶的顺序就是遇到节点的顺序。
func (b *archiveBuilder) bucket(group string, n canvasgraph.Node) *archiveBucket {
	if b.index[group] == nil {
		b.index[group] = make(map[string]*archiveBucket, 4)
	}
	if v, ok := b.index[group][n.ID()]; ok {
		return v
	}
	v := &archiveBucket{node: n}
	b.index[group][n.ID()] = v
	b.buckets[group] = append(b.buckets[group], v)
	return v
}

// collectText 收文字类节点：剧本原文、分镜表、角色设定——不打包就等于把创作过程丢在平台里。
// 文件名在分配路径时按节点标题统一生成，这里只带扩展名。
func (b *archiveBuilder) collectText(n canvasgraph.Node) {
	switch n.Kind() {
	case canvasgraph.KindScript:
		b.collectScript(n)
	case canvasgraph.KindStoryboard:
		b.collectBoard(n)
	case canvasgraph.KindCharacter:
		b.collectRole(n)
	}
}

func (b *archiveBuilder) collectScript(n canvasgraph.Node) {
	body := canvasgraph.Body(n)
	if strings.TrimSpace(body) == "" {
		return
	}
	bk := b.bucket(archiveGroupScript, n)
	bk.items = append(bk.items, ArchiveEntry{Ext: ".txt", Text: body, Spec: archiveWordsText(body)})
}

func (b *archiveBuilder) collectBoard(n canvasgraph.Node) {
	shots := canvasgraph.Shots(n)
	if len(shots) == 0 {
		return
	}
	bk := b.bucket(archiveGroupBoard, n)
	spec := archiveBoardSpec(shots)
	bk.items = append(bk.items,
		ArchiveEntry{Ext: ".md", Text: renderBoardMD(shots), Spec: spec},
		ArchiveEntry{Ext: ".csv", Text: renderBoardCSV(shots), Spec: spec},
	)
}

func (b *archiveBuilder) collectRole(n canvasgraph.Node) {
	info, ok := canvasgraph.CharacterOf(n)
	if !ok || strings.TrimSpace(info.Description+info.VoicePrompt) == "" {
		return
	}
	bk := b.bucket(archiveGroupRole, n)
	bk.items = append(bk.items, ArchiveEntry{
		Ext: ".md", Text: renderRoleMD(n, info), Spec: archiveRoleSpec(info),
	})
}

// collectMedia 收节点引用的媒体。单个引用可能查不到（素材被删 / 属于别人），
// 记进 skipped 而不是让整包失败。
func (b *archiveBuilder) collectMedia(n canvasgraph.Node) {
	for _, ref := range canvasgraph.MediaRefs(n) {
		b.collectRef(n, ref)
	}
}

func (b *archiveBuilder) collectRef(n canvasgraph.Node, ref canvasgraph.MediaRef) {
	id, err := strconv.ParseUint(ref.AssetID, 10, 64)
	if err != nil {
		b.skip(n, "素材编号无法识别")
		return
	}
	item, err := b.assets.Get(b.ctx, b.userID, id)
	if err != nil {
		b.skip(n, "素材不存在或已删除")
		return
	}
	bk := b.bucket(archiveGroupOf(ref, item), n)
	bk.items = append(bk.items, ArchiveEntry{Ext: archiveExt(item), Final: ref.Final, Shot: ref.Shot, Asset: item})
}

func (b *archiveBuilder) skip(n canvasgraph.Node, reason string) {
	b.a.Skipped = append(b.a.Skipped, ArchiveSkip{Node: n.Label(), Reason: reason})
}

// assignArchivePaths 分配目录与文件名：只有多份产物（历次版本、每镜手传图）才建「NN_节点名」目录，
// 单份产物（一个剧本、一段单版本视频、一条成片）直接放在分组下——否则会出现
// 「分镜表/01_分镜表/分镜表.md」这种目录名与文件名完全重复的多余层级。
func assignArchivePaths(a *CanvasArchive, buckets map[string][]*archiveBucket) {
	for _, group := range archiveGroupOrder {
		assignArchiveGroup(a, group, buckets[group])
	}
}

func assignArchiveGroup(a *CanvasArchive, group string, list []*archiveBucket) {
	// 同一分组下扁平文件的占用表；同名节点靠 -2、-3 区分
	used := make(map[string]bool, len(list))
	// 目录序号只数真正建了目录的节点，免得扁平节点把 01、02 占了还空出来
	nestedIndex := 0
	for _, bk := range list {
		// 文本条目固定导出 md（+csv）两份，不按数量判断，永远扁平
		nested := len(bk.items) > 1 && bk.items[0].Asset != nil
		dir := group
		if nested {
			nestedIndex++
			dir = path.Join(group, fmt.Sprintf("%02d_%s", nestedIndex, archiveDirName(bk.node)))
		}
		assignArchiveBucket(a, group, dir, nested, bk, used)
	}
}

func assignArchiveBucket(a *CanvasArchive, group, dir string, nested bool, bk *archiveBucket, used map[string]bool) {
	versions := archiveVersionCount(bk.items)
	base := archiveBaseName(bk.node, group)
	version := 0
	for j := range bk.items {
		it := &bk.items[j]
		it.Group, it.Dir, it.NodeID, it.Node = group, dir, bk.node.ID(), bk.node.Label()
		if nested && it.Asset != nil && !it.Final && it.Shot == 0 {
			version++
			it.Version, it.Versions = version, versions
		}
		it.Name = archiveEntryName(it, nested, base, used)
		a.Entries = append(a.Entries, *it)
		a.TotalBytes += it.Size()
	}
}

// archiveEntryName 给一个条目起名：目录里有多份产物时用 v1 / 成片 / 第01镜，
// 扁平时直接用节点标题（分镜手传图补上镜号），同组重名再靠 archiveUnique 加序号。
func archiveEntryName(it *ArchiveEntry, nested bool, base string, used map[string]bool) string {
	if it.Asset == nil {
		// 文本文件名就是节点标题：剧本/第一场.txt、分镜表/分镜表.md + .csv
		return archiveUnique(base+it.Ext, used)
	}
	if nested {
		switch {
		case it.Final:
			return "成片" + it.Ext
		case it.Shot > 0:
			return fmt.Sprintf("第%02d镜%s", it.Shot, it.Ext)
		}
		return fmt.Sprintf("v%d%s", it.Version, it.Ext)
	}
	switch {
	case it.Final:
		// 成片就是这一次的输出，不参与 v1、v2 编号
		return archiveUnique(base+it.Ext, used)
	case it.Shot > 0:
		// 分镜手传图要留住镜号，否则一堆图摆在「图片」下分不清是哪一镜
		return archiveUnique(fmt.Sprintf("%s_第%02d镜%s", base, it.Shot, it.Ext), used)
	}
	it.Version, it.Versions = 1, 1
	return archiveUnique(base+it.Ext, used)
}

func archiveVersionCount(items []ArchiveEntry) int {
	n := 0
	for _, it := range items {
		if it.Asset != nil && !it.Final && it.Shot == 0 {
			n++
		}
	}
	return n
}
