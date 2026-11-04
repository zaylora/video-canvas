package handler_test

import (
	"archive/zip"
	"bytes"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
)

type dramaRepeatedByte struct{}

func (dramaRepeatedByte) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}

func TestDramaPreflightAllowsLargeDocxAndCapsBody(t *testing.T) {
	// docx 附带大图片仍应在 64MB 内接受，原文只有两字，不能误用旧 32MB 上限。
	var document bytes.Buffer
	archive := zip.NewWriter(&document)
	image, err := archive.CreateHeader(&zip.FileHeader{Name: "word/media/image.bin", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyN(image, dramaRepeatedByte{}, 33<<20); err != nil {
		t.Fatal(err)
	}
	xml, err := archive.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(xml, `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>正文</w:t></w:r></w:p></w:body></w:document>`); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "大文件.docx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(part, &document); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	r, _ := settingsRouter()
	req := httptest.NewRequest("POST", "/drama-sources/preflight", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("33MB docx 应接受: %d %s", w.Code, w.Body.String())
	}
	// 流式构造 65MB 请求，避免为上限测试再分配一整块大内存。
	large := io.MultiReader(strings.NewReader("--drama-boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"原文.txt\"\r\nContent-Type: text/plain\r\n\r\n"), io.LimitReader(dramaRepeatedByte{}, 65<<20), strings.NewReader("\r\n--drama-boundary--\r\n"))
	req = httptest.NewRequest("POST", "/drama-sources/preflight", large)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=drama-boundary")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("超过64MB应拒绝: %d %s", w.Code, w.Body.String())
	}
}
