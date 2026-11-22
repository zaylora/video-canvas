package service

import (
	"archive/zip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"

	"go.uber.org/zap"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// 压缩包顶层分组，按生产链路排：剧本 → 分镜表 → 角色 → 图片 → 视频 → 音频 → 成片。
// 成片单独一组，不混进上游素材里。
const (
	archiveGroupScript = "剧本"
	archiveGroupBoard  = "分镜表"
	archiveGroupRole   = "角色"
	archiveGroupImage  = "图片"
	archiveGroupVideo  = "视频"
	archiveGroupAudio  = "音频"
	archiveGroupFinal  = "成片"
)

var archiveGroupOrder = []string{
	archiveGroupScript, archiveGroupBoard, archiveGroupRole,
	archiveGroupImage, archiveGroupVideo, archiveGroupAudio, archiveGroupFinal,
}

// 清单文件名。文本条目的文件名取节点标题（剧本 / 分镜表 / 角色），不再用固定名。
const (
	archiveManifestMD  = "manifest.md"
	archiveManifestCSV = "manifest.csv"
)

const (
	// archiveTicketTTL 是打包下载票据的有效期：够浏览器从点按钮到真正发请求，又不至于变成长期公开链接。
	archiveTicketTTL = 10 * time.Minute
	// archiveNameRunes 是目录 / 文件名里保留的字符数：节点标题可能很长，压到 48 字以内保证解压路径可用。
	archiveNameRunes = 48
)

// CanvasArchiveAssets 是打包下载依赖的素材能力，由 AssetService 实现（都校验素材归属当前用户）。
type CanvasArchiveAssets interface {
	// Get 返回素材元信息（mime、大小、时长），用于分组、命名和清单。
	Get(ctx context.Context, userID, assetID uint64) (*model.Asset, error)
	// Open 打开素材内容，调用方负责关闭 Body。
	Open(ctx context.Context, userID, assetID uint64) (*provider.AssetFile, error)
}

// CanvasArchiveCanvases 是打包下载读画布的入口，由 repository.CanvasProjectRepository 实现。
type CanvasArchiveCanvases interface {
	GetByID(ctx context.Context, userID, id uint64) (*model.CanvasProject, error)
}

// CanvasArchiveDeps 是打包下载服务的依赖。
type CanvasArchiveDeps struct {
	Canvases CanvasArchiveCanvases
	Assets   CanvasArchiveAssets
	// Secret 用来签下载票据；为空时票据功能关闭。
	Secret string
	// Now 返回当前时间，测试里注入固定值；为空时用 time.Now。
	Now func() time.Time
}

// ArchiveEntry 是压缩包里的一个文件：节点、版本、文件本身与它在包内的路径。
// 媒体条目带 Asset，文本条目（剧本 / 分镜表 / 角色设定）带 Text。
type ArchiveEntry struct {
	Group    string       // 顶层分组
	Dir      string       // 包内目录（含分组名），如「视频/01_S4 副歌」
	Name     string       // 文件名，如 v1.mp4
	Ext      string       // 扩展名，媒体条目按 mime 推、文本条目固定
	NodeID   string       // 画布节点 id
	Node     string       // 节点标题
	Version  int          // 媒体条目的版本号，从 1 起；成片和分镜手传图为 0
	Versions int          // 该节点在这一组里的媒体数
	Final    bool         // 是不是成片
	Shot     int          // >0 表示分镜表第 N 镜手传的分镜图
	Spec     string       // 文本条目的规格说明（如「12 镜 · 46.0s」）
	Asset    *model.Asset // 媒体条目的素材元信息
	Text     string       // 文本条目的内容
	Failed   bool         // 写包时读不到文件，清单里标出来
	Reason   string       // Failed 为真时的原因
}

// Path 返回条目在压缩包内的完整路径（zip 规范用 / 分隔）。
func (e ArchiveEntry) Path() string { return path.Join(e.Dir, e.Name) }

// Size 返回这个文件的字节数：媒体取素材大小，文本取内容长度。
func (e ArchiveEntry) Size() int64 {
	if e.Asset != nil {
		return e.Asset.ByteSize
	}
	return int64(len(e.Text))
}

// ArchiveSkip 是画布上引用了、但打不进包的素材（已删除、属于别人、编号坏了）。
type ArchiveSkip struct {
	Node   string // 节点标题
	Reason string // 给用户看的原因
}

// CanvasArchive 是一次打包的规划结果：包内文件、被跳过的引用、总体积与建议文件名。
type CanvasArchive struct {
	CanvasID uint64
	Title    string
	// Revision 是画布修订号，写进包名（v97）和清单，同一张画布多次打包不会互相覆盖。
	Revision uint64
	// FileName 是下载文件名：日期_项目名_版本号.zip。
	// 包内不再套一层同名文件夹：解压直接是「剧本/图片/视频…」，少一层目录更好翻。
	FileName   string
	CreatedAt  time.Time
	Entries    []ArchiveEntry
	Skipped    []ArchiveSkip
	TotalBytes int64
}

// Count 返回按分组统计的文件数。
func (a *CanvasArchive) Count() map[string]int {
	out := make(map[string]int, len(archiveGroupOrder))
	for _, e := range a.Entries {
		out[e.Group]++
	}
	return out
}

// CanvasArchiveService 把一张画布按节点归类打成一个 zip：顶层是分组，多份产物才给节点建目录，
// 媒体按历次产物 v1、v2 排，剧本 / 分镜表 / 角色设定导出成文本，另附 manifest.md（人看）
// 与 manifest.csv（表格 / 批量核对）。
//
// 为什么在后端流式打包而不是前端拉完再压：画布素材在对象存储里，几十个文件几百 MB，
// 浏览器全拉进内存再放气会直接卡死标签页。
type CanvasArchiveService struct {
	canvases CanvasArchiveCanvases
	assets   CanvasArchiveAssets
	key      []byte
	now      func() time.Time
}

// NewCanvasArchiveService 创建打包下载服务。
func NewCanvasArchiveService(d CanvasArchiveDeps) *CanvasArchiveService {
	s := &CanvasArchiveService{canvases: d.Canvases, assets: d.Assets, now: d.Now}
	if s.now == nil {
		s.now = time.Now
	}
	if d.Secret != "" {
		// 票据不直接用 JWT 密钥：换个用途单独派生一把，泄漏面互不影响
		sum := sha256.Sum256([]byte("video-canvas/canvas-archive/" + d.Secret))
		s.key = sum[:]
	}
	return s
}

// archiveBucket 是「一个节点在一个分组下的目录」：同目录的文件先攒起来，编号时才知道该给几个版本。
type archiveBucket struct {
	node  canvasgraph.Node
	items []ArchiveEntry
}

// Plan 读画布并算出压缩包里该有哪些文件。什么内容都没有时返回 ErrCanvasArchiveEmpty，
// 让前端在发下载请求之前就能给出提示。
func (s *CanvasArchiveService) Plan(ctx context.Context, userID, canvasID uint64) (*CanvasArchive, error) {
	c, err := s.canvases.GetByID(ctx, userID, canvasID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrCanvasNotFound
		}
		return nil, err
	}
	g, err := canvasgraph.Parse(c.PayloadJSON)
	if err != nil {
		return nil, errcode.ErrCanvasPayload
	}
	now := s.now()
	// 包内不再套一层和压缩包同名的文件夹：解压直接是「剧本/图片/视频…」，少一层目录更好翻
	a := &CanvasArchive{
		CanvasID:  canvasID,
		Title:     c.Title,
		Revision:  c.Revision,
		CreatedAt: now,
		FileName:  archiveFileName(c.Title, c.Revision, now),
	}

	b := newArchiveBuilder(ctx, s, userID, a)
	for _, n := range archiveNodes(g) {
		b.collect(n)
	}
	assignArchivePaths(a, b.buckets)

	if len(a.Entries) == 0 {
		return nil, errcode.ErrCanvasArchiveEmpty
	}
	return a, nil
}

// Write 把规划好的文件流式写进 w（zip）。单个媒体文件读不到时跳过并在清单里标记，不中断整包——
// 已经开始写 zip 后再返错只会让用户拿到一个坏包，不如把能给的都给出去。
func (s *CanvasArchiveService) Write(ctx context.Context, userID uint64, a *CanvasArchive, w io.Writer) error {
	zw := zip.NewWriter(w)
	for i := range a.Entries {
		e := &a.Entries[i]
		if e.Asset == nil {
			if err := writeArchiveText(zw, e, a.CreatedAt); err != nil {
				return err
			}
			continue
		}
		if err := s.writeArchiveMedia(ctx, userID, zw, e, a.CreatedAt); err != nil {
			logger.Warn("打包画布素材时跳过读不到的文件",
				zap.Uint64("asset_id", e.Asset.ID), zap.String("path", e.Path()), zap.Error(err))
			e.Failed, e.Reason = true, "文件读取失败，已跳过"
		}
	}
	if err := writeArchiveManifest(zw, a); err != nil {
		return err
	}
	return zw.Close()
}

// writeArchiveText 写一个文本条目（剧本 / 分镜表 / 角色设定），放气压缩。
func writeArchiveText(zw *zip.Writer, e *ArchiveEntry, modTime time.Time) error {
	h := &zip.FileHeader{Name: e.Path(), Method: zip.Deflate, Modified: modTime}
	h.SetMode(0o644)
	w, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, e.Text)
	return err
}

// writeArchiveMedia 写一个媒体文件条目。先 Open 再建条目：读不到就不建，避免 zip 里出现空文件。
func (s *CanvasArchiveService) writeArchiveMedia(
	ctx context.Context, userID uint64, zw *zip.Writer, e *ArchiveEntry, modTime time.Time,
) error {
	file, err := s.assets.Open(ctx, userID, e.Asset.ID)
	if err != nil {
		return err
	}
	defer func() { _ = file.Body.Close() }()

	// 音视频本身已经压过，再走 deflate 只是白耗 CPU
	h := &zip.FileHeader{Name: e.Path(), Method: zip.Store, Modified: modTime}
	h.SetMode(0o644)
	dst, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, file.Body)
	return err
}

// archiveNodeOrder 与画布上的阅读顺序一致：先上下、再左右，位置完全相同时按 id 定死，保证两次打包结果一致。
func archiveNodeOrder(a, b canvasgraph.Node) int {
	pa, pb := a.Position(), b.Position()
	if pa.Y != pb.Y {
		if pa.Y < pb.Y {
			return -1
		}
		return 1
	}
	if pa.X != pb.X {
		if pa.X < pb.X {
			return -1
		}
		return 1
	}
	return strings.Compare(a.ID(), b.ID())
}

// archiveGroupOf 决定素材进哪个分组：成片单独一组，其余按素材自己的 kind。
func archiveGroupOf(ref canvasgraph.MediaRef, item *model.Asset) string {
	if ref.Final {
		return archiveGroupFinal
	}
	switch item.Kind {
	case canvasgraph.MediaVideo:
		return archiveGroupVideo
	case canvasgraph.MediaAudio:
		return archiveGroupAudio
	case canvasgraph.MediaImage:
		return archiveGroupImage
	}
	switch ref.Kind {
	case canvasgraph.MediaVideo:
		return archiveGroupVideo
	case canvasgraph.MediaAudio:
		return archiveGroupAudio
	}
	return archiveGroupImage
}

// archiveExts 把 MIME 映射成扩展名；不认识的再退回原始文件名的后缀，最后兜底 .bin。
var archiveExts = map[string]string{
	"video/mp4":        ".mp4",
	"video/quicktime":  ".mov",
	"video/webm":       ".webm",
	"video/x-matroska": ".mkv",
	"image/png":        ".png",
	"image/jpeg":       ".jpg",
	"image/webp":       ".webp",
	"image/gif":        ".gif",
	"image/avif":       ".avif",
	"audio/mpeg":       ".mp3",
	"audio/mp4":        ".m4a",
	"audio/aac":        ".aac",
	"audio/wav":        ".wav",
	"audio/x-wav":      ".wav",
	"audio/ogg":        ".ogg",
	"audio/flac":       ".flac",
}

func archiveExt(item *model.Asset) string {
	base := strings.ToLower(strings.TrimSpace(strings.Split(item.MimeType, ";")[0]))
	if ext, ok := archiveExts[base]; ok {
		return ext
	}
	if dot := path.Ext(item.FileName); len(dot) > 1 && len(dot) <= 6 {
		return dot
	}
	return ".bin"
}

// archiveDirName 把节点标题清洗成可用的目录名：去掉路径分隔符和控制字符，超长截断，空标题用节点 id 兜底。
func archiveDirName(n canvasgraph.Node) string {
	name := archiveSafeText(n.Label(), archiveNameRunes)
	if name == "" {
		id := n.ID()
		if id == "" {
			id = "node"
		}
		name = id
	}
	return name
}

// archiveBaseName 是扁平文件的文件名主干：节点标题优先；标题为空时，
// 剧本 / 分镜表 / 角色这类文本节点退回分组名（和画布上的默认叫法一致），媒体退回节点 id。
func archiveBaseName(n canvasgraph.Node, group string) string {
	if name := archiveSafeText(n.Label(), archiveNameRunes); name != "" {
		return name
	}
	switch group {
	case archiveGroupScript, archiveGroupBoard, archiveGroupRole:
		return group
	}
	if id := archiveSafeText(n.ID(), archiveNameRunes); id != "" {
		return id
	}
	return "画布"
}

// archiveUnique 在同一分组内给扁平文件去重：两个节点标题撞了时依次加 -2、-3。
func archiveUnique(name string, used map[string]bool) string {
	if !used[name] {
		used[name] = true
		return name
	}
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		cand := base + "-" + strconv.Itoa(i) + ext
		if !used[cand] {
			used[cand] = true
			return cand
		}
	}
}

// archiveFileName 生成下载文件名：<日期>_<项目名>_v<修订号>.zip。
// 修订号跟着画布走，同一张画布改过之后重新打包不会覆盖上一次的包，也不会出现两个同名文件夹。
func archiveFileName(title string, revision uint64, now time.Time) string {
	base := archiveSafeText(title, archiveNameRunes)
	if base == "" {
		base = "画布"
	}
	if revision == 0 {
		revision = 1
	}
	return fmt.Sprintf("%s_%s_v%d.zip", now.Format("20060102"), base, revision)
}

// archiveSafeText 把标题清洗成可用的文件 / 目录名：只留中文、字母、数字与连字符，
// 其余（空格、斜杠、间隔号等特殊符号、控制字符）一律并成一个下划线，
// 纯数字的名字前面补「画布_」，避免和版本号、序号混在一起看不出是什么。
func archiveSafeText(s string, limit int) string {
	var b strings.Builder
	kept, gap := 0, false
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' {
			if kept > 0 {
				gap = true
			}
			continue
		}
		if gap && kept < limit {
			b.WriteByte('_')
			kept++
			gap = false
		}
		if kept >= limit {
			break
		}
		b.WriteRune(r)
		kept++
	}
	out := strings.Trim(b.String(), "_")
	if out != "" && archiveAllDigits(out) {
		out = "画布_" + out
	}
	return out
}

func archiveAllDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return s != ""
}

// Sign 给一次打包下载签票据。服务端不存状态：票据本身就是「画布 + 用户 + 过期时间」的签名，
// 这样浏览器可以直接用 <a download> 拿它下载，不必带 Authorization 头，也就不必把几百 MB 塞进 JS 内存。
func (s *CanvasArchiveService) Sign(userID, canvasID uint64) (string, time.Time) {
	if len(s.key) == 0 {
		return "", time.Time{}
	}
	exp := s.now().Add(archiveTicketTTL)
	payload := fmt.Sprintf("v1.%d.%d.%d", canvasID, userID, exp.Unix())
	return payload + "." + s.sign(payload), exp
}

// ParseTicket 校验票据并取回它对应的用户与画布。签名不对、格式不对、过期都返回 ErrCanvasArchiveTicket。
func (s *CanvasArchiveService) ParseTicket(ticket string) (userID, canvasID uint64, err error) {
	bad := errcode.ErrCanvasArchiveTicket
	if len(s.key) == 0 || ticket == "" {
		return 0, 0, bad
	}
	idx := strings.LastIndexByte(ticket, '.')
	if idx <= 0 {
		return 0, 0, bad
	}
	payload, sig := ticket[:idx], ticket[idx+1:]
	if !hmac.Equal([]byte(sig), []byte(s.sign(payload))) {
		return 0, 0, bad
	}
	parts := strings.Split(payload, ".")
	if len(parts) != 4 || parts[0] != "v1" {
		return 0, 0, bad
	}
	cid, cErr := strconv.ParseUint(parts[1], 10, 64)
	uid, uErr := strconv.ParseUint(parts[2], 10, 64)
	exp, eErr := strconv.ParseInt(parts[3], 10, 64)
	if cErr != nil || uErr != nil || eErr != nil || uid == 0 || cid == 0 {
		return 0, 0, bad
	}
	if s.now().After(time.Unix(exp, 0)) {
		return 0, 0, bad
	}
	return uid, cid, nil
}

func (s *CanvasArchiveService) sign(payload string) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
