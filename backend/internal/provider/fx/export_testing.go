package fx

import (
	"net/http"
	"time"
)

// SetClientForTest 替换外部传输与时钟，测试不依赖实时公网汇率。
func (s *Service) SetClientForTest(client *http.Client, now func() time.Time) {
	s.client = client
	s.now = now
}
