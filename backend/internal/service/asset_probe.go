package service

import (
	"encoding/binary"
	"image"
	_ "image/gif" // 注册 gif 解码器，供 image.DecodeConfig 使用
	_ "image/jpeg"
	_ "image/png"
	"io"
	"strings"
)

// assetMeta 是从文件内容里探测出来的元数据，探测不到的字段保持 0。
type assetMeta struct {
	Width      int
	Height     int
	DurationMs int64
}

// 素材白名单：真实类型（由 magic bytes 决定）到扩展名的映射。
// 故意不包含 svg / html 等可携带脚本的类型，也不包含 heic / avif 等浏览器支持不佳的类型。
var assetMimeExt = map[string]string{
	"image/png":       ".png",
	"image/jpeg":      ".jpg",
	"image/gif":       ".gif",
	"image/webp":      ".webp",
	"video/mp4":       ".mp4",
	"video/quicktime": ".mov",
	"video/webm":      ".webm",
	"audio/mpeg":      ".mp3",
	"audio/wav":       ".wav",
	"audio/mp4":       ".m4a",
	"audio/ogg":       ".ogg",
	"audio/flac":      ".flac",
	"audio/aac":       ".aac",
}

// assetKindOfMime 返回 mime 所属的素材种类（image / video / audio）。
func assetKindOfMime(mime string) string {
	if i := strings.IndexByte(mime, '/'); i > 0 {
		return mime[:i]
	}
	return ""
}

// sniffAssetMime 根据文件头（magic bytes）判断真实类型，不在白名单内返回 ok=false。
// head 至少给 32 字节才能识别全部格式，不足时按已有内容尽力判断。
func sniffAssetMime(head []byte) (string, bool) {
	switch {
	case hasPrefix(head, "\x89PNG\r\n\x1a\n"):
		return "image/png", true
	case hasPrefix(head, "\xff\xd8\xff"):
		return "image/jpeg", true
	case hasPrefix(head, "GIF87a"), hasPrefix(head, "GIF89a"):
		return "image/gif", true
	case len(head) >= 12 && hasPrefix(head, "RIFF") && string(head[8:12]) == "WEBP":
		return "image/webp", true
	case len(head) >= 12 && hasPrefix(head, "RIFF") && string(head[8:12]) == "WAVE":
		return "audio/wav", true
	case hasPrefix(head, "\x1a\x45\xdf\xa3"):
		// EBML 头：webm / mkv。这里只放行 webm 视频，mkv 与 webm 的区分留给浏览器播放
		return "video/webm", true
	case hasPrefix(head, "OggS"):
		return "audio/ogg", true
	case hasPrefix(head, "fLaC"):
		return "audio/flac", true
	case hasPrefix(head, "ID3"):
		return "audio/mpeg", true
	case len(head) >= 2 && head[0] == 0xff && head[1]&0xf6 == 0xf0:
		// ADTS 帧头（同步字 0xFFF，layer 位为 00），必须先于 MP3 判断
		return "audio/aac", true
	case len(head) >= 2 && head[0] == 0xff && head[1]&0xe0 == 0xe0:
		// MPEG 音频帧同步字（无 ID3 的裸 mp3）
		return "audio/mpeg", true
	case len(head) >= 12 && string(head[4:8]) == "ftyp":
		return sniffFtypMime(string(head[8:12]))
	}
	return "", false
}

func hasPrefix(b []byte, prefix string) bool {
	return len(b) >= len(prefix) && string(b[:len(prefix)]) == prefix
}

// sniffFtypMime 按 ISO BMFF 的 major brand 区分 mov / m4a / mp4；
// heic / avif 等图片 brand 和未知 brand 一律不放行。
func sniffFtypMime(brand string) (string, bool) {
	switch {
	case brand == "qt  ":
		return "video/quicktime", true
	case brand == "M4A " || brand == "M4B ":
		return "audio/mp4", true
	case brand == "M4V " || brand == "f4v " || brand == "MSNV" || brand == "mmp4":
		return "video/mp4", true
	case strings.HasPrefix(brand, "iso"), strings.HasPrefix(brand, "mp4"),
		strings.HasPrefix(brand, "avc"), strings.HasPrefix(brand, "dash"),
		strings.HasPrefix(brand, "3gp"), strings.HasPrefix(brand, "3g2"):
		return "video/mp4", true
	}
	return "", false
}

// probeAssetMeta 探测素材的宽高与时长；任何解析失败都只返回已得到的部分（不会报错，元数据留 0）。
func probeAssetMeta(mime string, ra io.ReaderAt, size int64) assetMeta {
	switch mime {
	case "image/png", "image/jpeg", "image/gif":
		cfg, _, err := image.DecodeConfig(io.NewSectionReader(ra, 0, size))
		if err != nil {
			return assetMeta{}
		}
		return assetMeta{Width: cfg.Width, Height: cfg.Height}
	case "image/webp":
		head := make([]byte, 32)
		n, _ := ra.ReadAt(head, 0)
		w, h := parseWebPSize(head[:n])
		return assetMeta{Width: w, Height: h}
	case "video/mp4", "video/quicktime", "audio/mp4":
		return parseMP4Meta(ra, size)
	}
	return assetMeta{}
}

// parseWebPSize 从 RIFF/WebP 头解析画布尺寸，支持 VP8（有损）、VP8L（无损）、VP8X（扩展）。
func parseWebPSize(b []byte) (int, int) {
	if len(b) < 30 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return 0, 0
	}
	switch string(b[12:16]) {
	case "VP8 ":
		// 帧标签 3 字节后是起始码 9d 01 2a，再是 14 位宽高
		if b[23] != 0x9d || b[24] != 0x01 || b[25] != 0x2a {
			return 0, 0
		}
		return int(binary.LittleEndian.Uint16(b[26:28]) & 0x3fff), int(binary.LittleEndian.Uint16(b[28:30]) & 0x3fff)
	case "VP8L":
		if b[20] != 0x2f {
			return 0, 0
		}
		bits := binary.LittleEndian.Uint32(b[21:25])
		return int(bits&0x3fff) + 1, int((bits>>14)&0x3fff) + 1
	case "VP8X":
		w := int(b[24]) | int(b[25])<<8 | int(b[26])<<16
		h := int(b[27]) | int(b[28])<<8 | int(b[29])<<16
		return w + 1, h + 1
	}
	return 0, 0
}

// mp4 解析的安全上限：防止恶意文件构造出超长 / 循环的 box 链导致长时间占用。
const (
	mp4MaxBoxes = 4096
	mp4MaxDepth = 8
)

// parseMP4Meta 手写的最小 ISO BMFF 解析：
// 从 moov/mvhd 取时长，从 moov/trak/tkhd 取第一个宽高非零轨道（即视频轨）的画面尺寸。
// 用 ReaderAt 顺着 box 头跳读，不会把 mdat 读进内存，moov 在文件末尾（非 faststart）也能解析。
func parseMP4Meta(ra io.ReaderAt, size int64) assetMeta {
	var meta assetMeta
	budget := mp4MaxBoxes
	walkMP4Boxes(ra, 0, size, 0, &budget, func(typ string, start, end int64) bool {
		if typ != "moov" {
			return false
		}
		walkMP4Boxes(ra, start, end, 1, &budget, func(typ string, start, end int64) bool {
			switch typ {
			case "mvhd":
				meta.DurationMs = parseMvhdDuration(ra, start, end)
			case "trak":
				walkMP4Boxes(ra, start, end, 2, &budget, func(typ string, start, end int64) bool {
					if typ == "tkhd" && meta.Width == 0 {
						meta.Width, meta.Height = parseTkhdSize(ra, start, end)
					}
					return false
				})
			}
			return false
		})
		return true // 只处理第一个 moov
	})
	return meta
}

// walkMP4Boxes 遍历 [start,end) 内的同级 box，fn 收到 box 类型和 payload 范围，返回 true 提前结束。
func walkMP4Boxes(ra io.ReaderAt, start, end int64, depth int, budget *int, fn func(typ string, payloadStart, payloadEnd int64) bool) {
	if depth > mp4MaxDepth {
		return
	}
	pos := start
	var hdr [16]byte
	for pos+8 <= end && *budget > 0 {
		*budget--
		if _, err := ra.ReadAt(hdr[:8], pos); err != nil {
			return
		}
		boxSize := int64(binary.BigEndian.Uint32(hdr[0:4]))
		typ := string(hdr[4:8])
		headerLen := int64(8)
		switch boxSize {
		case 0: // 延伸到父 box 结尾
			boxSize = end - pos
		case 1: // 64 位大小
			if pos+16 > end {
				return
			}
			if _, err := ra.ReadAt(hdr[8:16], pos+8); err != nil {
				return
			}
			u := binary.BigEndian.Uint64(hdr[8:16])
			if u > uint64(end-pos) {
				return
			}
			boxSize = int64(u)
			headerLen = 16
		}
		if boxSize < headerLen || pos+boxSize > end {
			return // 损坏或截断
		}
		if fn(typ, pos+headerLen, pos+boxSize) {
			return
		}
		pos += boxSize
	}
}

// parseMvhdDuration 读取 mvhd 的 timescale 与 duration，换算成毫秒。
func parseMvhdDuration(ra io.ReaderAt, start, end int64) int64 {
	var b [32]byte
	n, _ := ra.ReadAt(b[:], start)
	if n < 1 || end-start < 1 {
		return 0
	}
	var timescale uint32
	var duration uint64
	switch b[0] { // version
	case 0: // version/flags(4) creation(4) modification(4) timescale(4) duration(4)
		if n < 20 || end-start < 20 {
			return 0
		}
		timescale = binary.BigEndian.Uint32(b[12:16])
		duration = uint64(binary.BigEndian.Uint32(b[16:20]))
	case 1: // version/flags(4) creation(8) modification(8) timescale(4) duration(8)
		if n < 32 || end-start < 32 {
			return 0
		}
		timescale = binary.BigEndian.Uint32(b[20:24])
		duration = binary.BigEndian.Uint64(b[24:32])
	default:
		return 0
	}
	if timescale == 0 {
		return 0
	}
	// 先除后乘会丢精度，先乘可能溢出：用 float 换算，毫秒精度足够
	ms := float64(duration) * 1000 / float64(timescale)
	if ms < 0 || ms > 1e15 {
		return 0
	}
	return int64(ms)
}

// parseTkhdSize 读取 tkhd 末尾的 16.16 定点宽高。音频轨宽高为 0。
func parseTkhdSize(ra io.ReaderAt, start, end int64) (int, int) {
	var b [96]byte
	n, _ := ra.ReadAt(b[:], start)
	if n < 1 {
		return 0, 0
	}
	var off int
	switch b[0] {
	case 0:
		off = 76
	case 1:
		off = 88
	default:
		return 0, 0
	}
	if n < off+8 || end-start < int64(off+8) {
		return 0, 0
	}
	w := binary.BigEndian.Uint32(b[off:off+4]) >> 16
	h := binary.BigEndian.Uint32(b[off+4:off+8]) >> 16
	return int(w), int(h)
}
