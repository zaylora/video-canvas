package canvasgraph

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// 角色卡人物数据的长度上限，和前端角色详情页一致。
const (
	characterNameMax = 64
	characterTextMax = 500
)

// characterText 取一个人物字段：去掉首尾空白并检查长度；字段为 nil 返回 ok=false。
func characterText(field string, v *string, limit int) (string, bool, error) {
	if v == nil {
		return "", false, nil
	}
	text := strings.TrimSpace(*v)
	if utf8.RuneCountInString(text) > limit {
		return "", true, fmt.Errorf("角色的%s最多 %d 字", field, limit)
	}
	return text, true, nil
}

// newAgentCharacter 按 Agent 给的人物数据构造角色卡的 data.character：名字必填，形象不写是「基础形象」，
// 补充图为空、声音为 null（声音只能由用户在详情页生成或上传），音色描述有才写。结构和短剧拆解建的角色卡一致。
func newAgentCharacter(op *CharacterOp) (map[string]any, error) {
	if op == nil {
		return nil, errors.New("新建角色卡要写 character，至少给出角色的名字")
	}
	name, _, err := characterText("名字", op.Name, characterNameMax)
	if err != nil {
		return nil, err
	}
	if name == "" {
		return nil, errors.New("新建角色卡要写角色的名字（character.name）")
	}
	meta := map[string]any{"name": name, "look": defaultCharacterLook, "description": "", "extras": []any{}, "voice": nil}
	if err := mergeCharacter(meta, op); err != nil {
		return nil, err
	}
	return meta, nil
}

// patchAgentCharacter 只改给了的人物字段，补充图、声音等其余数据原样保留；名字不能改成空。
func patchAgentCharacter(n Node, op *CharacterOp) error {
	meta, _ := n.Data()["character"].(map[string]any)
	if meta == nil {
		meta = map[string]any{"look": defaultCharacterLook, "extras": []any{}, "voice": nil}
	}
	if op.Name != nil && strings.TrimSpace(*op.Name) == "" {
		return errors.New("角色的名字不能改成空")
	}
	if err := mergeCharacter(meta, op); err != nil {
		return err
	}
	n.Data()["character"] = meta
	return nil
}

// mergeCharacter 把给了的字段写进 meta：形象改成空时回到「基础形象」，音色描述改成空时删掉这个键。
func mergeCharacter(meta map[string]any, op *CharacterOp) error {
	fields := []struct {
		key, label string
		value      *string
		limit      int
	}{
		{"name", "名字", op.Name, characterNameMax},
		{"look", "形象", op.Look, characterNameMax},
		{"description", "描述", op.Description, characterTextMax},
		{"voicePrompt", "音色描述", op.VoicePrompt, characterTextMax},
	}
	for _, f := range fields {
		text, ok, err := characterText(f.label, f.value, f.limit)
		if err != nil {
			return err
		}
		switch {
		case !ok:
		case f.key == "look" && text == "":
			meta["look"] = defaultCharacterLook
		case f.key == "voicePrompt" && text == "":
			delete(meta, "voicePrompt")
		default:
			meta[f.key] = text
		}
	}
	return nil
}

// characterLabel 是角色卡的默认标题「名字 · 形象」，和短剧拆解建的角色卡一致。
func characterLabel(meta map[string]any) string {
	name, _ := meta["name"].(string)
	look, _ := meta["look"].(string)
	if look == "" {
		return name
	}
	return name + " · " + look
}
