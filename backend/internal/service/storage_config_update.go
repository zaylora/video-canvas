package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
	"video-canvas/internal/storage"
)

// ---- 修改 ----

// lockedStorageFields 是“定位字段”：它们决定素材在桶里的位置，已有素材引用后不允许修改，
// 否则一次误改就会让全部旧素材 404。换桶请新建一套存储。
func lockedChanges(oldSpec, newSpec storage.Spec) []string {
	var out []string
	add := func(changed bool, name string) {
		if changed {
			out = append(out, name)
		}
	}
	add(oldSpec.Region != newSpec.Region, "region（地域）")
	add(oldSpec.AccountID != newSpec.AccountID, "account_id（Account ID）")
	add(oldSpec.Endpoint != newSpec.Endpoint, "endpoint")
	add(oldSpec.Bucket != newSpec.Bucket, "bucket（桶名）")
	add(oldSpec.PathPrefix != newSpec.PathPrefix, "path_prefix（路径前缀）")
	add(oldSpec.Addressing != newSpec.Addressing, "addressing（寻址方式）")
	return out
}

// Update 修改一套存储的配置（整份表单提交）。
func (s *StorageConfigService) Update(ctx context.Context, actorID, id uint64, in StorageUpdateInput) (*StorageView, error) {
	// 1. 内置存储的配置来自 YAML，不能在后台改
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, storageNotFound(err)
	}
	if row.Builtin {
		return nil, errcode.ErrStorageBuiltin
	}

	// 2. 校验并规范化新配置；服务商创建后不可更改
	name, ttl, err := validateStorageCommon(in.Name, in.SignedTTLSec)
	if err != nil {
		return nil, err
	}
	newSpec, err := (storage.Spec{
		Provider: row.Provider, Endpoint: in.Endpoint, Region: in.Region, AccountID: in.AccountID, Bucket: in.Bucket,
		PathPrefix: in.PathPrefix, Addressing: in.Addressing, UseSSL: useSSLOrDefault(in.UseSSL), PublicBaseURL: in.PublicBaseURL,
	}).Normalize()
	if err != nil {
		return nil, invalidSpec(err)
	}
	oldSpec, err := specOf(row, "").Normalize()
	if err != nil {
		oldSpec = specOf(row, "") // 库里的旧配置不合法（不应出现）时按原样比较
	}

	// 3. 定位字段锁定：已有素材（或未过期的上传意图）引用这套存储时，改定位字段一律拒绝
	changed := lockedChanges(oldSpec, newSpec)
	if len(changed) > 0 {
		assets, intents, err := s.repo.CountRefs(ctx, id, s.now())
		if err != nil {
			return nil, storageNotFound(err)
		}
		if assets+intents > 0 {
			return nil, errcode.ErrStorageFieldLocked.WithMsg(fmt.Sprintf(
				"该存储已有 %d 个素材引用，不能修改：%s。如需换桶，请新建一套存储", assets, strings.Join(changed, "、")))
		}
	}

	// 4. 乐观锁更新：版本过期说明别人改过了
	err = s.repo.Update(ctx, id, in.Version, map[string]any{
		"name": name, "region": newSpec.Region, "account_id": newSpec.AccountID, "endpoint": newSpec.Endpoint, "bucket": newSpec.Bucket,
		"path_prefix": newSpec.PathPrefix, "addressing": newSpec.Addressing, "use_ssl": newSpec.UseSSL,
		"public_base_url": newSpec.PublicBaseURL, "signed_ttl_sec": ttl, "direct_upload": in.DirectUpload, "updated_by": actorID,
	})
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrRevisionConflict):
			return nil, errcode.ErrStorageVersionConflict
		case errors.Is(err, repository.ErrDuplicate):
			return nil, errcode.ErrStorageNameDup
		}
		return nil, storageNotFound(err)
	}
	s.invalidate(id) // 让本实例立刻按新配置创建客户端

	// 5. 连接相关字段变了（定位字段或公开域名）才重新测试；只改名称、有效期、直传开关不必
	if len(changed) > 0 || oldSpec.PublicBaseURL != newSpec.PublicBaseURL {
		s.recheck(ctx, id)
	}
	aiAudit(ctx, s.audit, actorID, model.AuditStorageUpdate, model.AuditTargetStorage, strconv.FormatUint(id, 10),
		map[string]any{"name": name, "bucket": newSpec.Bucket, "locked_changed": len(changed) > 0})
	return s.Get(ctx, id)
}

// recheck 用已存的密钥重新测试一套存储并记录结果（修改配置之后调用，失败不影响已保存的修改）。
func (s *StorageConfigService) recheck(ctx context.Context, id uint64) {
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return
	}
	secret, err := s.secrets.Get(ctx, model.StorageSecretName(id))
	if err != nil {
		if rerr := s.repo.RecordCheck(ctx, id, false, "存储密钥尚未设置或无法读取", s.now()); rerr != nil {
			logger.Warn("记录存储测试结果失败", zap.Uint64("storage_id", id), zap.Error(rerr))
		}
		return
	}
	res, err := s.runProbe(ctx, specOf(row, secret))
	if err != nil {
		res = storage.ProbeResult{Steps: []storage.ProbeStep{{Index: 1, Name: "配置", Issue: &storage.ProbeIssue{Title: err.Error()}}}}
	}
	s.recordResult(ctx, id, res, s.now())
}

// ReplaceSecret 同时替换 AccessKey ID 与 Secret：先用新凭证测试连接，通过才替换，
// 失败时原凭证继续可用。两者必须一起换，否则会出现新 ID 配旧 Secret 的错配。
func (s *StorageConfigService) ReplaceSecret(ctx context.Context, actorID, id uint64, accessKeyID, secretKey string) (*StorageView, error) {
	// 1. 内置存储没有密钥
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, storageNotFound(err)
	}
	if row.Builtin {
		return nil, errcode.ErrStorageBuiltin
	}
	accessKeyID, secretKey = strings.TrimSpace(accessKeyID), strings.TrimSpace(secretKey)
	if accessKeyID == "" || secretKey == "" {
		return nil, errcode.ErrStorageInvalid.WithMsg("AccessKey ID 与 Secret 不能为空")
	}

	// 2. 先测试新凭证；不通过就不动任何数据
	newRow := *row
	newRow.AccessKeyID = accessKeyID
	res, err := s.runProbe(ctx, specOf(&newRow, secretKey))
	if err != nil {
		return nil, err
	}
	if !res.OK {
		return nil, errcode.ErrStorageCheckFailed.WithMsg("新凭证测试未通过，未做任何修改：" + failureText(res))
	}

	// 3. 先带版本更新 AccessKey ID（版本冲突在这里发现，此时密钥还没动），再写密钥；
	//    写密钥失败就把 ID 改回去，避免新 ID 配旧 Secret
	err = s.repo.Update(ctx, id, row.Version, map[string]any{"access_key_id": accessKeyID, "updated_by": actorID})
	if err != nil {
		if errors.Is(err, repository.ErrRevisionConflict) {
			return nil, errcode.ErrStorageVersionConflict
		}
		return nil, storageNotFound(err)
	}
	if err := s.secrets.SetSecret(ctx, model.StorageSecretName(id), secretKey, actorID); err != nil {
		if rerr := s.repo.Update(ctx, id, row.Version+1, map[string]any{"access_key_id": row.AccessKeyID, "updated_by": actorID}); rerr != nil {
			logger.Error("写密钥失败后还原 AccessKey ID 也失败，凭证可能错配，请手动重设", zap.Uint64("storage_id", id), zap.Error(rerr))
		}
		return nil, err
	}

	// 4. 让客户端缓存失效、记录结果与审计（审计不含任何凭证）
	s.invalidate(id)
	s.recordResult(ctx, id, res, s.now())
	aiAudit(ctx, s.audit, actorID, model.AuditStorageSecret, model.AuditTargetStorage, strconv.FormatUint(id, 10), nil)
	return s.Get(ctx, id)
}

// Check 用已存的密钥重新测试一套存储并记录结果。内置本地存储直接测本地目录（没有可访问的绝对地址，跳过签名地址检查）。
func (s *StorageConfigService) Check(ctx context.Context, actorID, id uint64) (storage.ProbeResult, error) {
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return storage.ProbeResult{}, storageNotFound(err)
	}

	var res storage.ProbeResult
	if row.Provider == storage.ProviderLocal {
		st, err := storage.NewLocal(s.local)
		if err != nil {
			return storage.ProbeResult{}, errcode.ErrStorageUnavailable.WithMsg("本地存储目录不可用：" + err.Error())
		}
		res = storage.Probe(ctx, st, storage.ProbeOptions{SkipURLCheck: true})
	} else {
		secret, err := s.secrets.Get(ctx, model.StorageSecretName(id))
		if err != nil {
			return storage.ProbeResult{}, errcode.ErrStorageSecretUnset
		}
		if res, err = s.runProbe(ctx, specOf(row, secret)); err != nil {
			return storage.ProbeResult{}, err
		}
	}
	s.recordResult(ctx, id, res, s.now())
	return res, nil
}
