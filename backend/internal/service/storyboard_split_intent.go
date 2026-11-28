package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

type storyboardSplitIntent struct {
	key        *string
	hash       string
	maxCredits *int
}

var storyboardRequestKey = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func newStoryboardSplitIntent(req *model.StoryboardSplitReq, script string) (storyboardSplitIntent, error) {
	var intent storyboardSplitIntent
	if req.RequestKey == "" && (req.Collaboration == nil || req.Collaboration.MaxCredits == nil) {
		return intent, nil
	}
	if !storyboardRequestKey.MatchString(req.RequestKey) || req.Collaboration == nil || req.Collaboration.MaxCredits == nil || *req.Collaboration.MaxCredits < 0 {
		return intent, errcode.ErrInvalidParams.WithMsg("协作拆分镜需要请求编号和本次积分上限")
	}
	canonical := *req
	canonical.Script = script
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return intent, errcode.ErrInvalidParams
	}
	digest := sha256.Sum256(encoded)
	return storyboardSplitIntent{key: &canonical.RequestKey, hash: hex.EncodeToString(digest[:]), maxCredits: canonical.Collaboration.MaxCredits}, nil
}

func (s *StoryboardService) findStoryboardSplit(ctx context.Context, user, canvas uint64, intent storyboardSplitIntent) (*model.StoryboardSplitResp, error) {
	if intent.key == nil {
		return nil, nil
	}
	repo, ok := s.repo.(interface {
		SplitJobByRequest(context.Context, uint64, string) (*model.StoryboardSplitJob, error)
	})
	if !ok {
		return nil, errcode.ErrInternal
	}
	job, err := repo.SplitJobByRequest(ctx, user, *intent.key)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if job.CanvasID != canvas || job.RequestHash != intent.hash {
		return nil, errcode.ErrCanvasConflict.WithMsg("拆分镜编号已绑定其他请求")
	}
	if err := s.checkSplitAccess(ctx, job); err != nil {
		return nil, collaborationError(err)
	}
	return splitJobView(job), nil
}
