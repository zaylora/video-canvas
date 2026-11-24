package service

import (
	"archive/zip"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"video-canvas/internal/agent/canvasgraph"
)

// renderBoardMD 把分镜表渲染成人读的 Markdown 表：一镜一行，素材到位情况单独两列，
// 这样不打开画布也能看清哪几镜还没出图 / 没出片。
func renderBoardMD(shots []canvasgraph.Shot) string {
	var b strings.Builder
	b.WriteString("# 分镜表\n\n")
	b.WriteString(archiveBoardSpec(shots) + "\n\n")
	b.WriteString("| 镜号 | 景别 | 运镜 | 时长 | 画面 | 动作 | 对白 | 分镜图 | 视频 |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, s := range shots {
		b.WriteString("| " + fmt.Sprintf("%02d", s.Seq) + " | " + archiveCell(archiveOrDash(s.Size)) + " | " +
			archiveCell(archiveOrDash(s.Camera)) + " | " + archiveShotDuration(s.Duration) + " | " +
			archiveCell(archiveOrDash(s.Scene)) + " | " + archiveCell(archiveOrDash(s.Action)) + " | " +
			archiveCell(archiveOrDash(s.Dialogue)) + " | " + archiveTick(s.ImageNodeID != "" || s.HasImage) + " | " +
			archiveTick(s.VideoNodeID != "") + " |\n")
	}
	b.WriteString("\n> 分镜图与视频两列只标这一镜有没有关联素材，文件本身在「图片」「视频」两个分组里。\n")
	return b.String()
}

// renderBoardCSV 是分镜表的表格版本，列固定，便于 Excel 与后续工具处理。
func renderBoardCSV(shots []canvasgraph.Shot) string {
	var b strings.Builder
	cw := csv.NewWriter(&b)
	rows := [][]string{{"镜号", "景别", "运镜", "时长(秒)", "画面", "动作", "对白", "分镜图", "视频"}}
	for _, s := range shots {
		rows = append(rows, []string{
			strconv.Itoa(s.Seq), s.Size, s.Camera, archiveShotSeconds(s.Duration),
			s.Scene, s.Action, s.Dialogue,
			archiveTick(s.ImageNodeID != "" || s.HasImage), archiveTick(s.VideoNodeID != ""),
		})
	}
	_ = cw.WriteAll(rows)
	cw.Flush()
	return b.String()
}

// renderRoleMD 把角色卡的设定渲染成 Markdown：文字类信息（名字、形象、设定、声音提示词）落成文件，
// 图片仍然走「图片」分组，不重复导出。
func renderRoleMD(n canvasgraph.Node, info canvasgraph.CharacterInfo) string {
	var b strings.Builder
	title := info.Name
	if title == "" {
		title = n.Label()
	}
	look := info.Look
	if look == "" {
		look = "基础形象"
	}
	b.WriteString("# " + title + " · " + look + "\n\n")
	b.WriteString("- 补充图：" + strconv.Itoa(info.Extras) + " 张（在「图片」分组里）\n")
	if info.HasVoice {
		name := info.VoiceName
		if name == "" {
			name = "已绑定"
		}
		b.WriteString("- 声音：" + name + "（在「音频」分组里）\n")
	} else {
		b.WriteString("- 声音：未绑定\n")
	}
	b.WriteString("\n## 设定\n\n")
	b.WriteString(archiveOrDash(info.Description) + "\n")
	if strings.TrimSpace(info.VoicePrompt) != "" {
		b.WriteString("\n## 生成声音的提示词\n\n" + info.VoicePrompt + "\n")
	}
	return b.String()
}

// archiveBoardSpec 是分镜表的一句话规格：几镜、总时长。
func archiveBoardSpec(shots []canvasgraph.Shot) string {
	total := 0.0
	for _, s := range shots {
		total += s.Duration
	}
	return fmt.Sprintf("%d 镜 · 总时长 %.1fs", len(shots), total)
}

// archiveRoleSpec 是角色卡的规格：几张补充图、有没有声音。
func archiveRoleSpec(info canvasgraph.CharacterInfo) string {
	voice := "无声音"
	if info.HasVoice {
		voice = "有声音"
	}
	return fmt.Sprintf("补充图 %d 张 · %s", info.Extras, voice)
}

// archiveWordsText 数一段文本的字数（按字符算，中英文都按「字」）。
func archiveWordsText(s string) string {
	return strconv.Itoa(len([]rune(s))) + " 字"
}

// archiveShotDuration 是表格里的时长写法：3.5s；没填时长时给 -。
func archiveShotDuration(sec float64) string {
	if sec <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1fs", sec)
}

// archiveShotSeconds 是 CSV 里的时长：纯数字，方便表格求和。
func archiveShotSeconds(sec float64) string {
	if sec <= 0 {
		return ""
	}
	return strconv.FormatFloat(sec, 'f', 1, 64)
}

func archiveOrDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func archiveTick(ok bool) string {
	if ok {
		return "✓"
	}
	return "-"
}

// writeArchiveManifest 写清单：manifest.md 给人看，manifest.csv 给表格和批量核对。
func writeArchiveManifest(zw *zip.Writer, a *CanvasArchive) error {
	// 顺序写死：同一张画布反复打包，包内条目顺序要一致，方便对比两次导出
	for _, item := range []struct{ name, body string }{
		{archiveManifestMD, archiveManifestMDText(a)},
		{archiveManifestCSV, archiveManifestCSVText(a)},
	} {
		h := &zip.FileHeader{Name: item.name, Method: zip.Deflate, Modified: a.CreatedAt}
		h.SetMode(0o644)
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(w, item.body); err != nil {
			return err
		}
	}
	return nil
}

// archiveManifestMDText 渲染人读的清单：总览 + 每组一张表 + 跳过项。
func archiveManifestMDText(a *CanvasArchive) string {
	var b strings.Builder
	count := a.Count()
	b.WriteString("# " + a.Title + " · 画布素材\n\n")
	b.WriteString("- 导出时间：" + a.CreatedAt.Format("2006-01-02 15:04") + "\n")
	b.WriteString("- 画布修订号：v" + strconv.FormatUint(a.Revision, 10) + "（压缩包名里的版本号就是它）\n")
	b.WriteString("- 文件数：" + strconv.Itoa(len(a.Entries)) + " 个（")
	parts := make([]string, 0, len(archiveGroupOrder))
	for _, g := range archiveGroupOrder {
		if count[g] > 0 {
			parts = append(parts, g+" "+strconv.Itoa(count[g]))
		}
	}
	b.WriteString(strings.Join(parts, " · ") + "）\n")
	b.WriteString("- 合计：" + archiveSizeText(a.TotalBytes) + "\n")
	b.WriteString("- 归类方式：顶层是分组；一个节点有多份产物（历次版本、每镜手传图）时才给它一个目录，单份的直接放在分组下\n")
	b.WriteString("- 剧本、分镜表、角色设定导出成文本文件，内容与画布上一致\n")
	if len(a.Skipped) > 0 {
		b.WriteString("- 跳过：" + strconv.Itoa(len(a.Skipped)) + " 项（见文末）\n")
	}
	b.WriteString("\n")

	for _, group := range archiveGroupOrder {
		items := make([]ArchiveEntry, 0, count[group])
		for _, e := range a.Entries {
			if e.Group == group {
				items = append(items, e)
			}
		}
		if len(items) == 0 {
			continue
		}
		b.WriteString("## " + group + "（" + strconv.Itoa(len(items)) + "）\n\n")
		b.WriteString("| 目录 | 文件 | 节点 | 版本 | 规格 | 大小 | 素材 |\n")
		b.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
		for _, e := range items {
			name := e.Name
			if e.Failed {
				name += "（" + e.Reason + "）"
			}
			b.WriteString("| " + archiveCell(e.Dir) + " | " + archiveCell(name) + " | " + archiveCell(e.Node) + " | " +
				archiveEntryVersion(e) + " | " + archiveEntrySpec(e) + " | " + archiveSizeText(e.Size()) + " | " +
				archiveEntryAsset(e) + " |\n")
		}
		b.WriteString("\n")
	}

	if len(a.Skipped) > 0 {
		b.WriteString("## 跳过（" + strconv.Itoa(len(a.Skipped)) + "）\n\n")
		b.WriteString("画布上引用了、但文件已经取不到，无法放进压缩包：\n\n")
		for _, s := range a.Skipped {
			b.WriteString("- " + archiveCell(s.Node) + "：" + s.Reason + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("> " + archiveManifestCSV + " 是同一份清单的表格版本，可用 Excel 打开做批量核对。\n")
	return b.String()
}

// archiveManifestCSVText 是表格版清单，列固定，文本条目与媒体条目共用同一套列。
func archiveManifestCSVText(a *CanvasArchive) string {
	var b strings.Builder
	cw := csv.NewWriter(&b)
	rows := [][]string{{
		"分组", "目录", "文件", "节点", "节点ID", "版本", "成片", "分镜",
		"宽", "高", "时长(ms)", "大小(字节)", "素材ID", "原始文件名", "状态",
	}}
	for _, e := range a.Entries {
		final, shot, width, height, duration, assetID, fileName := "0", "", "0", "0", "0", "", ""
		if e.Final {
			final = "1"
		}
		if e.Shot > 0 {
			shot = strconv.Itoa(e.Shot)
		}
		if e.Asset != nil {
			width, height = strconv.Itoa(e.Asset.Width), strconv.Itoa(e.Asset.Height)
			duration = strconv.FormatInt(e.Asset.DurationMs, 10)
			assetID, fileName = strconv.FormatUint(e.Asset.ID, 10), e.Asset.FileName
		}
		version := ""
		if e.Version > 0 {
			version = strconv.Itoa(e.Version)
		}
		status := "ok"
		if e.Failed {
			status = e.Reason
		}
		rows = append(rows, []string{
			e.Group, e.Dir, e.Name, e.Node, e.NodeID, version, final, shot,
			width, height, duration, strconv.FormatInt(e.Size(), 10), assetID, fileName, status,
		})
	}
	_ = cw.WriteAll(rows)
	cw.Flush()
	return b.String()
}

// archiveEntryVersion 是清单里的「版本」列：成片、分镜手传图、文本各自标注，媒体给「第几版 / 共几版」。
func archiveEntryVersion(e ArchiveEntry) string {
	switch {
	case e.Final:
		return "成片"
	case e.Shot > 0:
		return fmt.Sprintf("第 %02d 镜", e.Shot)
	case e.Asset == nil:
		return "-"
	case e.Versions > 1:
		return fmt.Sprintf("%d / %d", e.Version, e.Versions)
	}
	return "1"
}

// archiveEntrySpec 是清单里的「规格」列：媒体给分辨率与时长，文本直接给预置说明。
func archiveEntrySpec(e ArchiveEntry) string {
	if e.Asset == nil {
		if e.Spec == "" {
			return "-"
		}
		return archiveCell(e.Spec)
	}
	size := ""
	if e.Asset.Width > 0 && e.Asset.Height > 0 {
		size = fmt.Sprintf("%d×%d", e.Asset.Width, e.Asset.Height)
	}
	duration := ""
	if e.Asset.DurationMs > 0 {
		duration = fmt.Sprintf("%.1fs", float64(e.Asset.DurationMs)/1000)
	}
	switch {
	case size != "" && duration != "":
		return size + " · " + duration
	case size != "":
		return size
	case duration != "":
		return duration
	}
	return "-"
}

// archiveEntryAsset 是清单里的「素材」列：媒体给素材 id 便于回溯，文本没有素材。
func archiveEntryAsset(e ArchiveEntry) string {
	if e.Asset == nil {
		return "-"
	}
	return strconv.FormatUint(e.Asset.ID, 10)
}

// archiveCell 转义 Markdown 表格里会破坏结构的竖线与换行。
func archiveCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.ReplaceAll(s, "\n", " ")
}

// archiveSizeText 把字节数写成人看的单位。
func archiveSizeText(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return strconv.FormatInt(n, 10) + " B"
}

// 编译期确认 AssetService 满足打包下载需要的素材能力。
var _ CanvasArchiveAssets = (*AssetService)(nil)
