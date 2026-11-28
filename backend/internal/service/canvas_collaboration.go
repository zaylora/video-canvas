package service

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"video-canvas/internal/canvasdoc"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/repository"
)

// CanvasCollaborationService coordinates collaboration authorization and client-safe views.
type CanvasCollaborationService struct {
	repo    *repository.CanvasCollaborationRepository
	shares  *repository.CanvasShareRepository
	reserve CanvasDeliveryGuard
	enabled bool
	bus     *ws.CanvasCollaborationBus
	media   canvasGenerationMediaStore
	drafts  canvasMediaDraftStore
	compose *CanvasComposeService
	users   CanvasUsernameDirectory
}

// CanvasUsernameDirectory 查询画布协作记录涉及的用户名。
type CanvasUsernameDirectory interface {
	// ListUsernames 批量返回匹配的账号及其用户名。
	ListUsernames(context.Context, []uint64) (map[uint64]string, error)
}

// WithUsernameDirectory 为协作记录提供账号名称。
func (s *CanvasCollaborationService) WithUsernameDirectory(users CanvasUsernameDirectory) *CanvasCollaborationService {
	s.users = users
	return s
}

func (s *CanvasCollaborationService) usernames(ctx context.Context, ids []uint64) (map[uint64]string, error) {
	if s.users == nil || len(ids) == 0 {
		return nil, nil
	}
	return s.users.ListUsernames(ctx, ids)
}

// WithRoomBus attaches live revision notifications to committed changes.
func (s *CanvasCollaborationService) WithRoomBus(bus *ws.CanvasCollaborationBus) *CanvasCollaborationService {
	s.bus = bus
	return s
}

// WithMediaImports 接入成员素材的独立对象存储。
func (s *CanvasCollaborationService) WithMediaImports(store canvasGenerationMediaStore) *CanvasCollaborationService {
	s.media = store
	return s
}

// NewCanvasCollaborationService wires the canvas repository and the activation gate.
func NewCanvasCollaborationService(repo *repository.CanvasCollaborationRepository, shares *repository.CanvasShareRepository, reserve CanvasDeliveryGuard, enabled bool) *CanvasCollaborationService {
	return &CanvasCollaborationService{repo: repo, shares: shares, reserve: reserve, enabled: enabled}
}

// CanvasCollaborationSnapshot contains the authoritative graph and role for a session.
type CanvasCollaborationSnapshot struct {
	AutomationActor       string                          `json:"automationActor,omitempty"`
	CumulativeBudget      bool                            `json:"cumulativeBudget,omitempty"`
	GenerationBudgetLimit bool                            `json:"generationBudgetLimit"`
	StoryboardBudgetLimit bool                            `json:"storyboardBudgetLimit"`
	CanGenerate           bool                            `json:"canGenerate"`
	CanCopyToLibrary      bool                            `json:"canCopyToLibrary"`
	Epoch                 uint64                          `json:"epoch"`
	Role                  string                          `json:"role"`
	Canvas                *model.CanvasProjectView        `json:"canvas"`
	Capabilities          CanvasCollaborationCapabilities `json:"capabilities"`
}

// CanvasCollaborationCapabilities 描述该协议版本向当前调用者开放的基本操作。
type CanvasCollaborationCapabilities struct {
	ProtocolVersion    int      `json:"protocolVersion"`
	MaxRecordsPerBatch int      `json:"maxRecordsPerBatch"`
	MaxBatchBytes      int      `json:"maxBatchBytes"`
	Actions            []string `json:"actions"`
}

func canvasCollaborationCapabilities(role string, canGenerate, canCollect bool) CanvasCollaborationCapabilities {
	actions := []string{"snapshot", "replay", "presence", "media-download"}
	if role == model.CanvasRoleEditor || role == model.CanvasRoleOwner {
		actions = append(actions, "commit", "undo", "receipt", "history", "comments-read", "comments-write", "comments-replay", "task-summaries")
		if canGenerate {
			actions = append(actions, "generate", "storyboard-split", "character-voice")
		}
		if canCollect {
			actions = append(actions, "library-collect")
		}
	}
	return CanvasCollaborationCapabilities{ProtocolVersion: 1, MaxRecordsPerBatch: model.CanvasCollaborationMaxBatchRecords,
		MaxBatchBytes: model.CanvasCollaborationMaxBatchBytes, Actions: actions}
}

// SetGenerationPermission 更新编辑成员的生成授权，并通知现有连接对账。
func (s *CanvasCollaborationService) SetGenerationPermission(ctx context.Context, owner, canvas, member uint64, enabled bool) error {
	if err := s.shares.SetGenerationPermission(ctx, owner, canvas, member, enabled); err != nil {
		return collaborationError(err)
	}
	s.notice(ctx, canvas, 0, 0)
	return nil
}

// CanvasCollaborationMode is the committed mode and version returned after a mode change.
type CanvasCollaborationMode struct {
	Active   bool   `json:"active"`
	Epoch    uint64 `json:"epoch"`
	Revision uint64 `json:"revision"`
}

// SetMode changes the authoritative representation for a canvas owned by the caller.
func (s *CanvasCollaborationService) SetMode(ctx context.Context, user, id, revision uint64, active bool, preview string) (*CanvasCollaborationMode, error) {
	if active {
		if !s.enabled {
			return nil, errcode.ErrCanvasCollaborationDisabled
		}
		if _, err := s.shares.Source(ctx, user, id); err != nil {
			return nil, collaborationError(err)
		}
		if s.reserve != nil {
			release, err := s.reserve(ctx, user, id)
			if err != nil {
				return nil, err
			}
			defer release()
		}
	}
	row, err := s.repo.SetActiveConfirmed(ctx, user, id, revision, active, preview)
	if err != nil {
		return nil, collaborationError(err)
	}
	s.notice(ctx, id, row.CollaborationEpoch, row.Revision)
	return &CanvasCollaborationMode{Active: row.CollaborationActive, Epoch: row.CollaborationEpoch, Revision: row.Revision}, nil
}

// ModePreview 向所有者展示目标协作模式的影响。
func (s *CanvasCollaborationService) ModePreview(ctx context.Context, user, id uint64, active bool) (*repository.CanvasModePreview, error) {
	if active && !s.enabled {
		return nil, errcode.ErrCanvasCollaborationDisabled
	}
	preview, err := s.repo.ModePreview(ctx, user, id, active)
	return preview, collaborationError(err)
}

// SetMember grants or replaces the role of a member addressed by email.
func (s *CanvasCollaborationService) SetMember(ctx context.Context, owner, id uint64, email, role string) error {
	if _, err := s.shares.Source(ctx, owner, id); err != nil {
		return collaborationError(err)
	}
	member, err := s.shares.Recipient(ctx, strings.ToLower(strings.TrimSpace(email)))
	if errors.Is(err, repository.ErrNotFound) || member == owner {
		return errcode.ErrInvalidParams.WithMsg("请输入其他可接收用户的邮箱")
	}
	if err != nil {
		return err
	}
	if err := s.shares.GrantRole(ctx, owner, id, member, role); err != nil {
		return collaborationError(err)
	}
	s.notice(ctx, id, 0, 0)
	return nil
}

// Snapshot loads an authorized collaboration session with scoped asset references for members.
func (s *CanvasCollaborationService) Snapshot(ctx context.Context, user, id uint64) (*CanvasCollaborationSnapshot, error) {
	row, role, err := s.repo.Snapshot(ctx, user, id)
	if err != nil {
		return nil, collaborationError(err)
	}
	view := row.View()
	key, scoped := model.CanvasAutomationFromContext(ctx)
	if role != model.CanvasRoleOwner || scoped {
		project := canvasdoc.ProjectShared
		if role == model.CanvasRoleEditor || scoped {
			project = canvasdoc.ProjectCollaboration
		}
		view, err = canvasSharedViewWith(row, project)
		if err != nil {
			return nil, err
		}
	}
	capabilities := canvasCollaborationCapabilities(role, row.CollaborationCanGenerate, row.CollaborationCanCopyToLibrary)
	if s.drafts != nil && (role == model.CanvasRoleOwner || role == model.CanvasRoleEditor) {
		capabilities.Actions = append(capabilities.Actions, "media-stage", "media-attach")
	}
	if s.compose.Available() && (role == model.CanvasRoleOwner || role == model.CanvasRoleEditor) {
		capabilities.Actions = append(capabilities.Actions, "compose")
	}
	if scoped {
		actions := []string{"snapshot", "replay", "receipt"}
		if key.CanEdit() {
			actions = append(actions, "commit")
		}
		canGenerate := key.Scope == model.CanvasAutomationGenerate && key.BudgetCredits != nil && row.CollaborationCanGenerate
		if canGenerate {
			actions = append(actions, "generation")
		}
		capabilities.Actions = actions
		return &CanvasCollaborationSnapshot{Epoch: row.CollaborationEpoch, Role: role, Canvas: view,
			Capabilities: capabilities, AutomationActor: strconv.FormatUint(user, 10),
			CanGenerate: canGenerate, GenerationBudgetLimit: canGenerate}, nil
	}
	return &CanvasCollaborationSnapshot{Epoch: row.CollaborationEpoch, Role: role, Canvas: view,
		Capabilities: capabilities,
		CanGenerate:  row.CollaborationCanGenerate, CanCopyToLibrary: row.CollaborationCanCopyToLibrary, GenerationBudgetLimit: true, StoryboardBudgetLimit: true,
		CumulativeBudget: row.CollaborationCanGenerate}, nil
}

// Commit applies an authorized edit and returns the durable confirmation.
func (s *CanvasCollaborationService) Commit(ctx context.Context, user, id uint64, batch repository.CanvasCollaborationBatch) (*repository.CanvasCollaborationCommit, error) {
	staged, err := s.StageMediaImports(ctx, user, id, batch)
	if err != nil {
		return nil, collaborationError(err)
	}
	var fileURL func(string) string
	if s.media != nil {
		fileURL = s.media.FileURL
	}
	result, err := s.repo.CommitWithImports(ctx, user, id, batch, staged, fileURL)
	if err == nil && len(result.Changes) > 0 {
		s.notice(ctx, id, result.Epoch, result.Revision)
	}
	return result, collaborationError(err)
}

// ReceiptStatus 返回当前编辑者自己的操作结果摘要。
func (s *CanvasCollaborationService) ReceiptStatus(ctx context.Context, user, id, epoch uint64, opID string) (*repository.CanvasCollaborationReceiptStatus, error) {
	result, err := s.repo.ReceiptStatus(ctx, user, id, epoch, opID)
	return result, collaborationError(err)
}

// Undo 条件式回退当前编辑者最近确认的修改。
func (s *CanvasCollaborationService) Undo(ctx context.Context, user, id uint64, req repository.CanvasCollaborationUndo) (*repository.CanvasCollaborationCommit, error) {
	result, err := s.repo.Undo(ctx, user, id, req)
	if err == nil && len(result.Changes) > 0 {
		s.notice(ctx, id, result.Epoch, result.Revision)
	}
	return result, collaborationError(err)
}

func (s *CanvasCollaborationService) notice(ctx context.Context, canvas, epoch, revision uint64) {
	if s.bus != nil {
		s.bus.Notice(ctx, ws.CanvasCollaborationNotice{CanvasID: idcodec.Encode(canvas), Epoch: epoch, Revision: revision})
	}
}

// Manage applies an owner-confirmed rename or archive.
func (s *CanvasCollaborationService) Manage(ctx context.Context, user, canvas uint64, req model.CanvasManagementReq) (*model.CanvasProject, error) {
	if req.Archive && s.reserve != nil {
		if _, err := s.shares.Source(ctx, user, canvas); err != nil {
			return nil, collaborationError(err)
		}
		release, err := s.reserve(ctx, user, canvas)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	project, err := s.repo.Manage(ctx, user, canvas, req)
	if err == nil && project.CollaborationActive {
		if req.Archive {
			s.notice(ctx, canvas, 0, 0)
		} else {
			s.notice(ctx, canvas, project.CollaborationEpoch, project.Revision)
		}
	}
	return project, collaborationError(err)
}

// ArchivePreview 返回归档前绑定确认的版本和成员影响范围。
func (s *CanvasCollaborationService) ArchivePreview(ctx context.Context, user, canvas uint64) (*model.CanvasArchivePreview, error) {
	preview, err := s.repo.ArchivePreview(ctx, user, canvas)
	return preview, collaborationError(err)
}

// Replay returns a page of authorized events after the client's last confirmed version.
func (s *CanvasCollaborationService) Replay(ctx context.Context, user, id, epoch, after uint64, limit int) (*repository.CanvasCollaborationReplay, error) {
	result, err := s.repo.Replay(ctx, user, id, epoch, after, limit)
	return result, collaborationError(err)
}

func collaborationError(err error) error {
	switch {
	case errors.Is(err, repository.ErrCanvasGenerationBudget):
		return errcode.ErrGenerationBudget.WithMsg(err.Error())
	case errors.Is(err, repository.ErrCanvasCollaborationSession):
		return errcode.ErrCanvasCollaborationSession
	case errors.Is(err, repository.ErrCanvasCollaborationOpID), errors.Is(err, repository.ErrCanvasMediaDraftStale):
		return errcode.ErrCanvasConflict.WithMsg(err.Error())
	case errors.Is(err, repository.ErrCanvasCollaborationInvalidBatch), errors.Is(err, repository.ErrCanvasCollaborationInvalidDocument):
		return errcode.ErrInvalidParams.WithMsg(err.Error())
	default:
		return canvasShareError(err)
	}
}
