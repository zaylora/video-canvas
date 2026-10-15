package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// aiGenKeyTries 是生成模型 key 时遇到重名最多重试的次数；12 位随机十六进制几乎不会重名，这里只是兜底。
const aiGenKeyTries = 5

// withModelKey 新建模型时补上 key：正文里没有 key、或 key 为空白，就生成一个唯一的并写回正文；
// 填了 key 的原样返回（格式与重名由后续校验处理）。正文不是 JSON 对象时原样返回，交给后续校验报错。
func (s *AIConfigService) withModelKey(ctx context.Context, body []byte) ([]byte, error) {
	trimmed := strings.TrimSpace(string(body))
	if !strings.HasPrefix(trimmed, "{") {
		return body, nil
	}
	var probe struct {
		Key *string `json:"key"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return body, nil
	}
	if probe.Key != nil && strings.TrimSpace(*probe.Key) != "" {
		return body, nil
	}
	key, err := s.newModelKey(ctx)
	if err != nil {
		return nil, err
	}
	return aiSetBodyKey(body, key), nil
}

// newModelKey 生成一个未被占用的模型 key：m- 加 12 位随机十六进制。
// 用户不需要读它（界面上看展示名称），所以不取可读形式；唯一性由主键兜底，这里先查一遍避免白白撞上。
func (s *AIConfigService) newModelKey(ctx context.Context) (string, error) {
	for range aiGenKeyTries {
		raw := make([]byte, 6)
		if _, err := rand.Read(raw); err != nil {
			return "", errcode.ErrInternal.WithMsg("生成模型标识失败")
		}
		key := "m-" + hex.EncodeToString(raw)
		_, err := s.repo.GetModelPointer(ctx, key)
		if errors.Is(err, repository.ErrNotFound) {
			return key, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", errcode.ErrInternal.WithMsg("生成模型标识失败：多次重名，请重试")
}

// aiSetBodyKey 把正文顶层的 key 设为 key，只改这一处文本、不重排其他字段（字段书写顺序就是前端的渲染顺序）：
// 已有顶层 key 就替换它的值，没有就插在开头的 { 之后。正文解析不了时原样返回。
func aiSetBodyKey(body []byte, key string) []byte {
	value := fmt.Sprintf("%q", key)
	dec := json.NewDecoder(bytes.NewReader(body))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return body
	}
	hasMembers := false
	for dec.More() {
		hasMembers = true
		tok, err := dec.Token()
		if err != nil {
			return body
		}
		start := dec.InputOffset()
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return body
		}
		if name, _ := tok.(string); name == "key" {
			end := dec.InputOffset()
			out := append([]byte{}, body[:start]...)
			out = append(out, ": "+value...)
			return append(out, body[end:]...)
		}
	}
	open := bytes.IndexByte(body, '{')
	member := `"key": ` + value
	if hasMembers {
		member += ","
	}
	out := append([]byte{}, body[:open+1]...)
	out = append(out, member...)
	return append(out, body[open+1:]...)
}
