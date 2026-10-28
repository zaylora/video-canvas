package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

type composeMemory struct {
	*dramaMemory
	tasks       []model.GenerationTask
	assets      []model.Asset
	creditCalls int
	insertErr   error
}

func newComposeMemory() *composeMemory {
	r := &composeMemory{dramaMemory: newDramaMemory()}
	r.episodes = []model.DramaEpisode{{BaseModel: model.BaseModel{ID: 3}, UserID: 7, DramaID: 1, No: 1, Revision: 1}}
	canvas, chosen := uint64(2), uint64(20)
	r.segments = []model.DramaSegment{{BaseModel: model.BaseModel{ID: 5}, UserID: 7, EpisodeID: 3, Seq: 0, ChosenTaskID: &chosen}, {BaseModel: model.BaseModel{ID: 6}, UserID: 7, EpisodeID: 3, Seq: 1}}
	r.tasks = []model.GenerationTask{{ID: chosen, UserID: 7, CanvasProjectID: &canvas, NodeID: "seg:" + idcodec.Encode(5), Kind: model.KindVideo, Status: model.TaskSucceeded, OutputJSON: []byte(`[{"asset_id":30,"media_type":"video"}]`)}}
	return r
}

func (r *composeMemory) WithTx(ctx context.Context, fn func(repository.DramaTx) error) error {
	before := append([]model.Asset{}, r.assets...)
	err := r.dramaMemory.WithTx(ctx, func(repository.DramaTx) error { return fn(r) })
	if err != nil {
		r.assets = before
	}
	return err
}
func (r *composeMemory) SegmentTasks(_ context.Context, user, canvas uint64, node string) ([]model.GenerationTask, error) {
	out := []model.GenerationTask{}
	for _, task := range r.tasks {
		if task.UserID == user && task.CanvasProjectID != nil && *task.CanvasProjectID == canvas && task.NodeID == node && !task.IsTest {
			out = append(out, task)
		}
	}
	return out, nil
}
func (r *composeMemory) CreateAsset(_ context.Context, asset *model.Asset) error {
	if r.insertErr != nil {
		return r.insertErr
	}
	asset.ID = 99
	r.assets = append(r.assets, *asset)
	return nil
}
func (r *composeMemory) JobByApproval(_ context.Context, user, approval uint64) (*model.DramaJob, error) {
	for _, job := range r.jobs {
		if job.UserID == user && job.ApprovalID != nil && *job.ApprovalID == approval {
			return &job, nil
		}
	}
	return nil, repository.ErrNotFound
}
func (r *composeMemory) EnsureCredit(context.Context, uint64, int) error {
	r.creditCalls++
	return errors.New("compose 不应创建积分账户")
}
func (r *composeMemory) LockCredit(context.Context, uint64) (*model.UserCredit, error) {
	r.creditCalls++
	return nil, errors.New("compose 不应锁积分")
}

type composeFiles struct {
	r           *composeMemory
	prepared    int
	discarded   int
	downloadErr error
}

func (a *composeFiles) Open(_ context.Context, user, id uint64) (*provider.AssetFile, error) {
	if a.downloadErr != nil {
		return nil, a.downloadErr
	}
	if user != 7 || id != 30 {
		return nil, provider.ErrAssetNotFound
	}
	return &provider.AssetFile{Asset: &model.Asset{ID: id, UserID: user, Kind: "video"}, Body: io.NopCloser(strings.NewReader("video"))}, nil
}
func (a *composeFiles) PrepareDramaVideo(_ context.Context, user uint64, path string) (*model.Asset, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	a.prepared++
	return &model.Asset{UserID: user, Kind: "video", Source: model.AssetSourceUpload, StorageKey: "compose.mp4"}, nil
}
func (a *composeFiles) DiscardCopies(_ context.Context, assets []*model.Asset) {
	a.discarded += len(assets)
}
func (a *composeFiles) View(_ context.Context, user, id uint64) (*model.AssetView, error) {
	if user != 7 {
		return nil, repository.ErrNotFound
	}
	return &model.AssetView{ID: id, Kind: "video", URL: "/files/compose.mp4"}, nil
}
func (a *composeFiles) ViewOf(_ context.Context, asset *model.Asset) (*model.AssetView, error) {
	return &model.AssetView{ID: asset.ID, Kind: "video", URL: "/files/compose.mp4"}, nil
}

type composeProcessor struct {
	calls  int
	during func()
	err    error
	inputs []string
}

func (p *composeProcessor) Compose(_ context.Context, paths []string, dst string) error {
	p.calls++
	p.inputs = append([]string{}, paths...)
	if p.during != nil {
		p.during()
	}
	if p.err != nil {
		return p.err
	}
	return os.WriteFile(dst, []byte("composed"), 0600)
}
func composeService(r *composeMemory, a *composeFiles, p *composeProcessor) *service.DramaService {
	return service.NewDramaService(service.DramaDeps{Repo: r, Segments: r, Assets: a, Compose: p})
}

func TestDramaCompose_ValidationWithoutModelOrCredits(t *testing.T) {
	for _, tc := range []struct {
		name        string
		scope       []int
		user        uint64
		empty, busy bool
		code        int
	}{
		{"成功", []int{1}, 7, false, false, 0},
		{"没有采用", []int{1}, 7, true, false, 67010},
		{"并发", []int{1}, 7, false, true, 67004},
		{"必须单集", []int{1, 2}, 7, false, false, 10001},
		{"必须选集", nil, 7, false, false, 10001},
		{"他人资源", []int{1}, 8, false, false, 67001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newComposeMemory()
			if tc.empty {
				r.segments[0].ChosenTaskID = nil
			}
			if tc.busy {
				r.jobs = []model.DramaJob{{Status: "running"}}
			}
			s := composeService(r, &composeFiles{r: r}, &composeProcessor{})
			req := &model.DramaJobReq{Step: "compose", Episodes: tc.scope}
			_, err := s.CreateJob(context.Background(), tc.user, 1, req)
			if tc.code != 0 {
				dramaCheckCode(t, err, tc.code)
			} else if err != nil {
				t.Fatal(err)
			}
			if r.creditCalls != 0 || len(r.ledger) != 0 {
				t.Fatal("合成触碰积分")
			}
		})
	}
}

func TestDramaCompose_ResultIdempotenceAndStale(t *testing.T) {
	r := newComposeMemory()
	a, p := &composeFiles{r: r}, &composeProcessor{}
	s := composeService(r, a, p)
	ctx := context.Background()
	quote, err := s.Estimate(ctx, 7, 1, &model.DramaJobReq{Step: "compose", Episodes: []int{1}})
	if err != nil || quote != 0 {
		t.Fatalf("估价 %d: %v", quote, err)
	}
	job, err := s.CreateAgentJob(ctx, 7, 1, 55, &model.DramaJobReq{Step: "compose", Episodes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.CreateAgentJob(ctx, 7, 1, 55, &model.DramaJobReq{Step: "compose", Episodes: []int{1}})
	if err != nil || again.ID != job.ID {
		t.Fatalf("审批重复建任务: %+v %v", again, err)
	}
	if err := s.HandleDrama(ctx, 7, 1, uint64(job.ID)); err != nil {
		t.Fatal(err)
	}
	if err := s.HandleDrama(ctx, 7, 1, uint64(job.ID)); err != nil {
		t.Fatal(err)
	}
	jobs, err := s.ListJobs(ctx, 7, 1, false)
	if err != nil || len(jobs) != 1 || jobs[0].Status != "succeeded" || jobs[0].Result == nil {
		t.Fatalf("结果 %+v %v", jobs, err)
	}
	if got := jobs[0].Result; got.Asset.ID != 99 || len(got.SkippedSeqs) != 1 || got.SkippedSeqs[0] != 1 {
		t.Fatalf("合成结果 %+v", got)
	}
	if p.calls != 1 || a.prepared != 1 || len(r.assets) != 1 || r.creditCalls != 0 {
		t.Fatalf("重复执行或扣费: processor=%d prepared=%d assets=%d credits=%d", p.calls, a.prepared, len(r.assets), r.creditCalls)
	}
	for _, path := range p.inputs {
		if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("下载临时文件未清理: %v", err)
		}
	}
	view, err := s.Get(ctx, 7, 1)
	if err != nil || view.Episodes[0].Final == nil || view.Episodes[0].FinalStale {
		t.Fatalf("成片概要 %+v %v", view, err)
	}
	r.episodes[0].SegmentVersion++
	view, err = s.Get(ctx, 7, 1)
	if err != nil || !view.Episodes[0].FinalStale {
		t.Fatalf("片段变化未标记过期: %+v %v", view, err)
	}
	other := r.jobs[0]
	other.Step = "scripts"
	r.jobs = []model.DramaJob{other}
	jobs, _ = s.ListJobs(ctx, 7, 1, false)
	if jobs[0].Result != nil {
		t.Fatal("其他任务泄露合成结果")
	}
}

func TestDramaCompose_CancelFailureAndRetry(t *testing.T) {
	for _, kind := range []string{"cancel", "download", "transcode", "commit"} {
		t.Run(kind, func(t *testing.T) {
			r := newComposeMemory()
			a := &composeFiles{r: r}
			p := &composeProcessor{}
			s := composeService(r, a, p)
			ctx := context.Background()
			job, err := s.CreateJob(ctx, 7, 1, &model.DramaJobReq{Step: "compose", Episodes: []int{1}})
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "cancel":
				p.during = func() {
					if _, err := s.CancelJob(ctx, 7, 1, uint64(job.ID)); err != nil {
						t.Fatal(err)
					}
				}
			case "download":
				a.downloadErr = errors.New("private storage secret")
			case "transcode":
				p.err = errors.New("private ffmpeg path")
			case "commit":
				r.insertErr = errors.New("database unavailable")
			}
			err = s.HandleDrama(ctx, 7, 1, uint64(job.ID))
			if kind == "commit" {
				if err == nil {
					t.Fatal("事务失败必须重试")
				}
				r.insertErr = nil
				if err = s.HandleDrama(ctx, 7, 1, uint64(job.ID)); err != nil {
					t.Fatal(err)
				}
				if len(r.assets) != 1 {
					t.Fatal("重试重复素材")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(r.assets) != 0 || r.episodes[0].FinalAssetID != nil || r.creditCalls != 0 {
				t.Fatal("取消失败发布了成片或扣费")
			}
			if kind != "cancel" && (r.jobs[0].ErrorCode != 10000 || !strings.Contains(r.jobs[0].ErrorMessage, map[string]string{"download": "下载失败", "transcode": "转码失败"}[kind]) || strings.Contains(r.jobs[0].ErrorMessage, "private")) {
				t.Fatalf("失败原因 %+v", r.jobs[0])
			}
		})
	}
}

func TestDramaCompose_TaskOwnership(t *testing.T) {
	for _, mutate := range []func(*model.GenerationTask){
		func(task *model.GenerationTask) { task.UserID = 8 },
		func(task *model.GenerationTask) { task.NodeID = "seg:6" },
		func(task *model.GenerationTask) { task.IsTest = true },
		func(task *model.GenerationTask) { task.Status = model.TaskFailed },
	} {
		r := newComposeMemory()
		mutate(&r.tasks[0])
		s := composeService(r, &composeFiles{r: r}, &composeProcessor{})
		_, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "compose", Episodes: []int{1}})
		if err == nil {
			raw, _ := json.Marshal(r.tasks)
			t.Fatalf("合成接受非法采用任务: %s", raw)
		}
	}
}

func (r *composeMemory) SegmentEpisode(_ context.Context, user, id uint64) (*model.DramaEpisode, error) {
	for i := range r.episodes {
		if r.episodes[i].UserID == user && r.episodes[i].ID == id {
			ep := r.episodes[i]
			return &ep, nil
		}
	}
	return nil, repository.ErrNotFound
}
