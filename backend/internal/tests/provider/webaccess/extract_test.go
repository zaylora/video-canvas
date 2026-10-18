package webaccess_test

import (
	"net/url"
	"strings"
	"testing"

	"video-canvas/internal/provider/webaccess"
)

const articleHTML = `<!doctype html><html><head><meta charset="utf-8"><title> 港口的清晨 - 示例站 </title>
<style>.x{color:red}</style><script>var secret = "不该出现";</script></head>
<body>
<nav><a href="/">首页</a><a href="/about">关于</a></nav>
<header>站点页眉</header>
<main>
  <article>
    <h1>港口的清晨</h1>
    <p>天刚亮，<b>渔船</b>陆续靠岸。</p>
    <h2>人物</h2>
    <ul><li>林夏：摄影师</li><li>老周：船长</li></ul>
    <p>参考：<a href="/refs/1">资料一</a> 与 <a href="https://other.example/x">资料二</a>。</p>
    <pre><code>fade_in(2s)
cut()</code></pre>
    <table><tr><th>镜头</th><th>时长</th></tr><tr><td>远景</td><td>3s</td></tr></table>
  </article>
</main>
<aside>相关推荐：广告</aside>
<footer>版权所有</footer>
<script>track()</script>
</body></html>`

func TestExtractPage(t *testing.T) {
	base, _ := url.Parse("https://site.example/posts/1")
	title, text, err := webaccess.ExtractPage(strings.NewReader(articleHTML), base)
	if err != nil {
		t.Fatal(err)
	}
	if title != "港口的清晨 - 示例站" {
		t.Errorf("标题应去掉首尾空白: %q", title)
	}
	for _, want := range []string{
		"# 港口的清晨", "天刚亮，渔船陆续靠岸。", "## 人物", "- 林夏：摄影师", "- 老周：船长",
		"[资料一](https://site.example/refs/1)", "[资料二](https://other.example/x)", "fade_in(2s)", "镜头 | 时长", "远景 | 3s",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("正文应包含 %q:\n%s", want, text)
		}
	}
	for _, bad := range []string{"不该出现", "color:red", "首页", "站点页眉", "相关推荐", "版权所有", "track()"} {
		if strings.Contains(text, bad) {
			t.Errorf("导航、页眉页脚、脚本和样式不该进正文，出现了 %q:\n%s", bad, text)
		}
	}
	if strings.Contains(text, "\n\n\n") {
		t.Errorf("不该有连续的空行: %q", text)
	}
}

func TestExtractPage_NoArticleFallsBackToBody(t *testing.T) {
	base, _ := url.Parse("https://site.example/")
	_, text, err := webaccess.ExtractPage(strings.NewReader(`<html><body><div><p>只有正文</p></div><script>x()</script></body></html>`), base)
	if err != nil || !strings.Contains(text, "只有正文") || strings.Contains(text, "x()") {
		t.Errorf("没有 article 和 main 就取 body: %q %v", text, err)
	}
}

func TestExtractPage_DropsUnsafeLinks(t *testing.T) {
	base, _ := url.Parse("https://site.example/")
	_, text, _ := webaccess.ExtractPage(strings.NewReader(`<body><p><a href="javascript:alert(1)">点我</a> <a href="mailto:a@b.c">邮件</a> <a href="/ok">好</a></p></body>`), base)
	if strings.Contains(text, "javascript:") || strings.Contains(text, "mailto:") {
		t.Errorf("只保留 http(s) 链接: %q", text)
	}
	if !strings.Contains(text, "点我") || !strings.Contains(text, "[好](https://site.example/ok)") {
		t.Errorf("文字照留: %q", text)
	}
}

func TestExtractPage_Empty(t *testing.T) {
	base, _ := url.Parse("https://site.example/")
	_, text, err := webaccess.ExtractPage(strings.NewReader(`<html><head><title>空</title></head><body></body></html>`), base)
	if err != nil || strings.TrimSpace(text) != "" {
		t.Errorf("空页面返回空正文而不是报错: %q %v", text, err)
	}
}
