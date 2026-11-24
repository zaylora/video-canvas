-- 令牌明文可回看：列表里「复制」需要原文，只在库里存指纹的话取不回来。
-- 这里存的是用凭证主密钥（APP_AI_SECRET_KEY）派生的 AES-256-GCM 密文，
-- 附加认证数据绑到「该令牌的 sha256」，所以密文被搬到别的行里解不出来。
-- 老令牌没有密文（空串）：界面按「只能重新签发」处理，不影响它们继续可用。
ALTER TABLE mcp_project_tokens ADD COLUMN secret_cipher text NOT NULL DEFAULT '';
