package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	. "video-canvas/internal/service"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/storage"
)

// ---- fakes ----

// storCfgRepo 实现 StorageConfigRepo，数据放内存里，行为和 repository 的约定一致。
type storCfgRepo struct {
	rows      map[uint64]*model.StorageConfig
	nextID    uint64
	assets    map[uint64]int64
	intents   map[uint64]int64
	deleted   []uint64
	createErr error
	backfills []uint64 // BackfillAssets 收到的目标存储 id
}

func newStorCfgRepo() *storCfgRepo {
	return &storCfgRepo{rows: map[uint64]*model.StorageConfig{}, nextID: 1, assets: map[uint64]int64{}, intents: map[uint64]int64{}}
}

func (r *storCfgRepo) Create(ctx context.Context, s *model.StorageConfig) error {
	if r.createErr != nil {
		return r.createErr
	}
	for _, x := range r.rows {
		if x.Name == s.Name {
			return repository.ErrDuplicate
		}
	}
	s.ID = r.nextID
	r.nextID++
	cp := *s
	r.rows[s.ID] = &cp
	return nil
}

func (r *storCfgRepo) GetByID(ctx context.Context, id uint64) (*model.StorageConfig, error) {
	s, ok := r.rows[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *s
	return &cp, nil
}

func (r *storCfgRepo) GetDefault(ctx context.Context) (*model.StorageConfig, error) {
	for _, s := range r.rows {
		if s.IsDefault {
			cp := *s
			return &cp, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *storCfgRepo) List(ctx context.Context) ([]model.StorageConfig, error) {
	var out []model.StorageConfig
	for id := uint64(1); id < r.nextID; id++ {
		if s, ok := r.rows[id]; ok {
			out = append(out, *s)
		}
	}
	return out, nil
}

func (r *storCfgRepo) Update(ctx context.Context, id uint64, version int, fields map[string]any) error {
	s, ok := r.rows[id]
	if !ok {
		return repository.ErrNotFound
	}
	if s.Version != version {
		return repository.ErrRevisionConflict
	}
	for k, v := range fields {
		switch k {
		case "name":
			for _, x := range r.rows {
				if x.ID != id && x.Name == v.(string) {
					return repository.ErrDuplicate
				}
			}
			s.Name = v.(string)
		case "region":
			s.Region = v.(string)
		case "account_id":
			s.AccountID = v.(string)
		case "endpoint":
			s.Endpoint = v.(string)
		case "bucket":
			s.Bucket = v.(string)
		case "path_prefix":
			s.PathPrefix = v.(string)
		case "addressing":
			s.Addressing = v.(string)
		case "use_ssl":
			s.UseSSL = v.(bool)
		case "public_base_url":
			s.PublicBaseURL = v.(string)
		case "signed_ttl_sec":
			s.SignedTTLSec = v.(int)
		case "direct_upload":
			s.DirectUpload = v.(bool)
		case "access_key_id":
			s.AccessKeyID = v.(string)
		case "updated_by":
			s.UpdatedBy = v.(uint64)
		}
	}
	s.Version++
	return nil
}

func (r *storCfgRepo) RecordCheck(ctx context.Context, id uint64, ok bool, errMsg string, at time.Time) error {
	s, found := r.rows[id]
	if !found {
		return repository.ErrNotFound
	}
	s.LastCheckOK, s.LastCheckError, s.LastCheckAt = &ok, errMsg, &at
	return nil
}

func (r *storCfgRepo) SetDefault(ctx context.Context, id uint64) error {
	if _, ok := r.rows[id]; !ok {
		return repository.ErrNotFound
	}
	for _, s := range r.rows {
		s.IsDefault = s.ID == id
	}
	return nil
}

func (r *storCfgRepo) BackfillAssets(ctx context.Context, id uint64) (int64, error) {
	r.backfills = append(r.backfills, id)
	return 0, nil
}

func (r *storCfgRepo) AssetCounts(ctx context.Context) (map[uint64]int64, error) {
	return r.assets, nil
}

func (r *storCfgRepo) CountRefs(ctx context.Context, id uint64, now time.Time) (int64, int64, error) {
	if _, ok := r.rows[id]; !ok {
		return 0, 0, repository.ErrNotFound
	}
	return r.assets[id], r.intents[id], nil
}

func (r *storCfgRepo) Delete(ctx context.Context, id uint64, now time.Time) error {
	if _, ok := r.rows[id]; !ok {
		return repository.ErrNotFound
	}
	if r.assets[id] > 0 || r.intents[id] > 0 {
		return repository.ErrInUse
	}
	delete(r.rows, id)
	r.deleted = append(r.deleted, id)
	return nil
}

// storSecrets 实现 StorageSecrets，记录写入的明文，用来断言密钥没有出现在别的地方。
type storSecrets struct {
	values    map[string]string
	forgotten []string
	setErr    error
}

func newStorSecrets() *storSecrets { return &storSecrets{values: map[string]string{}} }

func (s *storSecrets) SetSecret(ctx context.Context, name, value string, adminID uint64) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.values[name] = value
	return nil
}
func (s *storSecrets) SecretIsSet(ctx context.Context, name string) (bool, error) {
	_, ok := s.values[name]
	return ok, nil
}
func (s *storSecrets) Get(ctx context.Context, name string) (string, error) {
	v, ok := s.values[name]
	if !ok {
		return "", ErrAISecretNotSet
	}
	return v, nil
}
func (s *storSecrets) ForgetSecret(name string) { s.forgotten = append(s.forgotten, name) }

// storProber 实现 StorageProber：按脚本返回测试结果，记录收到的配置。
type storProber struct {
	result storage.ProbeResult
	err    error
	specs  []storage.Spec
}

func okProbe() storage.ProbeResult {
	r := storage.ProbeResult{OK: true}
	for i, n := range []string{"鉴权与桶", "写入探针对象", "读取探针对象", "签名地址可访问", "删除探针对象"} {
		r.Steps = append(r.Steps, storage.ProbeStep{Index: i + 1, Name: n, OK: true})
	}
	return r
}

func failProbe(title string) storage.ProbeResult {
	r := okProbe()
	r.OK = false
	r.Steps[0].OK = false
	r.Steps[0].Issue = &storage.ProbeIssue{Title: title, Hint: "检查密钥", Raw: "AccessDenied: Access Denied"}
	for i := 1; i < len(r.Steps); i++ {
		r.Steps[i].OK, r.Steps[i].Skipped = false, true
	}
	return r
}

func (p *storProber) Probe(ctx context.Context, spec storage.Spec) (storage.ProbeResult, error) {
	p.specs = append(p.specs, spec)
	return p.result, p.err
}

type storAudit struct{ logs []*model.AIAuditLog }

func (a *storAudit) InsertAudit(ctx context.Context, l *model.AIAuditLog) error {
	a.logs = append(a.logs, l)
	return nil
}

type storInvalidator struct{ ids []uint64 }

func (i *storInvalidator) Invalidate(id uint64) { i.ids = append(i.ids, id) }

type storEnv struct {
	svc   *StorageConfigService
	repo  *storCfgRepo
	sec   *storSecrets
	probe *storProber
	audit *storAudit
	inv   *storInvalidator
}

func newStorEnv(t *testing.T) *storEnv {
	t.Helper()
	e := &storEnv{repo: newStorCfgRepo(), sec: newStorSecrets(), probe: &storProber{result: okProbe()}, audit: &storAudit{}, inv: &storInvalidator{}}
	e.svc = NewStorageConfigService(e.repo, e.sec, e.audit, e.probe, config.Storage{Local: config.LocalStorage{Dir: t.TempDir()}})
	e.svc.SetInvalidator(e.inv)
	e.svc.SetNow(func() time.Time { return time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC) })
	return e
}

func ossInput() StorageCreateInput {
	return StorageCreateInput{Name: "OSS 杭州", Provider: storage.ProviderAliyunOSS, Region: "cn-hangzhou", Bucket: "vc-bucket",
		AccessKeyID: "LTAI5tABCDEFGHIJKLM", SecretKey: "super-secret-value"}
}

func storCode(t *testing.T, err error) int {
	t.Helper()
	var e *errcode.Error
	if !errors.As(err, &e) {
		t.Fatalf("期望业务错误，实际：%v", err)
	}
	return e.Code
}

// seedStorage 直接往 fake 里放一套已有的存储（绕过 Create），返回它的 id。
func (e *storEnv) seed(mut func(*model.StorageConfig)) uint64 {
	row := &model.StorageConfig{Name: "seed", Provider: storage.ProviderAliyunOSS, Region: "cn-hangzhou", Endpoint: "oss-cn-hangzhou.aliyuncs.com",
		Bucket: "vc-bucket", Addressing: "virtual", UseSSL: true, AccessKeyID: "LTAI5tABCDEFGHIJKLM", SignedTTLSec: 3600, Version: 1}
	if mut != nil {
		mut(row)
	}
	_ = e.repo.Create(context.Background(), row)
	e.sec.values[model.StorageSecretName(row.ID)] = "stored-secret"
	return row.ID
}

// ---- Create ----

func TestStorageConfigService_Create(t *testing.T) {
	ctx := context.Background()

	t.Run("成功：先测试，保存配置，密钥加密存放，视图脱敏", func(t *testing.T) {
		e := newStorEnv(t)
		v, err := e.svc.Create(ctx, 9, ossInput())
		if err != nil {
			t.Fatal(err)
		}
		if v.ID == 0 || v.Provider != storage.ProviderAliyunOSS || v.Endpoint != "oss-cn-hangzhou.aliyuncs.com" || !v.UseSSL || v.Addressing != "virtual" {
			t.Errorf("视图应带按预设推导的 endpoint：%+v", v)
		}
		if !v.SecretSet || v.AccessKeyID == "LTAI5tABCDEFGHIJKLM" || !strings.Contains(v.AccessKeyID, "••••") {
			t.Errorf("AccessKey 应脱敏、密钥只暴露 secret_set：%+v", v)
		}
		if v.Check == nil || !v.Check.OK || v.IsDefault || v.Access != "private" || v.DirectMethod != storage.DirectPostPolicy {
			t.Errorf("实际 %+v", v)
		}
		if got := e.sec.values[model.StorageSecretName(v.ID)]; got != "super-secret-value" {
			t.Errorf("密钥应存到 storage:<id> 下，实际 %q", got)
		}
		if len(e.probe.specs) != 1 || e.probe.specs[0].SecretKey != "super-secret-value" {
			t.Errorf("保存前应用提交的密钥测试一次：%+v", e.probe.specs)
		}
		row := e.repo.rows[v.ID]
		if row.CreatedBy != 9 || row.Version != 1 || row.LastCheckOK == nil || !*row.LastCheckOK {
			t.Errorf("落库行不对：%+v", row)
		}
		if len(e.audit.logs) != 1 || e.audit.logs[0].Action != model.AuditStorageCreate || strings.Contains(string(e.audit.logs[0].DetailJSON), "super-secret") {
			t.Errorf("应写一条不含密钥的审计：%+v", e.audit.logs)
		}
	})

	t.Run("测试不通过仍可保存，但记录失败原因，且不能设为默认", func(t *testing.T) {
		e := newStorEnv(t)
		e.probe.result = failProbe("密钥无权访问该桶")
		v, err := e.svc.Create(ctx, 9, ossInput())
		if err != nil {
			t.Fatalf("测试不通过也应保存：%v", err)
		}
		if v.Check == nil || v.Check.OK || !strings.Contains(v.Check.Error, "密钥无权访问该桶") {
			t.Errorf("应记录失败原因：%+v", v.Check)
		}
		if err := e.svc.SetDefault(ctx, 9, v.ID); storCode(t, err) != errcode.ErrStorageNotChecked.Code {
			t.Errorf("测试未通过不能设为默认：%v", err)
		}
	})

	t.Run("入参不合法", func(t *testing.T) {
		tests := []struct {
			name string
			mut  func(*StorageCreateInput)
		}{
			{"名称为空", func(in *StorageCreateInput) { in.Name = "  " }},
			{"名称过长", func(in *StorageCreateInput) { in.Name = strings.Repeat("a", 65) }},
			{"local 不能新建", func(in *StorageCreateInput) { in.Provider = storage.ProviderLocal }},
			{"未知服务商", func(in *StorageCreateInput) { in.Provider = "qiniu" }},
			{"COS 桶名没有 APPID", func(in *StorageCreateInput) { in.Provider = storage.ProviderTencentCOS; in.Region = "ap-guangzhou" }},
			{"缺少 AccessKey", func(in *StorageCreateInput) { in.AccessKeyID = "" }},
			{"缺少 Secret", func(in *StorageCreateInput) { in.SecretKey = " " }},
			{"签名有效期过短", func(in *StorageCreateInput) { in.SignedTTLSec = 10 }},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				e := newStorEnv(t)
				in := ossInput()
				tt.mut(&in)
				if _, err := e.svc.Create(ctx, 9, in); storCode(t, err) != errcode.ErrStorageInvalid.Code {
					t.Fatalf("期望 ErrStorageInvalid，实际：%v", err)
				}
				if len(e.repo.rows) != 0 || len(e.sec.values) != 0 {
					t.Errorf("不合法的入参不应留下任何数据")
				}
			})
		}
	})

	t.Run("名称重复：不写密钥", func(t *testing.T) {
		e := newStorEnv(t)
		e.seed(func(r *model.StorageConfig) { r.Name = "OSS 杭州" })
		before := len(e.sec.values)
		if _, err := e.svc.Create(ctx, 9, ossInput()); storCode(t, err) != errcode.ErrStorageNameDup.Code {
			t.Fatalf("期望 ErrStorageNameDup，实际：%v", err)
		}
		if len(e.sec.values) != before {
			t.Error("名称重复不应写入密钥")
		}
	})

	t.Run("写密钥失败要回滚刚创建的配置", func(t *testing.T) {
		e := newStorEnv(t)
		e.sec.setErr = errors.New("AI 主密钥未配置")
		if _, err := e.svc.Create(ctx, 9, ossInput()); err == nil {
			t.Fatal("期望失败")
		}
		if len(e.repo.rows) != 0 {
			t.Errorf("配置应被回滚，实际还有 %d 条", len(e.repo.rows))
		}
	})
}

// ---- Update ----

func TestStorageConfigService_Update(t *testing.T) {
	ctx := context.Background()
	update := func(id uint64, mut func(*StorageUpdateInput)) StorageUpdateInput {
		in := StorageUpdateInput{Version: 1, Name: "seed", Region: "cn-hangzhou", Bucket: "vc-bucket", SignedTTLSec: 3600}
		if mut != nil {
			mut(&in)
		}
		return in
	}

	t.Run("没有素材时可以改定位字段，并重新测试", func(t *testing.T) {
		e := newStorEnv(t)
		id := e.seed(nil)
		v, err := e.svc.Update(ctx, 9, id, update(id, func(in *StorageUpdateInput) { in.Bucket = "vc-bucket-2"; in.Region = "cn-shanghai" }))
		if err != nil {
			t.Fatal(err)
		}
		if v.Bucket != "vc-bucket-2" || v.Endpoint != "oss-cn-shanghai.aliyuncs.com" || v.Version != 2 {
			t.Errorf("实际 %+v", v)
		}
		if len(e.probe.specs) != 1 || e.probe.specs[0].SecretKey != "stored-secret" || e.probe.specs[0].Bucket != "vc-bucket-2" {
			t.Errorf("连接相关字段变了应用已存的密钥重测：%+v", e.probe.specs)
		}
		if !contains(e.inv.ids, id) {
			t.Errorf("更新后应让客户端缓存失效：%v", e.inv.ids)
		}
	})

	t.Run("已有素材时定位字段锁定，错误里写明是哪些字段", func(t *testing.T) {
		e := newStorEnv(t)
		id := e.seed(nil)
		e.repo.assets[id] = 1284
		_, err := e.svc.Update(ctx, 9, id, update(id, func(in *StorageUpdateInput) { in.Bucket = "other"; in.PathPrefix = "x" }))
		if storCode(t, err) != errcode.ErrStorageFieldLocked.Code {
			t.Fatalf("期望 ErrStorageFieldLocked，实际：%v", err)
		}
		for _, f := range []string{"bucket", "path_prefix"} {
			if !strings.Contains(err.Error(), f) {
				t.Errorf("错误应点名被锁字段 %s：%v", f, err)
			}
		}
		if e.repo.rows[id].Bucket != "vc-bucket" {
			t.Error("被拒绝的修改不能落库")
		}
	})

	t.Run("已有素材时仍可改名称、公开域名、签名有效期、直传开关；这些不触发重测", func(t *testing.T) {
		e := newStorEnv(t)
		id := e.seed(nil)
		e.repo.assets[id] = 5
		v, err := e.svc.Update(ctx, 9, id, update(id, func(in *StorageUpdateInput) {
			in.Name = "OSS 杭州 · 生产"
			in.SignedTTLSec = 21600
			in.DirectUpload = true
		}))
		if err != nil {
			t.Fatal(err)
		}
		if v.Name != "OSS 杭州 · 生产" || v.SignedTTLSec != 21600 || !v.DirectUpload {
			t.Errorf("实际 %+v", v)
		}
		if len(e.probe.specs) != 0 {
			t.Errorf("只改名称等非连接字段不应重测：%+v", e.probe.specs)
		}
	})

	t.Run("内置存储不可修改", func(t *testing.T) {
		e := newStorEnv(t)
		id := e.seed(func(r *model.StorageConfig) {
			r.Provider = storage.ProviderLocal
			r.Builtin = true
			r.Name = "本地磁盘"
		})
		if _, err := e.svc.Update(ctx, 9, id, update(id, nil)); storCode(t, err) != errcode.ErrStorageBuiltin.Code {
			t.Fatalf("实际：%v", err)
		}
	})

	t.Run("版本过期返回冲突，不存在返回 404，名称重复返回 409", func(t *testing.T) {
		e := newStorEnv(t)
		id := e.seed(nil)
		other := e.seed(func(r *model.StorageConfig) { r.Name = "other" })
		if _, err := e.svc.Update(ctx, 9, id, update(id, func(in *StorageUpdateInput) { in.Version = 7 })); storCode(t, err) != errcode.ErrStorageVersionConflict.Code {
			t.Errorf("版本冲突：%v", err)
		}
		if _, err := e.svc.Update(ctx, 9, 999, update(999, nil)); storCode(t, err) != errcode.ErrStorageNotFound.Code {
			t.Errorf("不存在：%v", err)
		}
		if _, err := e.svc.Update(ctx, 9, other, update(other, func(in *StorageUpdateInput) { in.Name = "seed" })); storCode(t, err) != errcode.ErrStorageNameDup.Code {
			t.Errorf("名称重复：%v", err)
		}
	})
}

func contains(ids []uint64, id uint64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// ---- ReplaceSecret ----

func TestStorageConfigService_ReplaceSecret(t *testing.T) {
	ctx := context.Background()

	t.Run("先用新密钥测试，通过才替换，并让缓存失效", func(t *testing.T) {
		e := newStorEnv(t)
		id := e.seed(nil)
		v, err := e.svc.ReplaceSecret(ctx, 9, id, "LTAINEWKEYNEWKEYNEW", "new-secret")
		if err != nil {
			t.Fatal(err)
		}
		if e.sec.values[model.StorageSecretName(id)] != "new-secret" || e.probe.specs[0].SecretKey != "new-secret" {
			t.Errorf("应先测后换：%v %+v", e.sec.values, e.probe.specs)
		}
		if v.Version != 2 || !strings.HasPrefix(v.AccessKeyID, "LTAI") || !contains(e.inv.ids, id) {
			t.Errorf("实际 %+v inv=%v", v, e.inv.ids)
		}
		if len(e.audit.logs) != 1 || e.audit.logs[0].Action != model.AuditStorageSecret || strings.Contains(string(e.audit.logs[0].DetailJSON), "new-secret") {
			t.Errorf("审计不应含密钥：%+v", e.audit.logs)
		}
	})

	t.Run("新密钥测试不通过：不替换，原密钥继续可用", func(t *testing.T) {
		e := newStorEnv(t)
		id := e.seed(nil)
		e.probe.result = failProbe("密钥无权访问该桶")
		_, err := e.svc.ReplaceSecret(ctx, 9, id, "LTAINEWKEYNEWKEYNEW", "bad")
		if storCode(t, err) != errcode.ErrStorageCheckFailed.Code || !strings.Contains(err.Error(), "密钥无权访问该桶") {
			t.Fatalf("期望带原因的 ErrStorageCheckFailed，实际：%v", err)
		}
		if e.sec.values[model.StorageSecretName(id)] != "stored-secret" || e.repo.rows[id].Version != 1 {
			t.Error("失败时密钥与配置都不能变")
		}
	})

	t.Run("内置存储没有密钥可换；空密钥不合法", func(t *testing.T) {
		e := newStorEnv(t)
		id := e.seed(func(r *model.StorageConfig) {
			r.Provider = storage.ProviderLocal
			r.Builtin = true
			r.Name = "本地磁盘"
		})
		if _, err := e.svc.ReplaceSecret(ctx, 9, id, "a", "b"); storCode(t, err) != errcode.ErrStorageBuiltin.Code {
			t.Errorf("内置：%v", err)
		}
		oss := e.seed(func(r *model.StorageConfig) { r.Name = "x" })
		if _, err := e.svc.ReplaceSecret(ctx, 9, oss, "", ""); storCode(t, err) != errcode.ErrStorageInvalid.Code {
			t.Errorf("空密钥：%v", err)
		}
	})
}

// ---- SetDefault / Delete ----

func TestStorageConfigService_SetDefault(t *testing.T) {
	ctx := context.Background()
	t.Run("测试通过的存储可以设为默认，并让缓存整体失效", func(t *testing.T) {
		e := newStorEnv(t)
		ok := true
		id := e.seed(func(r *model.StorageConfig) { r.LastCheckOK = &ok })
		if err := e.svc.SetDefault(ctx, 9, id); err != nil {
			t.Fatal(err)
		}
		if !e.repo.rows[id].IsDefault || !contains(e.inv.ids, 0) {
			t.Errorf("默认=%v inv=%v", e.repo.rows[id].IsDefault, e.inv.ids)
		}
		if len(e.audit.logs) != 1 || e.audit.logs[0].Action != model.AuditStorageDefault {
			t.Errorf("应写审计：%+v", e.audit.logs)
		}
	})
	t.Run("内置本地存储不需要测试结果", func(t *testing.T) {
		e := newStorEnv(t)
		id := e.seed(func(r *model.StorageConfig) {
			r.Provider = storage.ProviderLocal
			r.Builtin = true
			r.Name = "本地磁盘"
		})
		if err := e.svc.SetDefault(ctx, 9, id); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("没测过 / 不存在", func(t *testing.T) {
		e := newStorEnv(t)
		id := e.seed(nil)
		if err := e.svc.SetDefault(ctx, 9, id); storCode(t, err) != errcode.ErrStorageNotChecked.Code {
			t.Errorf("没测过：%v", err)
		}
		if err := e.svc.SetDefault(ctx, 9, 999); storCode(t, err) != errcode.ErrStorageNotFound.Code {
			t.Errorf("不存在：%v", err)
		}
	})
}

func TestStorageConfigService_Delete(t *testing.T) {
	ctx := context.Background()
	t.Run("无引用的非默认存储可以删除，同时清理密钥缓存", func(t *testing.T) {
		e := newStorEnv(t)
		id := e.seed(nil)
		if err := e.svc.Delete(ctx, 9, id); err != nil {
			t.Fatal(err)
		}
		if len(e.repo.deleted) != 1 || !contains(e.inv.ids, id) || len(e.sec.forgotten) != 1 || e.sec.forgotten[0] != model.StorageSecretName(id) {
			t.Errorf("deleted=%v inv=%v forgotten=%v", e.repo.deleted, e.inv.ids, e.sec.forgotten)
		}
	})
	t.Run("内置 / 默认 / 被引用 都不能删", func(t *testing.T) {
		e := newStorEnv(t)
		builtin := e.seed(func(r *model.StorageConfig) {
			r.Provider = storage.ProviderLocal
			r.Builtin = true
			r.Name = "本地磁盘"
		})
		def := e.seed(func(r *model.StorageConfig) { r.Name = "def"; r.IsDefault = true })
		used := e.seed(func(r *model.StorageConfig) { r.Name = "used" })
		e.repo.assets[used] = 1284
		if err := e.svc.Delete(ctx, 9, builtin); storCode(t, err) != errcode.ErrStorageBuiltin.Code {
			t.Errorf("内置：%v", err)
		}
		if err := e.svc.Delete(ctx, 9, def); storCode(t, err) != errcode.ErrStorageIsDefault.Code {
			t.Errorf("默认：%v", err)
		}
		err := e.svc.Delete(ctx, 9, used)
		if storCode(t, err) != errcode.ErrStorageInUse.Code || !strings.Contains(err.Error(), "1284") {
			t.Errorf("被引用应写明数量：%v", err)
		}
		if len(e.repo.deleted) != 0 {
			t.Error("拒绝删除时不能删任何数据")
		}
	})
	t.Run("DeleteCheck 说明能不能删与原因", func(t *testing.T) {
		e := newStorEnv(t)
		used := e.seed(nil)
		e.repo.assets[used] = 7
		e.repo.intents[used] = 2
		c, err := e.svc.DeleteCheck(ctx, used)
		if err != nil || c.Deletable || c.AssetCount != 7 || c.PendingUploads != 2 || c.Reason == "" {
			t.Errorf("实际 %+v err=%v", c, err)
		}
		free := e.seed(func(r *model.StorageConfig) { r.Name = "free" })
		if c, _ := e.svc.DeleteCheck(ctx, free); !c.Deletable {
			t.Errorf("无引用应可删：%+v", c)
		}
	})
}

// ---- List / Get / Test / Check ----

func TestStorageConfigService_ListAndGet(t *testing.T) {
	ctx := context.Background()
	e := newStorEnv(t)
	a := e.seed(nil)
	b := e.seed(func(r *model.StorageConfig) {
		r.Name = "b"
		r.PublicBaseURL = "https://cdn.example.com"
		r.IsDefault = true
	})
	e.repo.assets[a] = 3

	list, err := e.svc.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("实际 %+v err=%v", list, err)
	}
	if list[0].AssetCount != 3 || !list[0].Locked || list[1].Locked || !list[1].IsDefault || list[1].Access != "public" {
		t.Errorf("素材数 / 锁定 / 默认 / 访问方式不对：%+v %+v", list[0], list[1])
	}
	for _, v := range list {
		if !v.SecretSet {
			t.Errorf("seed 的存储都有密钥：%+v", v)
		}
	}
	if _, err := e.svc.Get(ctx, 999); storCode(t, err) != errcode.ErrStorageNotFound.Code {
		t.Errorf("不存在：%v", err)
	}
	got, _ := e.svc.Get(ctx, b)
	if got.AssetCount != 0 || got.Locked {
		t.Errorf("实际 %+v", got)
	}
}

func TestStorageConfigService_TestAndCheck(t *testing.T) {
	ctx := context.Background()

	t.Run("Test 测试未保存的配置，不落库也不写密钥", func(t *testing.T) {
		e := newStorEnv(t)
		res, err := e.svc.Test(ctx, ossInput())
		if err != nil || !res.OK || len(e.repo.rows) != 0 || len(e.sec.values) != 0 {
			t.Fatalf("实际 %+v err=%v", res, err)
		}
		bad := ossInput()
		bad.Bucket = ""
		if _, err := e.svc.Test(ctx, bad); storCode(t, err) != errcode.ErrStorageInvalid.Code {
			t.Errorf("配置不合法：%v", err)
		}
	})

	t.Run("Check 用已存的密钥重新测试，并记录结果", func(t *testing.T) {
		e := newStorEnv(t)
		id := e.seed(nil)
		e.probe.result = failProbe("桶不存在或地域不对")
		res, err := e.svc.Check(ctx, 9, id)
		if err != nil || res.OK {
			t.Fatalf("实际 %+v err=%v", res, err)
		}
		row := e.repo.rows[id]
		if row.LastCheckOK == nil || *row.LastCheckOK || !strings.Contains(row.LastCheckError, "桶不存在或地域不对") {
			t.Errorf("应记录失败：%+v", row)
		}
		if row.Version != 1 {
			t.Errorf("记录测试结果不应改变版本，实际 %d", row.Version)
		}
	})

	t.Run("Check 内置本地存储：跳过签名地址检查", func(t *testing.T) {
		e := newStorEnv(t)
		id := e.seed(func(r *model.StorageConfig) {
			r.Provider = storage.ProviderLocal
			r.Builtin = true
			r.Name = "本地磁盘"
		})
		res, err := e.svc.Check(ctx, 9, id)
		if err != nil || !res.OK || !res.Steps[3].Skipped {
			t.Fatalf("实际 %+v err=%v", res, err)
		}
		if len(e.probe.specs) != 0 {
			t.Error("本地存储不走对象存储探针")
		}
	})
}

func TestStorageConfigService_Presets(t *testing.T) {
	e := newStorEnv(t)
	ps := e.svc.Presets()
	if len(ps) != 4 || ps[0].Provider != storage.ProviderAliyunOSS || ps[3].Provider != storage.ProviderR2 || ps[3].DirectMethod != storage.DirectPresignedPut {
		t.Errorf("实际 %+v", ps)
	}
}

// ---- storage.Source：给 Registry 提供解密后的运行时配置 ----

func TestStorageConfigService_Source(t *testing.T) {
	ctx := context.Background()

	t.Run("DefaultID 返回默认存储，没有默认存储报错", func(t *testing.T) {
		e := newStorEnv(t)
		if _, err := e.svc.DefaultID(ctx); err == nil {
			t.Fatal("没有默认存储应报错")
		}
		id := e.seed(func(r *model.StorageConfig) { r.IsDefault = true })
		if got, err := e.svc.DefaultID(ctx); err != nil || got != id {
			t.Errorf("实际 %d err=%v", got, err)
		}
	})

	t.Run("对象存储：带上解密后的密钥与策略", func(t *testing.T) {
		e := newStorEnv(t)
		id := e.seed(func(r *model.StorageConfig) { r.SignedTTLSec = 7200; r.DirectUpload = true; r.PathPrefix = "assets" })
		en, err := e.svc.Entry(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if en.Spec.SecretKey != "stored-secret" || en.Spec.AccessKey != "LTAI5tABCDEFGHIJKLM" || en.Spec.Bucket != "vc-bucket" || en.Spec.PathPrefix != "assets" {
			t.Errorf("实际 %+v", en.Spec)
		}
		if en.SignedTTL != 2*time.Hour || !en.DirectUpload || en.Version != 1 || en.Provider != storage.ProviderAliyunOSS {
			t.Errorf("实际 %+v", en)
		}
	})

	t.Run("内置本地存储取 YAML 里的目录", func(t *testing.T) {
		e := newStorEnv(t)
		id := e.seed(func(r *model.StorageConfig) {
			r.Provider = storage.ProviderLocal
			r.Builtin = true
			r.Name = "本地磁盘"
		})
		en, err := e.svc.Entry(ctx, id)
		if err != nil || en.Provider != storage.ProviderLocal || en.Local.Dir == "" {
			t.Fatalf("实际 %+v err=%v", en, err)
		}
	})

	t.Run("存储不存在 / 密钥未设置", func(t *testing.T) {
		e := newStorEnv(t)
		if _, err := e.svc.Entry(ctx, 999); !errors.Is(err, storage.ErrStorageNotFound) {
			t.Errorf("不存在应返回 storage.ErrStorageNotFound：%v", err)
		}
		id := e.seed(nil)
		delete(e.sec.values, model.StorageSecretName(id))
		if _, err := e.svc.Entry(ctx, id); err == nil {
			t.Error("密钥未设置应报错")
		}
	})
}

// ---- 升级：把旧版用环境变量配置的 S3 一次性导入为后台存储 ----

func legacyS3() config.Storage {
	return config.Storage{
		Driver:    "s3",
		SignedTTL: 2 * time.Hour,
		S3: config.S3Storage{Endpoint: "https://oss-cn-hangzhou.aliyuncs.com", Region: "cn-hangzhou", Bucket: "vc-prod",
			AccessKey: "LTAI5tLEGACYLEGACY", SecretKey: "legacy-secret", UseSSL: true, PathPrefix: "assets", PublicBaseURL: "https://cdn.example.com"},
	}
}

func TestStorageConfigService_ImportLegacyS3(t *testing.T) {
	ctx := context.Background()

	t.Run("导入为后台存储：存密钥、旧素材指向它、设为默认", func(t *testing.T) {
		e := newStorEnv(t)
		imported, err := e.svc.ImportLegacyS3(ctx, legacyS3())
		if err != nil || !imported {
			t.Fatalf("实际 imported=%v err=%v", imported, err)
		}
		var row *model.StorageConfig
		for _, r := range e.repo.rows {
			row = r
		}
		if row == nil || row.Provider != storage.ProviderS3 || row.Endpoint != "oss-cn-hangzhou.aliyuncs.com" || row.Bucket != "vc-prod" ||
			row.PathPrefix != "assets" || row.PublicBaseURL != "https://cdn.example.com" || row.SignedTTLSec != 7200 || row.Builtin {
			t.Fatalf("导入的配置不对：%+v", row)
		}
		if row.Addressing != "auto" {
			t.Errorf("应保持原来的自动寻址，实际 %q", row.Addressing)
		}
		if e.sec.values[model.StorageSecretName(row.ID)] != "legacy-secret" {
			t.Errorf("密钥应存到 storage:%d：%v", row.ID, e.sec.values)
		}
		if len(e.repo.backfills) != 1 || e.repo.backfills[0] != row.ID || !row.IsDefault {
			t.Errorf("旧素材应回填到导入的存储并设为默认：backfills=%v default=%v", e.repo.backfills, row.IsDefault)
		}
		if len(e.audit.logs) != 1 || strings.Contains(string(e.audit.logs[0].DetailJSON), "legacy-secret") {
			t.Errorf("应写不含密钥的审计：%+v", e.audit.logs)
		}
	})

	t.Run("已经有后台创建的存储就不再导入（只导入一次）", func(t *testing.T) {
		e := newStorEnv(t)
		e.seed(nil)
		imported, err := e.svc.ImportLegacyS3(ctx, legacyS3())
		if err != nil || imported || len(e.repo.rows) != 1 || len(e.repo.backfills) != 0 {
			t.Fatalf("实际 imported=%v err=%v rows=%d", imported, err, len(e.repo.rows))
		}
	})

	t.Run("内置本地存储不算已有存储", func(t *testing.T) {
		e := newStorEnv(t)
		e.seed(func(r *model.StorageConfig) {
			r.Provider = storage.ProviderLocal
			r.Builtin = true
			r.Name = "本地磁盘"
		})
		if imported, err := e.svc.ImportLegacyS3(ctx, legacyS3()); err != nil || !imported {
			t.Fatalf("实际 imported=%v err=%v", imported, err)
		}
	})

	t.Run("配置不完整：报错并说明缺什么，不留数据", func(t *testing.T) {
		e := newStorEnv(t)
		cfg := legacyS3()
		cfg.S3.SecretKey = ""
		if _, err := e.svc.ImportLegacyS3(ctx, cfg); err == nil || !strings.Contains(err.Error(), "APP_STORAGE_S3_SECRET_KEY") {
			t.Fatalf("应提示缺少哪个环境变量：%v", err)
		}
		cfg = legacyS3()
		cfg.S3.Bucket = ""
		if _, err := e.svc.ImportLegacyS3(ctx, cfg); err == nil {
			t.Fatal("桶名为空应报错")
		}
		if len(e.repo.rows) != 0 {
			t.Error("失败不应留下配置")
		}
	})

	t.Run("写密钥失败要回滚，不能让旧素材指向没有密钥的存储", func(t *testing.T) {
		e := newStorEnv(t)
		e.sec.setErr = errors.New("未配置凭证主密钥")
		if _, err := e.svc.ImportLegacyS3(ctx, legacyS3()); err == nil || !strings.Contains(err.Error(), "APP_AI_SECRET_KEY") {
			t.Fatalf("应提示需要配置 APP_AI_SECRET_KEY：%v", err)
		}
		if len(e.repo.rows) != 0 || len(e.repo.backfills) != 0 {
			t.Errorf("应整体回滚：rows=%d backfills=%v", len(e.repo.rows), e.repo.backfills)
		}
	})
}
