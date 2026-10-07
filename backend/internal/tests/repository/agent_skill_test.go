package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

func newSkillRepo(t *testing.T) *repository.AgentSkillRepository {
	t.Helper()
	db := isolatedDB(t, &model.AgentSkill{}, &model.AgentSkillVersion{}, &model.AgentSkillImport{})
	return repository.NewAgentSkillRepository(db)
}

func reserve(t *testing.T, r *repository.AgentSkillRepository, name, sha string) (*model.AgentSkill, *model.AgentSkillVersion) {
	t.Helper()
	s, v, err := r.ReserveVersion(context.Background(), repository.ReserveSkillInput{
		Name: name, Title: name, CreatedBy: 7,
		Version: model.AgentSkillVersion{SHA256: sha, Description: "d", PackageKey: "agent-skills/" + name + "/" + sha + ".zip", FilesJSON: datatypes.JSON("[]")},
	})
	if err != nil {
		t.Fatalf("ReserveVersion: %v", err)
	}
	return s, v
}

func TestAgentSkillRepo_VersionLifecycle(t *testing.T) {
	r, ctx := newSkillRepo(t), context.Background()

	s, v1 := reserve(t, r, "demo", "a")
	if v1.Version != 1 || v1.State != model.SkillVersionPending || s.ActiveVersion != nil {
		t.Fatalf("首个版本应是 pending v1，技能无生效版本: %+v %+v", s, v1)
	}
	// pending 对读路径不可见
	if _, err := r.GetVersion(ctx, s.ID, 1); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("pending 版本不应可读: %v", err)
	}
	if _, err := r.FindVersionBySHA(ctx, s.ID, "a"); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("pending 版本不应参与同内容查重: %v", err)
	}

	sk, err := r.PromoteVersion(ctx, v1.ID)
	if err != nil || sk.ActiveVersion == nil || *sk.ActiveVersion != 1 {
		t.Fatalf("首个版本入库后应自动成为生效版本: %+v %v", sk, err)
	}
	if _, err := r.PromoteVersion(ctx, v1.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("重复转正应返回 ErrNotFound: %v", err)
	}

	// 第二个版本：号码递增，不改变生效版本
	_, v2 := reserve(t, r, "demo", "b")
	if v2.Version != 2 {
		t.Fatalf("版本号应递增: %d", v2.Version)
	}
	if sk, err = r.PromoteVersion(ctx, v2.ID); err != nil || *sk.ActiveVersion != 1 {
		t.Fatalf("后续版本不应抢占生效版本: %+v %v", sk, err)
	}
	if got, err := r.FindVersionBySHA(ctx, s.ID, "b"); err != nil || got.Version != 2 {
		t.Errorf("同内容查重: %+v %v", got, err)
	}
	if list, _ := r.ListVersions(ctx, s.ID); len(list) != 2 || list[0].Version != 2 || list[0].BodyText != "" {
		t.Errorf("列表应倒序且不读正文: %+v", list)
	}
}

func TestAgentSkillRepo_DeleteVersionKeepsNumbering(t *testing.T) {
	r, ctx := newSkillRepo(t), context.Background()
	s, v1 := reserve(t, r, "demo", "a")
	_, _ = r.PromoteVersion(ctx, v1.ID)
	_, v2 := reserve(t, r, "demo", "b")
	_, _ = r.PromoteVersion(ctx, v2.ID)

	if err := r.MarkVersionDeleting(ctx, s.ID, 1); !errors.Is(err, repository.ErrInUse) {
		t.Errorf("生效版本不能删: %v", err)
	}
	if err := r.MarkVersionDeleting(ctx, s.ID, 2); err != nil {
		t.Fatalf("删除非生效版本: %v", err)
	}
	if _, err := r.GetVersion(ctx, s.ID, 2); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("deleting 版本应不可读: %v", err)
	}
	if err := r.MarkVersionDeleting(ctx, s.ID, 9); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("不存在的版本: %v", err)
	}
	// 删除后版本号不复用
	if _, v3 := reserve(t, r, "demo", "c"); v3.Version != 3 {
		t.Errorf("删除后版本号不应复用: %d", v3.Version)
	}
	// 清理任务能看到 deleting 的版本，硬删后消失
	garbage, _ := r.ListGarbageVersions(ctx, time.Now().Add(-time.Hour), 10)
	if len(garbage) != 1 || garbage[0].ID != v2.ID {
		t.Fatalf("待清理: %+v", garbage)
	}
	if err := r.DeleteVersionRow(ctx, v2.ID); err != nil {
		t.Fatal(err)
	}
	if garbage, _ = r.ListGarbageVersions(ctx, time.Now().Add(-time.Hour), 10); len(garbage) != 0 {
		t.Errorf("硬删后应清空: %+v", garbage)
	}
}

func TestAgentSkillRepo_EnabledAndDelete(t *testing.T) {
	r, ctx := newSkillRepo(t), context.Background()
	s, v1 := reserve(t, r, "demo", "a")
	_, _ = r.PromoteVersion(ctx, v1.ID)

	if act, err := r.ListActiveVersions(ctx); err != nil || len(act) != 1 || act[s.ID].Version != 1 || act[s.ID].BodyText != "" {
		t.Fatalf("生效版本头信息: %+v %v", act, err)
	}
	if nums, err := r.ListVersionNumbers(ctx); err != nil || len(nums[s.ID]) != 1 || nums[s.ID][0] != 1 {
		t.Fatalf("版本号: %+v %v", nums, err)
	}
	if list, _ := r.ListEnabled(ctx); len(list) != 0 {
		t.Fatalf("默认停用，目录里不应出现: %+v", list)
	}
	if err := r.SetEnabled(ctx, s.ID, true); err != nil {
		t.Fatal(err)
	}
	list, _ := r.ListEnabled(ctx)
	if len(list) != 1 || list[0].Name != "demo" || list[0].VersionID != v1.ID || list[0].Version != 1 {
		t.Fatalf("启用后应出现在目录: %+v", list)
	}
	if err := r.DeleteSkill(ctx, s.ID); !errors.Is(err, repository.ErrInUse) {
		t.Errorf("启用中不能删: %v", err)
	}
	_ = r.SetEnabled(ctx, s.ID, false)
	if err := r.DeleteSkill(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetSkill(ctx, "demo"); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("删除后技能应不存在: %v", err)
	}
	if g, _ := r.ListGarbageVersions(ctx, time.Now(), 10); len(g) != 1 {
		t.Errorf("版本应转入待清理: %+v", g)
	}
	// 同名重新导入得到全新的技能（版本号从 1 开始）
	if s2, v := reserve(t, r, "demo", "z"); s2.ID == s.ID || v.Version != 1 {
		t.Errorf("同名重新导入应是新技能: %+v %+v", s2, v)
	}
}

func TestAgentSkillRepo_PendingGarbageAndImports(t *testing.T) {
	r, ctx := newSkillRepo(t), context.Background()
	_, v := reserve(t, r, "demo", "a")
	if g, _ := r.ListGarbageVersions(ctx, time.Now().Add(-time.Hour), 10); len(g) != 0 {
		t.Errorf("刚建的 pending 不该被回收: %+v", g)
	}
	if g, _ := r.ListGarbageVersions(ctx, time.Now().Add(time.Minute), 10); len(g) != 1 || g[0].ID != v.ID {
		t.Errorf("超时的 pending 应被回收: %+v", g)
	}
	if err := r.DiscardVersion(ctx, v.ID); err != nil {
		t.Fatal(err)
	}

	imp := &model.AgentSkillImport{ID: "11111111-1111-1111-1111-111111111111", UserID: 3, ResultJSON: datatypes.JSON(`{}`), SHA256: "x", PackageKey: "k", ExpiresAt: time.Now().Add(-time.Minute)}
	if err := r.CreateImport(ctx, imp); err != nil {
		t.Fatal(err)
	}
	if exp, _ := r.ListExpiredImports(ctx, time.Now(), 10); len(exp) != 1 {
		t.Errorf("过期暂存: %+v", exp)
	}
	if ok, _ := r.DeleteImport(ctx, imp.ID); !ok {
		t.Error("首次删除应成功")
	}
	if ok, _ := r.DeleteImport(ctx, imp.ID); ok {
		t.Error("重复删除应返回 false，保证只有一个确认者")
	}
	if _, err := r.GetImport(ctx, imp.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("删除后: %v", err)
	}
}
