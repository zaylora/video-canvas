package canvasgraph

// Shot 是分镜表一行镜头的只读快照，给导出 / 打包这类只需要读的调用方用。
type Shot struct {
	Seq         int     // 镜号，按行序从 1 起（不存库，现算）
	Scene       string  // 画面描述
	Size        string  // 景别
	Camera      string  // 运镜
	Action      string  // 动作 / 走位
	Dialogue    string  // 对白 / 旁白
	Duration    float64 // 时长（秒）
	ImageNodeID string  // 这一镜关联的图片节点
	VideoNodeID string  // 这一镜关联的视频节点
	HasImage    bool    // 行上还挂着旧版本地手传的分镜图
}

// Shots 返回分镜表节点的镜头，按行序；不是分镜表或没有镜头时返回 nil。
func Shots(n Node) []Shot {
	if n.Kind() != KindStoryboard {
		return nil
	}
	raw := existingShots(n)
	if len(raw) == 0 {
		return nil
	}
	out := make([]Shot, 0, len(raw))
	for i, m := range raw {
		_, hasImage := m["image"].(map[string]any)
		out = append(out, Shot{
			Seq:         i + 1,
			Scene:       str(m["scene"]),
			Size:        str(m["shot"]),
			Camera:      str(m["camera"]),
			Action:      str(m["action"]),
			Dialogue:    str(m["dialogue"]),
			Duration:    num(m["duration"]),
			ImageNodeID: str(m["imageNodeId"]),
			VideoNodeID: str(m["videoNodeId"]),
			HasImage:    hasImage,
		})
	}
	return out
}

// CharacterInfo 是角色卡节点的只读快照：设定、声音和补充图张数。
type CharacterInfo struct {
	Name        string // 角色名
	Look        string // 形象名
	Description string // 设定
	VoicePrompt string // 生成声音用的提示词
	VoiceName   string // 绑定的音色名
	Extras      int    // 补充图张数
	HasVoice    bool   // 有没有绑定声音
}

// CharacterOf 读角色卡节点；不是角色卡时返回 false。
func CharacterOf(n Node) (CharacterInfo, bool) {
	if n.Kind() != KindCharacter {
		return CharacterInfo{}, false
	}
	ch, ok := n.Data()["character"].(map[string]any)
	if !ok {
		return CharacterInfo{}, false
	}
	info := CharacterInfo{
		Name:        str(ch["name"]),
		Look:        str(ch["look"]),
		Description: str(ch["description"]),
		VoicePrompt: str(ch["voicePrompt"]),
	}
	if extras, ok := ch["extras"].([]any); ok {
		info.Extras = len(extras)
	}
	if voice, ok := ch["voice"].(map[string]any); ok && str(voice["assetId"]) != "" {
		info.HasVoice = true
		info.VoiceName = str(voice["name"])
	}
	return info, true
}

// Body 返回文本节点的正文；其它种类返回空串。
func Body(n Node) string {
	if n.Kind() != KindScript {
		return ""
	}
	return str(n.Data()["text"])
}
