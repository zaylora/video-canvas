package drama_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"

	. "video-canvas/internal/drama"
)

func docxFile(t *testing.T, name, document string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	f, err := w.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte(document)); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestParseSource(t *testing.T) {
	document := `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>第一</w:t></w:r><w:r><w:t>集</w:t></w:r></w:p><w:p><w:r><w:t>林川 &amp; 阿月</w:t></w:r></w:p><w:tbl><w:tr><w:tc><w:p><w:r><w:t>表格正文</w:t></w:r></w:p></w:tc></w:tr></w:tbl></w:body></w:document>`
	tests := []struct {
		name, filename string
		data           []byte
		want           string
		wantErr        error
	}{
		{"文本", "script.txt", []byte("  原文\r\n下一段  "), "原文\n下一段", nil},
		{"Markdown保留原文", "script.MD", []byte("\xef\xbb\xbf# 第一集\n正文"), "# 第一集\n正文", nil},
		{"Word正文分段", "script.docx", docxFile(t, "word/document.xml", document), "第一集\n林川 & 阿月\n表格正文", nil},
		{"空文件", "empty.txt", nil, "", ErrSourceInvalid},
		{"空白", "empty.md", []byte("\xef\xbb\xbf \t\r\n"), "", ErrSourceInvalid},
		{"非法UTF8", "bad.txt", []byte{0xff}, "", ErrSourceInvalid},
		{"非文本控制符", "bad.txt", []byte("原\x00文"), "", ErrSourceInvalid},
		{"上限按汉字", "large.txt", []byte(strings.Repeat("汉", 300000)), strings.Repeat("汉", 300000), nil},
		{"超过汉字上限", "large.txt", []byte(strings.Repeat("汉", 300001)), "", ErrSourceTooLong},
		{"未支持格式", "script.pdf", []byte("正文"), "", ErrSourceInvalid},
		{"无扩展名", "script", []byte("正文"), "", ErrSourceInvalid},
		{"损坏Word", "bad.docx", []byte("正文"), "", ErrSourceInvalid},
		{"Word缺正文", "bad.docx", docxFile(t, "word/header1.xml", document), "", ErrSourceInvalid},
		{"WordXML损坏", "bad.docx", docxFile(t, "word/document.xml", "<w:document>"), "", ErrSourceInvalid},
		{"Word空正文", "empty.docx", docxFile(t, "word/document.xml", `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p/></w:body></w:document>`), "", ErrSourceInvalid},
		{"Word超长", "large.docx", docxFile(t, "word/document.xml", `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>`+strings.Repeat("汉", 300001)+`</w:t></w:r></w:p></w:body></w:document>`), "", ErrSourceTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSource(tt.filename, tt.data)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("错误 = %v，期望 %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("正文长度 = %d，期望 %d", len(got), len(tt.want))
			}
		})
	}
}

func TestDecodeOutput(t *testing.T) {
	tests := []struct {
		name, step, data string
		valid            bool
	}{
		{"研读", "analyze", `{"source_kind":"novel","summary":"故事梗概","characters_md":"人物表"}`, true},
		{"大纲", "outline", `{"outline_md":"大纲","characters_md":"人物","episodes":[{"no":1,"title":"初见","synopsis":"故事开端","script_md":""}]}`, true},
		{"剧本", "scripts", `{"script_md":"第一场：街头，日，外。"}`, true},
		{"拆解", "breakdown", `{"assets":[{"type":"character","name":"林川","look":"童年","aliases":["小川"],"episodes":[1,2],"description":"短发少年","voice_prompt":"清亮的少年声线"}]}`, true},
		{"全集资产", "breakdown", `{"assets":[{"type":"scene","name":"街头","look":"","aliases":[],"episodes":[],"description":"晨光中的街头","voice_prompt":""}]}`, true},
		{"资产提示词", "asset_prompts", `{"prompts":[{"node_id":"n_a","prompt":"晨光中的街头"}]}`, true},
		{"空输出", "analyze", `{}`, false},
		{"非法JSON", "scripts", `{"script_md":`, false},
		{"拒绝Markdown围栏", "scripts", "```json\n{\"script_md\":\"正文\"}\n```", false},
		{"拒绝尾随对象", "scripts", `{"script_md":"正文"}{}`, false},
		{"拒绝未知字段", "scripts", `{"script_md":"正文","other":true}`, false},
		{"拒绝大小写字段", "scripts", `{"Script_MD":"正文"}`, false},
		{"拒绝重复字段", "scripts", `{"script_md":"正文","script_md":"另一份"}`, false},
		{"拒绝空剧本", "scripts", `{"script_md":" \n "}`, false},
		{"拒绝null", "scripts", `{"script_md":null}`, false},
		{"拒绝非法来源", "analyze", `{"source_kind":"pdf","summary":"梗概","characters_md":"人物"}`, false},
		{"拒绝缺失人物", "analyze", `{"source_kind":"script","summary":"梗概"}`, false},
		{"拒绝空集列表", "outline", `{"outline_md":"大纲","characters_md":"人物","episodes":[]}`, false},
		{"拒绝不连续集号", "outline", `{"outline_md":"大纲","characters_md":"人物","episodes":[{"no":2,"title":"初见","synopsis":"梗概","script_md":""}]}`, false},
		{"拒绝缺失空值字段", "outline", `{"outline_md":"大纲","characters_md":"人物","episodes":[{"no":1,"title":"初见","synopsis":"梗概"}]}`, false},
		{"拒绝非法资产类型", "breakdown", `{"assets":[{"type":"audio","name":"音乐","look":"","aliases":[],"episodes":[1],"description":"音乐","voice_prompt":""}]}`, false},
		{"拒绝重复集号", "breakdown", `{"assets":[{"type":"prop","name":"钥匙","look":"","aliases":[],"episodes":[1,1],"description":"铜钥匙","voice_prompt":""}]}`, false},
		{"拒绝零集号", "breakdown", `{"assets":[{"type":"scene","name":"街头","look":"","aliases":[],"episodes":[0],"description":"街头","voice_prompt":""}]}`, false},
		{"拒绝null数组", "breakdown", `{"assets":[{"type":"scene","name":"街头","look":"","aliases":null,"episodes":[],"description":"街头","voice_prompt":""}]}`, false},
		{"同名同造型交由画布合并", "breakdown", `{"assets":[{"type":"scene","name":"Street","look":"","aliases":[],"episodes":[1],"description":"街头","voice_prompt":""},{"type":"scene","name":" street ","look":"","aliases":[],"episodes":[2],"description":"街头","voice_prompt":""}]}`, true},
		{"拒绝重复节点提示词", "asset_prompts", `{"prompts":[{"node_id":"n_a","prompt":"图像"},{"node_id":"n_a","prompt":"图像"}]}`, false},
		{"拒绝未知步骤", "storyboard", `{}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := DecodeOutput(tt.step, []byte(tt.data))
			if tt.valid {
				if err != nil || output == nil {
					t.Fatalf("期望有效输出，得到 %v / %v", output, err)
				}
			} else if !errors.Is(err, ErrOutputInvalid) {
				t.Fatalf("期望 ErrOutputInvalid，得到 %v", err)
			}
		})
	}
}
