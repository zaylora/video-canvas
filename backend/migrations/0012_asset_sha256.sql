-- 素材内容哈希：画布导入时按 (sha256, byte_size) 复用已有文件，避免重复存储。
-- 空串表示还没计算（存量素材与尚未回填的行），索引在 0013 里并发创建。
ALTER TABLE assets ADD COLUMN sha256 varchar(64) NOT NULL DEFAULT '';
