package storage

// ObjectPath 返回对象在桶内的完整 key：路径前缀（规范成 "" 或 "a/b/"）加业务 key。
// 图片处理服务据此拼出对象的访问地址，规则和 S3Storage 写入时用的 key 一致。
func ObjectPath(pathPrefix, key string) string {
	return normalizePrefix(pathPrefix) + key
}

// ObjectURLPath 返回对象在访问域名下的路径：ObjectPath 按段转义（保留 /），不带前导 /。
func ObjectURLPath(pathPrefix, key string) string {
	return escapePath(ObjectPath(pathPrefix, key))
}
