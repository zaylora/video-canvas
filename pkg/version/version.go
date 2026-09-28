// Package version 保存构建信息，编译时通过 -ldflags 注入：
//
//	go build -ldflags "-X video-canvas/pkg/version.Version=v1.0.0 -X video-canvas/pkg/version.GitCommit=$(git rev-parse --short HEAD)"
package version

import "runtime"

var (
	Version   = "dev"
	GitCommit = "none"
	BuildTime = "unknown"
)

type Info struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
}

func Get() Info {
	return Info{
		Version:   Version,
		GitCommit: GitCommit,
		BuildTime: BuildTime,
		GoVersion: runtime.Version(),
	}
}
