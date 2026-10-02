package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
)

// 凭证相关的哨兵错误：插件宿主拿到它们会按 terminal 处理，同时日志里能看出原因。
var (
	// ErrAISecretKeyMissing 服务端没有配置凭证主密钥（APP_AI_SECRET_KEY）。
	ErrAISecretKeyMissing = errors.New("未配置凭证主密钥（APP_AI_SECRET_KEY），无法读取或设置凭证")
	// ErrAISecretNotSet 凭证尚未设置。
	ErrAISecretNotSet = errors.New("凭证尚未设置")
)

// 凭证密文使用的密钥版本；将来轮换主密钥时新增版本，并按 key_version 选择解密密钥。
const aiSecretKeyVersion = 1

// aiSecretNameRe 限制凭证名的字符集：凭证名会出现在缓存 key 和日志里；允许冒号是因为渠道凭证名固定为 channel:<渠道 key>。
var aiSecretNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:\-]{0,127}$`)

// aiSecretMaxLen 是凭证明文的最大字节数，正常的 API Key 远小于它，超出多半是粘贴错了内容。
const aiSecretMaxLen = 4096

// aiSecretCipher 封装 AES-256-GCM。
type aiSecretCipher struct {
	aead cipher.AEAD
}

// newAISecretCipher 由主密钥文本创建加解密器；主密钥为空返回 nil（服务照常启动，凭证读写时报明确错误）。
func newAISecretCipher(masterKey string) *aiSecretCipher {
	masterKey = strings.TrimSpace(masterKey)
	if masterKey == "" {
		return nil
	}
	block, err := aes.NewCipher(deriveAISecretKey(masterKey))
	if err != nil { // 密钥固定 32 字节，不会失败
		return nil
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil
	}
	return &aiSecretCipher{aead: aead}
}

// deriveAISecretKey 把主密钥文本变成 32 字节：
// 64 位十六进制、或解码后正好 32 字节的 base64 直接使用；其他任意文本用 SHA-256 派生。
func deriveAISecretKey(raw string) []byte {
	if len(raw) == 64 {
		if b, err := hex.DecodeString(raw); err == nil {
			return b
		}
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(raw); err == nil && len(b) == 32 {
			return b
		}
	}
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// seal 加密明文，name 作为附加认证数据绑定到密文上，防止把 A 凭证的密文拷贝到 B 凭证的行里被当成合法数据解出。
func (c *aiSecretCipher) seal(name, plaintext string) (ciphertext, nonce []byte, err error) {
	nonce = make([]byte, c.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return c.aead.Seal(nil, nonce, []byte(plaintext), []byte(name)), nonce, nil
}

func (c *aiSecretCipher) open(name string, ciphertext, nonce []byte) (string, error) {
	if len(nonce) != c.aead.NonceSize() {
		return "", errors.New("凭证数据损坏")
	}
	b, err := c.aead.Open(nil, nonce, ciphertext, []byte(name))
	if err != nil {
		return "", errors.New("凭证解密失败，请确认主密钥未变更")
	}
	return string(b), nil
}

// aiSecretCacheItem 是解密后的凭证明文缓存项。
type aiSecretCacheItem struct {
	value   string
	expires time.Time
}

// SetSecret 设置（覆盖）凭证。只写：明文用 AES-256-GCM 加密后入库，之后任何接口都读不回明文。
func (s *AIConfigService) SetSecret(ctx context.Context, name, value string, adminID uint64) error {
	// 1. 没有主密钥就无法加密：返回明确的服务端错误，而不是悄悄存明文
	if s.cipher == nil {
		return errcode.ErrInternal.WithMsg(ErrAISecretKeyMissing.Error())
	}
	// 2. 参数校验；粘贴 Key 时常带首尾空白 / 换行，先去掉
	if !aiSecretNameRe.MatchString(name) {
		return errcode.ErrInvalidParams.WithMsg("凭证名只能包含字母、数字、下划线、点、冒号和短横线，且以字母或数字开头")
	}
	value = strings.TrimSpace(value)
	if value == "" || len(value) > aiSecretMaxLen {
		return errcode.ErrInvalidParams.WithMsg(fmt.Sprintf("凭证值不能为空，且不超过 %d 字节", aiSecretMaxLen))
	}
	// 3. 加密并入库
	ct, nonce, err := s.cipher.seal(name, value)
	if err != nil {
		return fmt.Errorf("加密凭证失败：%w", err)
	}
	if err := s.repo.UpsertSecret(ctx, &model.AISecret{
		Name: name, Ciphertext: ct, Nonce: nonce, KeyVersion: aiSecretKeyVersion, UpdatedBy: adminID, UpdatedAt: s.now(),
	}); err != nil {
		return err
	}
	// 4. 清掉本实例的明文缓存，让新值立即生效（日志只记名字，不记值）
	s.secMu.Lock()
	delete(s.secCache, name)
	s.secMu.Unlock()
	logger.Info("设置凭证", zap.String("name", name), zap.Uint64("admin_id", adminID))
	return nil
}

// ForgetSecret 清掉本实例对该凭证的明文缓存：渠道被删除（连同它的 Key）后调用，避免缓存里的旧明文在过期前还能被取到。
// 其他实例的缓存最多 aiSecretCacheTTL 后过期；而渠道删除后 Registry 已没有它，不会再有新任务去取这个 Key。
func (s *AIConfigService) ForgetSecret(name string) {
	s.secMu.Lock()
	delete(s.secCache, name)
	s.secMu.Unlock()
}

// SecretIsSet 判断凭证是否已设置（只看有没有这一行，不解密、不需要主密钥）。渠道视图的 secret_set 与发布前置检查用它。
func (s *AIConfigService) SecretIsSet(ctx context.Context, name string) (bool, error) {
	// 1. 只读元信息：这里不需要明文，也就不走解密与缓存
	_, err := s.repo.GetSecret(ctx, name)
	if errors.Is(err, repository.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Get 实现 provider.SecretResolver：解密并返回凭证明文，只给插件宿主的鉴权注入环节使用。
// 明文只放在内存缓存里（有过期时间，SetSecret 会立即清除），不进日志、不进插件 ctx（auth: custom 且渠道开启除外）。
func (s *AIConfigService) Get(ctx context.Context, name string) (string, error) {
	// 1. 内存缓存：轮询很频繁，避免每次请求都查库解密
	now := s.now()
	s.secMu.Lock()
	if it, ok := s.secCache[name]; ok && now.Before(it.expires) {
		s.secMu.Unlock()
		return it.value, nil
	}
	s.secMu.Unlock()

	// 2. 没有主密钥无法解密
	if s.cipher == nil {
		return "", ErrAISecretKeyMissing
	}
	// 3. 读密文并解密；密钥版本不认识说明主密钥轮换后还没做数据迁移
	row, err := s.repo.GetSecret(ctx, name)
	if errors.Is(err, repository.ErrNotFound) {
		return "", fmt.Errorf("%w：%s", ErrAISecretNotSet, name)
	}
	if err != nil {
		return "", err
	}
	if row.KeyVersion != aiSecretKeyVersion {
		return "", fmt.Errorf("凭证 %s 的密钥版本 %d 不受支持", name, row.KeyVersion)
	}
	plain, err := s.cipher.open(name, row.Ciphertext, row.Nonce)
	if err != nil {
		return "", fmt.Errorf("%w：%s", err, name)
	}
	// 4. 写缓存
	s.secMu.Lock()
	s.secCache[name] = aiSecretCacheItem{value: plain, expires: now.Add(aiSecretCacheTTL)}
	s.secMu.Unlock()
	return plain, nil
}
