package service

import (
	"slices"

	"video-canvas/internal/model"
)

// dramaJobsConflict 两条任务线互不阻塞；用户文本互斥，媒体仅同集互斥。
func dramaJobsConflict(jobs []model.DramaJob, req *model.DramaJobReq) bool {
	text := func(step string) bool { return step == "scripts" || step == "breakdown" || step == "asset_prompts" }
	for _, j := range jobs {
		if pipelineJob(j) {
			continue
		}
		if text(req.Step) {
			if text(j.Step) {
				return true
			}
			continue
		}
		if text(j.Step) {
			continue
		}
		if len(j.Episodes) == 0 {
			return true
		}
		for _, no := range req.Episodes {
			if slices.Contains(j.Episodes, no) {
				return true
			}
		}
	}
	return false
}
