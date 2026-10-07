// Package skillpkg 是 Agent 技能包的预检：把 zip、文件夹或单个 SKILL.md 解析成统一的 Result，
// 并产出规范化后的 zip 与整包 sha256。
// 这里全是纯函数，只处理内存里的 []byte，不读写磁盘、不碰数据库；
// 「与内置同名」「与已有版本同内容」要查库，由 service 层负责。
// 预检不通过不返回 Go error，问题都记在 Result.Issues 里。
package skillpkg

import (
	"regexp"
	"sort"
)

// 预检限额，数值以设计文档 §6.7 为准。
const (
	// MaxFiles 是一个包最多的文件数（不含被忽略的文件）。
	MaxFiles = 500
	// MaxZipBytes 是上传的 zip 最大字节数。
	MaxZipBytes = 50 << 20
	// MaxFileBytes 是单个文件解压后的最大字节数。
	MaxFileBytes = 25 << 20
	// MaxTotalBytes 是全部文件解压后的最大总字节数。
	MaxTotalBytes = 100 << 20
	// MaxRatio 是允许的最大压缩比（解压后 / 压缩后）。
	MaxRatio = 100
	// MinRatioBytes 是检查压缩比的最小文件大小；更小的文件即使比值很高也无害（如一千个重复字符）。
	MinRatioBytes = 64 << 10
	// MaxNameLen 是 name 的最大字符数。
	MaxNameLen = 64
	// MaxDescriptionLen 是 description 的最大字符数。
	MaxDescriptionLen = 1024
	// MaxCompatibilityLen 是 compatibility 的最大字符数。
	MaxCompatibilityLen = 500
	// MaxSkillMDLines 是 SKILL.md 建议的最大行数，超过只提示。
	MaxSkillMDLines = 500
	// MaxPreviewBytes 是文本预览的上限，供上层判断；本包不截断内容。
	MaxPreviewBytes = 1 << 20
)

// Issue 的级别。
const (
	LevelError = "error" // 阻断确认导入
	LevelWarn  = "warn"  // 提示，不阻断
	LevelInfo  = "info"  // 信息
)

// Issue 的代码。前 15 个与设计文档 §6.7 一致（BUILTIN_NAME、SAME_AS_VERSION 要查库，不在本包产出）。
const (
	CodeNoSkillMD        = "NO_SKILL_MD"           // 错误：根目录没有 SKILL.md
	CodeMultiSkillMD     = "MULTI_SKILL_MD"        // 错误：包里有多个 SKILL.md
	CodeBadYAML          = "BAD_YAML"              // 错误：frontmatter 不存在或解析失败
	CodeBadName          = "BAD_NAME"              // 错误：name 缺失或不合规
	CodeBadDescription   = "BAD_DESCRIPTION"       // 错误：description 缺失、为空或过长
	CodePathEscape       = "PATH_ESCAPE"           // 错误：路径含 ..、绝对路径、盘符、反斜杠、NUL
	CodeSymlink          = "SYMLINK"               // 错误：条目是符号链接
	CodeDupPath          = "DUP_PATH"              // 错误：重复路径（含仅大小写不同）
	CodeBadFilenameEnc   = "BAD_FILENAME_ENCODING" // 错误：文件名非 UTF-8 且 GBK 也解不开
	CodeTooManyFiles     = "TOO_MANY_FILES"        // 错误：文件数超过 MaxFiles
	CodeFileTooLarge     = "FILE_TOO_LARGE"        // 错误：单文件超过 MaxFileBytes
	CodePackageTooLarge  = "PACKAGE_TOO_LARGE"     // 错误：zip 超过 MaxZipBytes 或解压后总量超过 MaxTotalBytes
	CodeZipBomb          = "ZIP_BOMB"              // 错误：压缩比超过 MaxRatio
	CodeBadZip           = "BAD_ZIP"               // 错误（本包新增）：不是有效 zip、条目损坏或加密
	CodeNameDirMismatch  = "NAME_DIR_MISMATCH"     // 提示：name 与外层目录名不一致
	CodeUnsupportedField = "UNSUPPORTED_FIELD"     // 提示：frontmatter 含本系统未支持的字段
	CodeLongSkillMD      = "LONG_SKILL_MD"         // 提示：SKILL.md 超过 500 行
	CodeMissingRef       = "MISSING_REF"           // 提示：SKILL.md 引用的相对路径不在包里
	CodeHasScripts       = "HAS_SCRIPTS"           // 提示：包里有脚本文件
	CodeBinaryExecutable = "BINARY_EXECUTABLE"     // 提示：包里有 ELF / Mach-O / PE 可执行文件
	CodeIgnoredFiles     = "IGNORED_FILES"         // 信息：自动忽略了垃圾文件
	CodeBadCompatibility = "BAD_COMPATIBILITY"     // 提示（本包新增）：compatibility 不是字符串或超过 500 字符
)

// 文件类型 Kind。
const (
	KindSkill  = "skill"  // SKILL.md
	KindScript = "script" // scripts/** 或脚本扩展名
	KindDoc    = "doc"    // assets/ 之外的 .md、.txt
	KindAsset  = "asset"  // assets/**、图片、数据文件
	KindOther  = "other"  // 其余
)

// Input 是文件夹拖入或单个 SKILL.md 时，前端传来的一个文件。
type Input struct {
	Path string // 相对路径，用 / 分隔，可带外层目录
	Data []byte
}

// File 是包内一个文件的清单项，字段与 files_json 对应。
type File struct {
	Path   string `json:"path"`           // 规范化后的路径，用 / 分隔
	Size   int64  `json:"size"`           // 解压后字节数
	SHA256 string `json:"sha256"`         // 文件内容的 sha256（十六进制）
	Kind   string `json:"kind"`           // skill / script / doc / asset / other
	Lang   string `json:"lang,omitempty"` // 脚本语言，仅 script 有
	Text   bool   `json:"text"`           // 有效 UTF-8 且前 8 KB 无 NUL，可预览
}

// Issue 是一条预检问题。
type Issue struct {
	Level   string `json:"level"`          // error / warn / info
	Code    string `json:"code"`           // 见 Code* 常量
	Path    string `json:"path,omitempty"` // 相关文件，可为空
	Message string `json:"message"`        // 中文说明
}

// Result 是一次预检的结果。
// 结构性错误（路径穿越、符号链接、超限、不是 zip 等）会让预检提前结束，此时只有 Issues；
// 内容类错误（缺 SKILL.md、frontmatter 不合规）仍会填充 Files、SHA256 和 Package，方便界面展示。
type Result struct {
	Name        string         // frontmatter.name
	Description string         // frontmatter.description
	Title       string         // metadata.title，没有则用 name
	Frontmatter map[string]any // frontmatter 全部字段，原样保存
	Unsupported []string       // 本系统未支持的字段名（含 allowed-tools），已排序
	Body        string         // SKILL.md 去掉头部后的正文
	Files       []File         // 清单，按路径排序
	HasScripts  bool           // 是否含脚本文件
	TotalBytes  int64          // 全部文件解压后的字节数
	Issues      []Issue        // 预检问题
	SHA256      string         // 规范化整包的 sha256（十六进制）
	Package     []byte         // 规范化后的 zip
}

// HasErrors 报告是否有 error 级问题；有则不能确认导入。
func (r *Result) HasErrors() bool {
	for _, i := range r.Issues {
		if i.Level == LevelError {
			return true
		}
	}
	return false
}

// nameRe 是 name 的格式：小写字母、数字、连字符，不以连字符开头结尾，不连续。
var nameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// sortIssues 让错误排在前面，同级别保持产生顺序。
func sortIssues(in []Issue) {
	rank := map[string]int{LevelError: 0, LevelWarn: 1, LevelInfo: 2}
	sort.SliceStable(in, func(i, j int) bool { return rank[in[i].Level] < rank[in[j].Level] })
}
