package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/storage"
)

// ---- 升级导入 ----

// legacyImportName 是从环境变量导入的存储的显示名。
const legacyImportName = "迁移自环境变量"

// legacyProbeTimeout 是导入后顺手测试一次的超时：启动路径上不能被网络问题拖太久。
const legacyProbeTimeout = 10 * time.Second

// ImportLegacyS3 把旧版用环境变量（APP_STORAGE_DRIVER=s3 + APP_STORAGE_S3_*）配置的 S3 存储一次性导入为后台存储，
// 返回是否真的导入了。启动时在内置存储初始化之前调用。
//
// 为什么必须导入：升级前素材表没有记录存储，升级后所有旧素材的 storage_id 都是 0。如果不导入，它们会被回填到
// 内置的本地磁盘，而这些素材其实都在对象存储里，升级后会全部 404。
//
// 只导入一次：已经存在后台创建的存储（非内置）就说明导入过或管理员已经自己配置了，直接跳过。
func (s *StorageConfigService) ImportLegacyS3(ctx context.Context, cfg config.Storage) (bool, error) {
	// 1. 已有非内置存储就不再导入
	rows, err := s.repo.List(ctx)
	if err != nil {
		return false, err
	}
	for i := range rows {
		if !rows[i].Builtin {
			return false, nil
		}
	}

	// 2. 校验旧配置，缺什么就点名哪个环境变量，方便运维补齐
	c := cfg.S3
	switch {
	case c.Endpoint == "" || c.Bucket == "":
		return false, errors.New("检测到 APP_STORAGE_DRIVER=s3，但缺少 APP_STORAGE_S3_ENDPOINT 或 APP_STORAGE_S3_BUCKET")
	case c.AccessKey == "":
		return false, errors.New("检测到 APP_STORAGE_DRIVER=s3，但缺少 APP_STORAGE_S3_ACCESS_KEY")
	case c.SecretKey == "":
		return false, errors.New("检测到 APP_STORAGE_DRIVER=s3，但缺少 APP_STORAGE_S3_SECRET_KEY")
	}
	spec, err := (storage.Spec{
		Provider: storage.ProviderS3, Endpoint: c.Endpoint, Region: c.Region, Bucket: c.Bucket, PathPrefix: c.PathPrefix,
		Addressing: storage.AddressingAuto, UseSSL: c.UseSSL, PublicBaseURL: c.PublicBaseURL,
		AccessKey: c.AccessKey, SecretKey: c.SecretKey,
	}).Normalize()
	if err != nil {
		return false, fmt.Errorf("旧版 S3 配置不合法: %w", err)
	}
	ttl := int(cfg.SignedTTL / time.Second)
	if ttl < storageMinTTL || ttl > storageMaxTTL {
		ttl = storageDefaultTTL
	}

	// 3. 落库并写密钥。寻址保持“自动”，和升级前的行为一致；写密钥失败（通常是没配主密钥）要回滚，
	//    否则旧素材会指向一个没有密钥、永远用不了的存储
	row := &model.StorageConfig{
		Name: legacyImportName, Provider: spec.Provider, Endpoint: spec.Endpoint, Region: spec.Region, Bucket: spec.Bucket,
		PathPrefix: spec.PathPrefix, Addressing: spec.Addressing, UseSSL: spec.UseSSL, AccessKeyID: spec.AccessKey,
		PublicBaseURL: spec.PublicBaseURL, SignedTTLSec: ttl, Version: 1,
	}
	if err := s.repo.Create(ctx, row); err != nil {
		return false, err
	}
	if err := s.secrets.SetSecret(ctx, model.StorageSecretName(row.ID), spec.SecretKey, 0); err != nil {
		if derr := s.repo.Delete(ctx, row.ID, s.now()); derr != nil {
			logger.Error("回滚导入的存储失败，请手动清理", zap.Uint64("storage_id", row.ID), zap.Error(derr))
		}
		return false, fmt.Errorf("写入存储密钥失败，请确认已配置 APP_AI_SECRET_KEY: %w", err)
	}

	// 4. 旧素材指向它并设为默认
	if _, err := s.repo.BackfillAssets(ctx, row.ID); err != nil {
		return false, fmt.Errorf("回填旧素材的存储失败: %w", err)
	}
	if err := s.repo.SetDefault(ctx, row.ID); err != nil {
		return false, fmt.Errorf("设置默认存储失败: %w", err)
	}

	// 5. 顺手测试一次并记录结果（不通过也不阻止启动：运行时会按需重试，后台能看到失败原因）
	pctx, cancel := context.WithTimeout(ctx, legacyProbeTimeout)
	defer cancel()
	if res, err := s.prober.Probe(pctx, spec); err == nil {
		s.recordResult(ctx, row.ID, res, s.now())
	}
	aiAudit(ctx, s.audit, 0, model.AuditStorageCreate, model.AuditTargetStorage, strconv.FormatUint(row.ID, 10),
		map[string]any{"name": legacyImportName, "provider": spec.Provider, "bucket": spec.Bucket, "source": "legacy_env"})
	logger.Warn("已把环境变量里的 S3 存储导入为后台存储，之后请在后台“存储配置”里管理，并可删除 APP_STORAGE_DRIVER / APP_STORAGE_S3_* 环境变量",
		zap.Uint64("storage_id", row.ID), zap.String("bucket", spec.Bucket))
	return true, nil
}
