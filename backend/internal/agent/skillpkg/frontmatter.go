package skillpkg

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
)

// ParseFrontmatter 解析 SKILL.md 头部「---」包起来的 YAML，返回字段、正文和错误。
// 支持折行（>、|）、列表和嵌套；遇到常见的「值里有未加引号的冒号」会自动加引号重试。
// 没有头部、没有结束分隔线、YAML 无法解析或头部不是映射时返回错误。
func ParseFrontmatter(raw []byte) (head map[string]any, body string, err error) {
	text := strings.TrimPrefix(string(raw), "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		return nil, "", errors.New("SKILL.md 缺少以 --- 开头的 frontmatter")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t") == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, "", errors.New("frontmatter 没有以 --- 结束")
	}
	yamlText := strings.Join(lines[1:end], "\n")
	body = strings.Join(lines[end+1:], "\n")

	head, err = unmarshalMap(yamlText)
	if err != nil {
		// 兜底：把「key: 值里有: 冒号」的值加上引号再试一次
		fixed := quoteColonValues(yamlText)
		if fixed == yamlText {
			return nil, "", err
		}
		var err2 error
		if head, err2 = unmarshalMap(fixed); err2 != nil {
			return nil, "", err
		}
	}
	return head, body, nil
}

// unmarshalMap 把 YAML 解成 map；空头部得到空 map，非映射返回错误。
func unmarshalMap(s string) (map[string]any, error) {
	if strings.TrimSpace(s) == "" {
		return map[string]any{}, nil
	}
	var v any
	if err := yaml.NewDecoder(bytes.NewReader([]byte(s))).Decode(&v); err != nil {
		return nil, fmt.Errorf("frontmatter 不是合法的 YAML：%w", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("frontmatter 必须是 key: value 形式的映射")
	}
	return m, nil
}

// topKeyRe 匹配顶层「key: value」行。
var topKeyRe = regexp.MustCompile(`^([A-Za-z_][\w-]*):[ \t]+(\S.*)$`)

// quoteColonValues 给值里含「: 」且没有加引号、不是块标量/流式集合的顶层行加上双引号。
func quoteColonValues(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		m := topKeyRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		v := strings.TrimSpace(m[2])
		if !strings.Contains(v, ": ") {
			continue
		}
		switch v[0] {
		case '"', '\'', '[', '{', '|', '>', '&', '*', '!':
			continue
		}
		v = strings.ReplaceAll(v, `\`, `\\`)
		v = strings.ReplaceAll(v, `"`, `\"`)
		lines[i] = m[1] + `: "` + v + `"`
	}
	return strings.Join(lines, "\n")
}
