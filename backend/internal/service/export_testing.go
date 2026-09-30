package service

import (
	"time"

	"video-canvas/internal/provider/modelcfg"
)

// 本文件只为 internal/tests 下的外部测试包暴露包内符号，业务代码不要引用。

const (
	AIRegistryTTL     = aiRegistryTTL
	AISecretCacheTTL  = aiSecretCacheTTL
	AISecretMaxLen    = aiSecretMaxLen
	AssetMaxNameRunes = assetMaxNameRunes
	DefaultMaxUpload  = defaultMaxUpload
)

type AssetMeta = assetMeta

var (
	AIFormatIssues        = aiFormatIssues
	AIRedactResult        = aiRedactResult
	AIRedactString        = aiRedactString
	AssetKindOfMime       = assetKindOfMime
	AssetMimeExt          = assetMimeExt
	DeriveAISecretKey     = deriveAISecretKey
	NewAssetUUID          = newAssetUUID
	ParseMP4Meta          = parseMP4Meta
	ParseWebPSize         = parseWebPSize
	ProbeAssetMeta        = probeAssetMeta
	SanitizeAssetFileName = sanitizeAssetFileName
	SniffAssetMime        = sniffAssetMime
)

func (s *AIConfigService) Repo() AIConfigRepo { return s.repo }

func (s *AIConfigService) SetNow(fn func() time.Time) { s.now = fn }

// ResetModelParse 清空已解析模型的内存缓存，模拟进程重启后的冷加载。
func (s *AIConfigService) ResetModelParse() { s.modelParse = map[uint64]*modelcfg.ModelConfig{} }

func (s *AssetService) SetRepo(r AssetRepo) { s.repo = r }

func (s *AssetService) SetNow(fn func() time.Time) { s.now = fn }
