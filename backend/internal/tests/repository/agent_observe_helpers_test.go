package repository_test

import (
	"context"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
	agentsvc "video-canvas/internal/service/agent"
)

// runsOf 让仓储测试里的观测服务只认这一个运行：按用户和 id 读运行。
type runsOf struct{ run *model.AgentRun }

func (r runsOf) GetRun(_ context.Context, userID, id uint64) (*model.AgentRun, error) {
	if id != r.run.ID || userID != r.run.UserID {
		return nil, ErrNotFound
	}
	cp := *r.run
	return &cp, nil
}

// agentsvcNewObserve 用真实的观测仓储装配观测服务，不发事件、不写审计。
func agentsvcNewObserve(repo *AgentObserveRepository, run *model.AgentRun) *agentsvc.ObserveService {
	return agentsvc.NewObserveService(agentsvc.ObserveDeps{Repo: repo, Runs: runsOf{run}})
}

func agentsvcActor() agentsvc.ObserveActor { return agentsvc.ObserveActor{UserID: 9} }
