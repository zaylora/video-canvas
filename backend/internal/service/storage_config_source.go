package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
	"video-canvas/internal/storage"
)

// ---- storage.Source ----

// DefaultID 实现 storage.Source：返回默认存储的 id。
func (s *StorageConfigService) DefaultID(ctx context.Context) (uint64, error) {
	row, err := s.repo.GetDefault(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return 0, errors.New("没有默认存储，请在后台“存储配置”里设置")
		}
		return 0, err
	}
	return row.ID, nil
}

// Entry 实现 storage.Source：返回一套存储的运行时配置，对象存储会带上解密后的密钥。
// 明文只在内存里流向存储客户端，不进日志、不进响应。
func (s *StorageConfigService) Entry(ctx context.Context, id uint64) (storage.Entry, error) {
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return storage.Entry{}, storage.ErrStorageNotFound
		}
		return storage.Entry{}, err
	}
	e := storage.Entry{ID: row.ID, Version: row.Version, Provider: row.Provider, SignedTTL: time.Duration(row.SignedTTLSec) * time.Second, DirectUpload: row.DirectUpload}
	if row.Provider == storage.ProviderLocal {
		e.Local = s.local
		return e, nil
	}
	secret, err := s.secrets.Get(ctx, model.StorageSecretName(id))
	if err != nil {
		return storage.Entry{}, fmt.Errorf("读取存储 %d 的密钥失败: %w", id, err)
	}
	e.Spec = specOf(row, secret)
	return e, nil
}
