package llmgateway

import (
	"net/url"
	"strings"
)

// geminiEndpoint 保留渠道前缀，并把上游模型名转义为单个路径段。
func geminiEndpoint(base *url.URL, model string) string {
	return strings.TrimRight(base.String(), "/") + "/v1beta/models/" + url.PathEscape(model) + ":streamGenerateContent?alt=sse"
}
