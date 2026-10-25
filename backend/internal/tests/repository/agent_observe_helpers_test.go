package repository_test

import (
	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
	agentsvc "video-canvas/internal/service/agent"
)

// runsOf 让仓储测试里的观测服务只认这一个运行：按用户和 id 读运行。
// agentsvcNewObserve 用真实的观测仓储装配观测服务，不发事件、不写审计。
func agentsvcNewObserve(repo *AgentObserveRepository, run *model.AgentRun) *agentsvc.ObserveService {
	return agentsvc.NewObserveService(agentsvc.ObserveDeps{Repo: repo})
}

func agentsvcActor() agentsvc.ObserveActor { return agentsvc.ObserveActor{UserID: 9} }
