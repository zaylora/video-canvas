package service

import (
	"context"
	"encoding/base64"
	"strconv"
	"strings"

	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"

	"go.uber.org/zap"
)

// 令牌明文的可回看部分。
//
// 鉴权仍然只用 sha256 指纹；明文另存一份密文，目的是让登录用户在列表里点「复制」就能拿到原文，
// 不必「签发时没存下来就再也取不回」。密文用凭证主密钥（APP_AI_SECRET_KEY）派生的 AES-256-GCM，
// 附加认证数据绑到该令牌自己的哈希上：把 A 令牌的密文搬到 B 令牌的行里解不出来。

// tokenCipherAAD 是密文的附加认证数据：与这一行令牌的指纹绑定。
func tokenCipherAAD(hash string) string { return "mcp-token:" + hash }

// NewMCPProjectService 创建服务；主密钥为空时不建加密器（令牌可用，但取不回明文）。
func (s *MCPProjectService) initCipher() {
	if strings.TrimSpace(s.d.SecretKey) != "" {
		s.cipher = newAISecretCipher(s.d.SecretKey)
	}
}

// sealToken 把令牌明文加密成 base64(nonce||ciphertext)；没有主密钥时返回空串。
func (s *MCPProjectService) sealToken(plain, hash string) string {
	if s.cipher == nil {
		return ""
	}
	ct, nonce, err := s.cipher.seal(tokenCipherAAD(hash), plain)
	if err != nil {
		logger.Error("加密 MCP 令牌明文失败", zap.Error(err))
		return ""
	}
	return base64.StdEncoding.EncodeToString(append(nonce, ct...))
}

// openToken 解出令牌明文；密文为空或主密钥变了都会失败。
func (s *MCPProjectService) openToken(cipherText, hash string) (string, error) {
	if s.cipher == nil {
		return "", errcode.ErrInvalidParams.WithMsg("服务端没有配置凭证主密钥，取不回令牌明文，请重新签发")
	}
	raw, err := base64.StdEncoding.DecodeString(cipherText)
	if err != nil || len(raw) <= s.cipher.aead.NonceSize() {
		return "", errcode.ErrInvalidParams.WithMsg("令牌明文已损坏，请重新签发")
	}
	n := s.cipher.aead.NonceSize()
	plain, err := s.cipher.open(tokenCipherAAD(hash), raw[n:], raw[:n])
	if err != nil {
		return "", errcode.ErrInvalidParams.WithMsg("令牌明文解密失败（主密钥变更过），请重新签发")
	}
	return plain, nil
}

// tokenDays 决定这次签发的有效期：传了就用传的，留空用后台配的默认天数。
func (s *MCPProjectService) tokenDays(ctx context.Context, days int) (int, error) {
	if days != 0 {
		if days < 1 || days > mcpTokenMaxDays {
			return 0, errcode.ErrInvalidParams.WithMsg("有效期必须在 1–90 天之间")
		}
		return days, nil
	}
	if s.d.Defaults == nil {
		return mcpDefaultTokenDays, nil
	}
	n, err := s.d.Defaults.MCPTokenDays(ctx)
	if err != nil || n < 1 || n > mcpTokenMaxDays {
		return mcpDefaultTokenDays, nil
	}
	return n, nil
}

// TokenSecret 取回这个项目那枚令牌的明文（后台代管：能读全部项目）。
//
// 一个项目只有一枚令牌，所以不再按令牌 id 取：调用方只需要说清是哪个项目。
// 只对「未注销、未过期」的令牌返回：失效的令牌就算取出明文也没有任何用处，
// 反而容易让人以为它还能用。老令牌没有密文时明确让用户重新生成。
func (s *MCPProjectService) TokenSecret(ctx context.Context, callerID, projectID uint64) (string, error) {
	if _, err := s.project(ctx, projectID); err != nil {
		return "", err
	}
	return s.projectTokenSecret(ctx, callerID, projectID)
}

// OwnedTokenSecret 取回自己项目那枚令牌的明文。
func (s *MCPProjectService) OwnedTokenSecret(ctx context.Context, ownerID, projectID uint64) (string, error) {
	if _, err := s.ownedProject(ctx, ownerID, projectID); err != nil {
		return "", err
	}
	return s.projectTokenSecret(ctx, ownerID, projectID)
}

// projectTokenSecret 取项目唯一令牌的明文；老数据里万一有多条，按 id 取最新一条。
func (s *MCPProjectService) projectTokenSecret(ctx context.Context, callerID, projectID uint64) (string, error) {
	tokens, err := s.d.Repo.ListTokens(ctx, projectID)
	if err != nil {
		return "", err
	}
	if len(tokens) == 0 {
		return "", errcode.ErrMCPTokenNotFound
	}
	t := &tokens[0]
	if t.RevokedAt != nil {
		return "", errcode.ErrInvalidParams.WithMsg("这枚令牌已注销，不能复制；重新生成一枚即可")
	}
	if !t.ExpiresAt.After(s.d.Now()) {
		return "", errcode.ErrInvalidParams.WithMsg("这枚令牌已过期，不能复制；重新生成一枚即可")
	}
	if t.SecretCipher == "" {
		return "", errcode.ErrInvalidParams.WithMsg("这枚令牌是早期生成的，当时没有保存明文，只能重新生成")
	}
	plain, err := s.openToken(t.SecretCipher, t.TokenHash)
	if err != nil {
		return "", err
	}
	// 复制等同于持有人重新拿到凭证：留一条日志，事后能查是谁取的
	logger.Info("取回 MCP 令牌明文",
		zap.Uint64("caller_id", callerID), zap.Uint64("project_id", projectID),
		zap.String("token_id", strconv.FormatUint(t.ID, 10)))
	return plain, nil
}

// MCPTokenSecretView 是「复制令牌」的响应：只要明文。
type MCPTokenSecretView struct {
	Token string `json:"token"`
}
