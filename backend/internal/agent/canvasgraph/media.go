package canvasgraph

// 素材种类，与前端 NodeMediaType 和 assets.kind 一致。
const (
	MediaImage = "image"
	MediaVideo = "video"
	MediaAudio = "audio"
)

// MediaRef 是节点上引用的一个素材：只记素材 id、种类和它属于哪一类（成片 / 第几镜的分镜图），
// 文件本身的信息（mime、大小）由调用方按 id 查库。
type MediaRef struct {
	AssetID string // 素材 id（十进制字符串）
	Kind    string // image / video / audio
	Final   bool   // 是不是节点上的成片（data.finalAssetId）
	Shot    int    // >0 表示分镜表第 N 镜（从 1 起）手传的分镜图，旧数据才有的形态
}

// MediaRefs 返回节点引用的全部素材，按「历次产物（旧 → 新）→ 当前版（若不在产物里）→ 成片 → 角色素材 → 分镜手传图」排列。
//
// 为什么要在后端也做一遍：打包下载要按节点归类把文件附上，而画布里的素材引用有五种形态
// （outputs、老节点的 assetId 镜像、finalAssetId、角色卡的补充图 / 声音、分镜旧手传图），
// 少读一种就会在压缩包里静默少文件。规则与前端 readOutputs / activeOutputIdOf 一致：
// 产物非空就只认产物，产物为空时老节点的那份 assetId 当成唯一一版。
func MediaRefs(n Node) []MediaRef {
	d := n.Data()
	c := &mediaCollector{
		out:      make([]MediaRef, 0, 4),
		seen:     make(map[string]bool, 4),
		nodeKind: n.Kind(),
	}
	c.collectOutputs(d)
	c.collectLegacyAsset(d)
	c.collectFinal(d)
	c.collectCharacter(d)
	c.collectShots(d)
	return c.out
}

// mediaCollector 边收集边按素材 id 去重：同一个素材在产物和旧字段里都出现时只留一份。
type mediaCollector struct {
	out      []MediaRef
	seen     map[string]bool
	nodeKind string
}

// add 记一个素材；id 为空或已经收过就跳过，种类不认识时按图片兜底。
func (c *mediaCollector) add(id, kind string, final bool, shot int) {
	if id == "" || id == "0" || c.seen[id] {
		return
	}
	switch kind {
	case MediaImage, MediaVideo, MediaAudio:
	default:
		kind = MediaImage
	}
	c.seen[id] = true
	c.out = append(c.out, MediaRef{AssetID: id, Kind: kind, Final: final, Shot: shot})
}

// addWithNodeKind 在种类没写时用节点种类兜底：视频节点的产物就是视频，图片节点的就是图片。
func (c *mediaCollector) addWithNodeKind(id, kind string) {
	if kind == "" {
		kind = c.nodeKind
	}
	c.add(id, kind, false, 0)
}

// collectOutputs 收历次产物（旧 → 新）。
func (c *mediaCollector) collectOutputs(d map[string]any) {
	for _, o := range readOutputs(d) {
		c.addWithNodeKind(str(o["assetId"]), str(o["mediaType"]))
	}
}

// collectLegacyAsset 收老节点上的 assetId 镜像：没有产物时它就是唯一一版。
func (c *mediaCollector) collectLegacyAsset(d map[string]any) {
	if id := str(d["assetId"]); id != "" {
		c.addWithNodeKind(id, str(d["mediaType"]))
	}
}

// collectFinal 收成片节点的 finalAssetId。
func (c *mediaCollector) collectFinal(d map[string]any) {
	c.add(str(d["finalAssetId"]), MediaVideo, true, 0)
}

// collectCharacter 收角色卡的补充图与绑定的声音。
func (c *mediaCollector) collectCharacter(d map[string]any) {
	ch, ok := d["character"].(map[string]any)
	if !ok {
		return
	}
	extras, _ := ch["extras"].([]any)
	for _, item := range extras {
		if m, ok := item.(map[string]any); ok {
			c.add(str(m["assetId"]), MediaImage, false, 0)
		}
	}
	if voice, ok := ch["voice"].(map[string]any); ok {
		c.add(str(voice["assetId"]), MediaAudio, false, 0)
	}
}

// collectShots 收分镜表里旧的手传分镜图，带上镜号。
func (c *mediaCollector) collectShots(d map[string]any) {
	sb, ok := d["storyboard"].(map[string]any)
	if !ok {
		return
	}
	shots, _ := sb["shots"].([]any)
	for i, item := range shots {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if img, ok := m["image"].(map[string]any); ok {
			c.add(str(img["assetId"]), MediaImage, false, i+1)
		}
	}
}
