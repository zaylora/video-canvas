// Package canvasdoc 定义画布导出文件的格式：文档结构、解析与校验。
//
// 文件格式见 docs/design/画布导入导出/画布导入导出.md 第 7 节。它是纯 Go 包，
// 不依赖任何内部包，也不碰数据库和存储：调用方负责读写文件、查找素材和落库。
package canvasdoc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

const (
	// Format 是画布导出文件的固定标识，导入时第一项校验。
	Format = "video-canvas/canvas"
	// CurrentVersion 是当前支持的文件格式版本。
	CurrentVersion = 1
)

// 哨兵错误。具体原因用 fmt.Errorf("%w: …") 包装，原因文案是中文，可以直接给用户看。
var (
	// ErrInvalid 表示文件不是合法的画布导出文件（格式、结构或引用关系不对）。
	ErrInvalid = errors.New("画布文件不合法")
	// ErrTooNew 表示文件的格式版本高于当前系统支持的版本。
	ErrTooNew = errors.New("画布文件版本过高")
	// ErrLimit 表示文件超出了节点数、素材数或素材总大小的上限。
	ErrLimit = errors.New("画布文件超出限制")
)

// Document 是一份完整的画布导出文件。
type Document struct {
	Format     string                   `json:"format"`
	Version    int                      `json:"version"`
	ExportedAt string                   `json:"exportedAt"`
	AppVersion string                   `json:"appVersion"`
	Canvas     CanvasPart               `json:"canvas"`
	Assets     map[string]AssetInfo     `json:"assets"`
	Characters map[string]CharacterInfo `json:"characters"`
}

// CanvasPart 是画布本体：标题、节点（含组节点）和连线。
// 节点和连线用 map 而不是结构体，是为了让前端新增的字段原样往返、不被这里丢掉。
type CanvasPart struct {
	Title string           `json:"title"`
	Nodes []map[string]any `json:"nodes"`
	Edges []map[string]any `json:"edges"`
}

// AssetInfo 是一个素材的元信息，键是内容哈希。只用于展示和交叉校验。
type AssetInfo struct {
	Kind       string `json:"kind"`
	MimeType   string `json:"mimeType"`
	ByteSize   int64  `json:"byteSize"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	DurationMs int64  `json:"durationMs"`
	FileName   string `json:"fileName"`
}

// CharacterInfo 是一张角色卡的内容，素材引用都是 assets 里的哈希。
type CharacterInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Avatar      string   `json:"avatar"`
	Extras      []string `json:"extras"`
	Voice       *string  `json:"voice"`
}

// Limits 是导入文件的体量上限。
// MaxBytes 针对文件原始字节数，由读取文件的一侧在 Parse 之前检查（Validate 看不到原文）。
type Limits struct {
	MaxBytes      int64
	MaxNodes      int
	MaxAssets     int
	MaxAssetBytes int64
}

// DefaultLimits 返回默认上限：文件 10 MB、节点 2000、素材 200 个、素材总大小 1 GB。
func DefaultLimits() Limits {
	return Limits{
		MaxBytes:      10 * 1024 * 1024,
		MaxNodes:      2000,
		MaxAssets:     200,
		MaxAssetBytes: 1 << 30,
	}
}

// Parse 解析画布导出文件。
//
// 数字用 json.Number 保留原样，所以 createdAt 这类毫秒时间戳重新序列化后不会变成科学计数法。
// 返回 ErrInvalid（不是本系统的画布文件）或 ErrTooNew（版本比当前支持的高）；
// 引用关系和体量限制由 Validate 检查。
func Parse(raw []byte) (*Document, error) {
	// 1. 先只读 format 和 version：高版本文件的正文结构可能已经变了，
	//    不能让后面的完整解码把它误报成"不合法"，而应当提示"版本过高"
	var head struct {
		Format  string `json:"format"`
		Version int    `json:"version"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, fmt.Errorf("%w: 文件不是画布导出文件", ErrInvalid)
	}
	if head.Format != Format {
		return nil, fmt.Errorf("%w: 文件不是画布导出文件", ErrInvalid)
	}
	if head.Version > CurrentVersion {
		return nil, fmt.Errorf("%w: 文件版本为 %d，当前只支持到 %d，请先升级应用", ErrTooNew, head.Version, CurrentVersion)
	}
	if head.Version < 1 {
		return nil, fmt.Errorf("%w: 缺少有效的文件版本号", ErrInvalid)
	}

	// 2. 完整解码：UseNumber 让数字保持原样；JSON 之后不允许再有内容
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc Document
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%w: 画布内容结构不对（节点和连线必须是对象）", ErrInvalid)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: 文件末尾有多余内容", ErrInvalid)
	}

	// 3. null 或缺省的 assets / characters 规整成空 map，后续代码可以直接读写
	if doc.Assets == nil {
		doc.Assets = map[string]AssetInfo{}
	}
	if doc.Characters == nil {
		doc.Characters = map[string]CharacterInfo{}
	}
	return &doc, nil
}

// Validate 检查文档的内容：素材哈希格式、体量上限，以及节点和角色卡里的每个引用都能在文件内找到。
// 引用不全返回 ErrInvalid，超出体量返回 ErrLimit。
func (d *Document) Validate(l Limits) error {
	// 1. 体量上限放在最前面：超大文件不必再逐项检查
	if err := d.CheckLimits(l); err != nil {
		return err
	}
	// 2. assets 的键必须是 64 位小写十六进制
	for hash := range d.Assets {
		if !isHash(hash) {
			return fmt.Errorf("%w: 素材编号 %q 不是 64 位小写十六进制哈希", ErrInvalid, hash)
		}
	}
	// 3. 节点：每个都必须是对象；它引用的哈希必须在 assets 里，角色卡引用必须在 characters 里
	for i, node := range d.Canvas.Nodes {
		if err := d.checkNode(i, node); err != nil {
			return err
		}
	}
	// 4. 角色卡：主图必填，主图、补充图、声音都必须在 assets 里
	return d.checkCharacters()
}

// CheckLimits 检查节点数、素材数和素材总大小是否超出上限（ErrLimit），素材大小为负时返回 ErrInvalid。
// 不查 MaxBytes（看不到原文）。Validate 第一步就调它；导出也用它，保证导出的文件一定能被导入。
func (d *Document) CheckLimits(l Limits) error {
	if len(d.Canvas.Nodes) > l.MaxNodes {
		return fmt.Errorf("%w: 节点数 %d 超过上限 %d", ErrLimit, len(d.Canvas.Nodes), l.MaxNodes)
	}
	if len(d.Assets) > l.MaxAssets {
		return fmt.Errorf("%w: 素材数 %d 超过上限 %d", ErrLimit, len(d.Assets), l.MaxAssets)
	}
	// 负数会拉低总量、让文件绕过上限，先单独扫一遍拒绝，
	// 这样同一份文件的结果不取决于 map 先遍历到负数还是先超限
	for hash, info := range d.Assets {
		if info.ByteSize < 0 {
			return fmt.Errorf("%w: 素材 %s 的大小不能为负", ErrInvalid, shortHash(hash))
		}
	}
	var total int64
	for _, info := range d.Assets {
		// 先比较剩余额度再累加：total 不超过 MaxAssetBytes，用减法比较不会整数溢出，
		// 结果也不依赖 map 的遍历顺序
		if info.ByteSize > l.MaxAssetBytes-total {
			return fmt.Errorf("%w: 素材总大小超过上限 %d 字节", ErrLimit, l.MaxAssetBytes)
		}
		total += info.ByteSize
	}
	return nil
}

// checkNode 检查第 i 个节点的素材引用和角色卡引用。
func (d *Document) checkNode(i int, node map[string]any) error {
	if node == nil {
		return fmt.Errorf("%w: 第 %d 个节点不是对象", ErrInvalid, i+1)
	}
	id, _ := node["id"].(string)
	for _, h := range hashRefs(node) {
		if _, ok := d.Assets[h]; !ok {
			return fmt.Errorf("%w: 节点 %q 引用的素材 %s 不在文件的素材清单里", ErrInvalid, id, shortHash(h))
		}
	}
	if ref, ok := characterRef(node); ok {
		if _, exists := d.Characters[ref]; !exists {
			return fmt.Errorf("%w: 节点 %q 引用的角色卡 %q 不在文件里", ErrInvalid, id, ref)
		}
	}
	return nil
}

// checkCharacters 检查每张角色卡的素材引用。按编号排序遍历，让多处出错时报错稳定。
func (d *Document) checkCharacters() error {
	refs := make([]string, 0, len(d.Characters))
	for ref := range d.Characters {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	for _, ref := range refs {
		c := d.Characters[ref]
		if c.Avatar == "" {
			return fmt.Errorf("%w: 角色卡 %q 缺少主图", ErrInvalid, ref)
		}
		all := append([]string{c.Avatar}, c.Extras...)
		if c.Voice != nil {
			all = append(all, *c.Voice)
		}
		for _, h := range all {
			if _, ok := d.Assets[h]; !ok {
				return fmt.Errorf("%w: 角色卡 %q 引用的素材 %s 不在文件的素材清单里", ErrInvalid, ref, shortHash(h))
			}
		}
	}
	return nil
}

// hashRefs 取出一个节点里出现的所有素材哈希引用（含重复，不校验格式）：
// data.assetHash、data.outputs[].assetHash、data.params.images / videos / audios 的每一项、
// data.paramAssets 的键、data.scene.thumbHash。
// 校验、导出后的扫描和导入时的改写都靠它统一找引用，新增引用位置只改这里。
func hashRefs(node map[string]any) []string {
	data, _ := node["data"].(map[string]any)
	if data == nil {
		return nil
	}
	var refs []string
	refs = appendString(refs, data["assetHash"])
	outputs, _ := data["outputs"].([]any)
	for _, o := range outputs {
		if m, ok := o.(map[string]any); ok {
			refs = appendString(refs, m["assetHash"])
		}
	}
	params, _ := data["params"].(map[string]any)
	for _, key := range []string{"images", "videos", "audios"} {
		list, _ := params[key].([]any)
		for _, v := range list {
			refs = appendString(refs, v)
		}
	}
	// paramAssets 的键是哈希；map 遍历顺序不定，排序后追加，让报错和后续改写的顺序稳定
	paramAssets, _ := data["paramAssets"].(map[string]any)
	keys := make([]string, 0, len(paramAssets))
	for k := range paramAssets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	refs = append(refs, keys...)
	scene, _ := data["scene"].(map[string]any)
	return appendString(refs, scene["thumbHash"])
}

// ReferencedSets 返回画布节点真正用到的素材哈希和角色卡编号，供导入只复制、只建这些（设计 §8.4）：
// 素材 = 各节点的 hashRefs + 被引用角色卡的主图、补充图、声音；角色卡 = 节点上的 characterRef。
// 只保留在 assets / characters 里存在的条目（Validate 通过后两者一致），都去重；
// 哈希按字典序，编号按长度再按字典序（c10 排在 c9 之后，与导出时的编号顺序一致）。没有引用时返回空切片。
func ReferencedSets(doc *Document) (hashes []string, characterRefs []string) {
	seenHash := map[string]bool{}
	addHash := func(h string) {
		if _, ok := doc.Assets[h]; ok && !seenHash[h] {
			seenHash[h] = true
			hashes = append(hashes, h)
		}
	}
	seenRef := map[string]bool{}
	characterRefs = []string{}
	for _, node := range doc.Canvas.Nodes {
		for _, h := range hashRefs(node) {
			addHash(h)
		}
		if ref, ok := characterRef(node); ok && !seenRef[ref] {
			if _, exists := doc.Characters[ref]; exists {
				seenRef[ref] = true
				characterRefs = append(characterRefs, ref)
			}
		}
	}
	// 被引用的角色卡要能重建，它们的素材也算被引用
	for _, ref := range characterRefs {
		c := doc.Characters[ref]
		addHash(c.Avatar)
		for _, h := range c.Extras {
			addHash(h)
		}
		if c.Voice != nil {
			addHash(*c.Voice)
		}
	}
	if hashes == nil {
		hashes = []string{}
	}
	sort.Strings(hashes)
	sort.Slice(characterRefs, func(i, j int) bool {
		a, b := characterRefs[i], characterRefs[j]
		if len(a) != len(b) {
			return len(a) < len(b)
		}
		return a < b
	})
	return hashes, characterRefs
}

// appendString 在 v 是字符串时把它追加到 refs，否则原样返回。
func appendString(refs []string, v any) []string {
	if s, ok := v.(string); ok {
		return append(refs, s)
	}
	return refs
}

// characterRef 取出节点引用的角色卡局部编号（data.characterRef）。
func characterRef(node map[string]any) (string, bool) {
	data, _ := node["data"].(map[string]any)
	ref, ok := data["characterRef"].(string)
	return ref, ok
}

// isHash 判断 s 是否是 64 位小写十六进制（SHA-256）。
func isHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// shortHash 截取哈希前 8 位用于错误提示，避免把超长字符串原样回显给用户。
func shortHash(h string) string {
	if len(h) > 8 {
		return h[:8] + "…"
	}
	return h
}
