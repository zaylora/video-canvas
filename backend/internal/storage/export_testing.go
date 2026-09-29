package storage

// 本文件只为 internal/tests 下的外部测试包暴露包内符号，业务代码不要引用。

var (
	EscapePath      = escapePath
	NormalizePrefix = normalizePrefix
)

func (s *S3Storage) ObjectKey(key string) (string, error) { return s.objectKey(key) }

func (l *LocalStorage) Dir() string { return l.dir }
