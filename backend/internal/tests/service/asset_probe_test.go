package service_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
	. "video-canvas/internal/service"
)

// ---- 测试素材构造 ----

func assetTestPNG(t testing.TB, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func assetTestJPEG(t testing.TB, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func assetTestGIF(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{color.Black, color.White})
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func be32(v uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, v); return b }
func be64(v uint64) []byte { b := make([]byte, 8); binary.BigEndian.PutUint64(b, v); return b }

// mp4Box 构造一个 ISO BMFF box。
func mp4Box(typ string, payload ...[]byte) []byte {
	body := bytes.Join(payload, nil)
	return append(append(be32(uint32(8+len(body))), typ...), body...)
}

// mp4Mvhd 构造 mvhd；version 1 用 64 位时长。
func mp4Mvhd(version byte, timescale uint32, duration uint64) []byte {
	var p []byte
	if version == 0 {
		p = append(p, 0, 0, 0, 0)         // version/flags
		p = append(p, make([]byte, 8)...) // creation/modification
		p = append(p, be32(timescale)...)
		p = append(p, be32(uint32(duration))...)
		p = append(p, make([]byte, 80)...) // rate / volume / matrix ...
	} else {
		p = append(p, 1, 0, 0, 0)
		p = append(p, make([]byte, 16)...)
		p = append(p, be32(timescale)...)
		p = append(p, be64(duration)...)
		p = append(p, make([]byte, 80)...)
	}
	return mp4Box("mvhd", p)
}

// mp4Tkhd 构造 tkhd，宽高是 16.16 定点数。
func mp4Tkhd(version byte, w, h uint32) []byte {
	var p []byte
	if version == 0 {
		p = append(p, 0, 0, 0, 3)
		p = append(p, make([]byte, 72)...) // 到 matrix 结束共 76 字节（含前 4 字节）
	} else {
		p = append(p, 1, 0, 0, 3)
		p = append(p, make([]byte, 84)...) // 到 matrix 结束共 88 字节
	}
	p = append(p, be32(w<<16)...)
	p = append(p, be32(h<<16)...)
	return mp4Box("tkhd", p)
}

// assetTestMP4 构造 moov 在文件末尾（非 faststart）的最小 mp4：音频轨（0x0）+ 视频轨。
func assetTestMP4(version byte, timescale uint32, duration uint64, w, h uint32) []byte {
	ftyp := mp4Box("ftyp", []byte("isom"), be32(512), []byte("isom"), []byte("mp41"))
	mdat := mp4Box("mdat", make([]byte, 1000))
	moov := mp4Box("moov",
		mp4Mvhd(version, timescale, duration),
		mp4Box("trak", mp4Tkhd(version, 0, 0)),
		mp4Box("trak", mp4Tkhd(version, w, h)),
	)
	return bytes.Join([][]byte{ftyp, mdat, moov}, nil)
}

// assetTestWebP 构造只含头部的 webp。
func assetTestWebP(kind string, w, h int) []byte {
	b := make([]byte, 40)
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], 32)
	copy(b[8:], "WEBP")
	switch kind {
	case "VP8 ":
		copy(b[12:], "VP8 ")
		b[23], b[24], b[25] = 0x9d, 0x01, 0x2a
		binary.LittleEndian.PutUint16(b[26:], uint16(w))
		binary.LittleEndian.PutUint16(b[28:], uint16(h))
	case "VP8L":
		copy(b[12:], "VP8L")
		b[20] = 0x2f
		binary.LittleEndian.PutUint32(b[21:], uint32(w-1)|uint32(h-1)<<14)
	case "VP8X":
		copy(b[12:], "VP8X")
		b[24], b[25], b[26] = byte(w-1), byte((w-1)>>8), byte((w-1)>>16)
		b[27], b[28], b[29] = byte(h-1), byte((h-1)>>8), byte((h-1)>>16)
	}
	return b
}

// ---- 测试 ----

func TestSniffAssetMime(t *testing.T) {
	pad := func(s string) []byte { return append([]byte(s), make([]byte, 32)...) }
	tests := []struct {
		name string
		head []byte
		want string // 空表示不支持
	}{
		{"png", assetTestPNG(t, 2, 2), "image/png"},
		{"jpeg", assetTestJPEG(t, 2, 2), "image/jpeg"},
		{"gif", assetTestGIF(t, 2, 2), "image/gif"},
		{"webp", assetTestWebP("VP8X", 2, 2), "image/webp"},
		{"mp4 isom", assetTestMP4(0, 1000, 1000, 2, 2), "video/mp4"},
		{"mov", append(be32(24), []byte("ftypqt  \x00\x00\x00\x00qt  ")...), "video/quicktime"},
		{"m4a", append(be32(24), []byte("ftypM4A \x00\x00\x00\x00M4A ")...), "audio/mp4"},
		{"heic 不支持", append(be32(24), []byte("ftypheic\x00\x00\x00\x00heic")...), ""},
		{"avif 不支持", append(be32(24), []byte("ftypavif\x00\x00\x00\x00avif")...), ""},
		{"webm", pad("\x1a\x45\xdf\xa3"), "video/webm"},
		{"mp3 带 ID3", pad("ID3\x04"), "audio/mpeg"},
		{"mp3 裸帧", pad("\xff\xfb\x90\x00"), "audio/mpeg"},
		{"aac adts", pad("\xff\xf1\x50\x80"), "audio/aac"},
		{"wav", pad("RIFF\x00\x00\x00\x00WAVE"), "audio/wav"},
		{"ogg", pad("OggS\x00"), "audio/ogg"},
		{"flac", pad("fLaC"), "audio/flac"},
		{"html 伪装", pad("<!DOCTYPE html><script>alert(1)</script>"), ""},
		{"svg 不支持", pad(`<svg xmlns="http://www.w3.org/2000/svg">`), ""},
		{"exe", pad("MZ\x90\x00"), ""},
		{"纯文本", pad("hello world"), ""},
		{"空", nil, ""},
		{"过短的 riff", []byte("RIFF"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			head := tt.head
			if len(head) > 32 {
				head = head[:32]
			}
			got, ok := SniffAssetMime(head)
			if tt.want == "" {
				if ok {
					t.Fatalf("期望不支持，实际识别为 %q", got)
				}
				return
			}
			if !ok || got != tt.want {
				t.Fatalf("期望 %q，实际 %q（ok=%v）", tt.want, got, ok)
			}
		})
	}
}

func TestAssetMimeExtCoversKinds(t *testing.T) {
	// 白名单里每个 mime 都必须属于 image / video / audio 三类之一
	for mime := range AssetMimeExt {
		switch AssetKindOfMime(mime) {
		case "image", "video", "audio":
		default:
			t.Errorf("%s 不属于允许的种类", mime)
		}
	}
	if AssetKindOfMime("application/pdf") != "application" || AssetKindOfMime("noslash") != "" {
		t.Error("assetKindOfMime 解析异常")
	}
}

func TestProbeAssetMeta_Image(t *testing.T) {
	tests := []struct {
		name    string
		mime    string
		data    []byte
		w, h    int
		wantErr bool
	}{
		{"png", "image/png", assetTestPNG(t, 30, 20), 30, 20, false},
		{"jpeg", "image/jpeg", assetTestJPEG(t, 64, 48), 64, 48, false},
		{"gif", "image/gif", assetTestGIF(t, 7, 9), 7, 9, false},
		{"webp 有损", "image/webp", assetTestWebP("VP8 ", 320, 240), 320, 240, false},
		{"webp 无损", "image/webp", assetTestWebP("VP8L", 100, 50), 100, 50, false},
		{"webp 扩展", "image/webp", assetTestWebP("VP8X", 1920, 1080), 1920, 1080, false},
		{"损坏的 png 元数据留 0", "image/png", []byte("\x89PNG\r\n\x1a\ngarbage"), 0, 0, true},
		{"截断的 webp 元数据留 0", "image/webp", []byte("RIFF\x00\x00\x00\x00WEBPVP8 "), 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := ProbeAssetMeta(tt.mime, bytes.NewReader(tt.data), int64(len(tt.data)))
			if m.Width != tt.w || m.Height != tt.h || m.DurationMs != 0 {
				t.Fatalf("期望 %dx%d，实际 %+v", tt.w, tt.h, m)
			}
		})
	}
}

func TestParseMP4Meta(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want AssetMeta
	}{
		{"v0 moov 在末尾", assetTestMP4(0, 1000, 2500, 640, 360), AssetMeta{Width: 640, Height: 360, DurationMs: 2500}},
		{"v1 64 位时长", assetTestMP4(1, 90000, 90000*3+45000, 1920, 1080), AssetMeta{Width: 1920, Height: 1080, DurationMs: 3500}},
		{"timescale 为 0 时长留 0", assetTestMP4(0, 0, 100, 16, 9), AssetMeta{Width: 16, Height: 9, DurationMs: 0}},
		{"没有 moov", mp4Box("ftyp", []byte("isom"), be32(0), []byte("isom")), AssetMeta{}},
		{"空文件", nil, AssetMeta{}},
		{"随机垃圾", bytes.Repeat([]byte{0xab}, 200), AssetMeta{}},
		{"box 大小超出文件（截断）", func() []byte { b := assetTestMP4(0, 1000, 2500, 640, 360); return b[:len(b)-30] }(), AssetMeta{}},
		{"box 大小小于头长度", append(mp4Box("ftyp", []byte("isom")), 0, 0, 0, 4, 'm', 'o', 'o', 'v'), AssetMeta{}},
		{"音频 mp4 只有时长", bytes.Join([][]byte{
			mp4Box("ftyp", []byte("M4A "), be32(0), []byte("M4A ")),
			mp4Box("moov", mp4Mvhd(0, 44100, 44100*10), mp4Box("trak", mp4Tkhd(0, 0, 0))),
		}, nil), AssetMeta{Width: 0, Height: 0, DurationMs: 10000}},
		{"largesize 头", func() []byte {
			// mdat 用 64 位 size（size=1）
			mdatBody := make([]byte, 100)
			mdat := append(append(be32(1), "mdat"...), be64(uint64(16+len(mdatBody)))...)
			mdat = append(mdat, mdatBody...)
			moov := mp4Box("moov", mp4Mvhd(0, 1000, 1500), mp4Box("trak", mp4Tkhd(0, 320, 240)))
			return bytes.Join([][]byte{mp4Box("ftyp", []byte("isom"), be32(0), []byte("isom")), mdat, moov}, nil)
		}(), AssetMeta{Width: 320, Height: 240, DurationMs: 1500}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseMP4Meta(bytes.NewReader(tt.data), int64(len(tt.data)))
			if got != tt.want {
				t.Fatalf("期望 %+v，实际 %+v", tt.want, got)
			}
		})
	}
}

func TestParseMP4Meta_Malicious(t *testing.T) {
	// 大量零长度 box 与深层嵌套都不能导致死循环或 panic
	t.Run("size=0 的 box 延伸到结尾不会死循环", func(t *testing.T) {
		data := append(append(be32(0), "free"...), make([]byte, 64)...)
		ParseMP4Meta(bytes.NewReader(data), int64(len(data)))
	})
	t.Run("海量小 box 受预算限制", func(t *testing.T) {
		data := bytes.Repeat(mp4Box("free"), 100000)
		ParseMP4Meta(bytes.NewReader(data), int64(len(data)))
	})
	t.Run("moov 内嵌自身", func(t *testing.T) {
		inner := mp4Box("moov")
		for i := 0; i < 50; i++ {
			inner = mp4Box("moov", inner)
		}
		ParseMP4Meta(bytes.NewReader(inner), int64(len(inner)))
	})
}

func TestParseWebPSize_Invalid(t *testing.T) {
	bad := assetTestWebP("VP8 ", 10, 10)
	bad[23] = 0 // 起始码错误
	for name, b := range map[string][]byte{
		"短数据":      []byte("RIFF"),
		"非 webp":   append([]byte("RIFF\x00\x00\x00\x00WAVEfmt "), make([]byte, 20)...),
		"起始码错误":    bad,
		"未知 chunk": append(append([]byte("RIFF\x00\x00\x00\x00WEBPXXXX"), make([]byte, 20)...), 0),
	} {
		if w, h := ParseWebPSize(b); w != 0 || h != 0 {
			t.Errorf("%s：期望 0x0，实际 %dx%d", name, w, h)
		}
	}
}

func TestSanitizeAssetFileName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"普通文件名", "photo.png", "photo.png"},
		{"中文文件名保留", "海报 最终版.jpg", "海报 最终版.jpg"},
		{"unix 路径只留文件名", "/etc/passwd", "passwd"},
		{"穿越路径只留文件名", "../../secret.png", "secret.png"},
		{"windows 路径只留文件名", `C:\Users\x\a.mp4`, "a.mp4"},
		{"控制字符被删", "a\x00b\nc.png", "abc.png"},
		{"RTL 覆盖符被删", "evil\u202egnp.exe", "evilgnp.exe"},
		{"非法字符替换", `a<b>c:d"e|f?g*.png`, "a_b_c_d_e_f_g_.png"},
		{"首尾点和空格去掉", "  ..hidden.png. ", "hidden.png"},
		{"空串", "", ""},
		{"只有点", "..", ""},
		{"只有斜杠", "///", ""},
		{"全是控制字符", "\x01\x02", ""},
		{"超长保留扩展名", strings.Repeat("长", 300) + ".png", strings.Repeat("长", AssetMaxNameRunes-4) + ".png"},
		{"超长无扩展名", strings.Repeat("a", 300), strings.Repeat("a", AssetMaxNameRunes)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeAssetFileName(tt.in); got != tt.want {
				t.Fatalf("期望 %q，实际 %q", tt.want, got)
			}
		})
	}
}

func TestNewAssetUUID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		u := NewAssetUUID()
		if len(u) != 36 || u[14] != '4' || seen[u] {
			t.Fatalf("uuid 格式或唯一性异常：%q", u)
		}
		seen[u] = true
	}
}
