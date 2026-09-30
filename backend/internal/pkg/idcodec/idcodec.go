// Package idcodec 把数据库里的 uint64 主键和对外的十六进制串互相转换。
//
// 编码是可逆的：id 和固定校验串拼成一个 16 字节的块，用 AES-128 加密，再转 32 位小写十六进制。
// 解码时解密并校验尾部，格式不对、被改过或密钥不同的串都会被拒绝，所以不需要在库里加列或做数据迁移。
// 密钥换了以后旧的串全部失效（例如书签里的画布地址），生产环境要固定密钥。
package idcodec

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync/atomic"
)

const (
	// EncodedLen 是编码后十六进制串的长度。
	EncodedLen = 32
	// devKey 是没调用 Init 时的密钥（单元测试等）；服务启动时一定会 Init。
	devKey = "video-canvas-dev-id-key"
)

// tag 是拼在 id 后面的固定校验串，解码时校验它来识别伪造 / 损坏 / 密钥不符的串。
var tag = [8]byte{'v', 'c', 'a', 'n', 'v', 'a', 's', '!'}

var block atomic.Pointer[cipher.Block]

func init() { Init(devKey) }

// Init 用 key 派生 AES 密钥并设为全局编码器。key 不能为空由调用方保证；同一环境的所有实例必须用同一个 key。
func Init(key string) {
	sum := sha256.Sum256([]byte("video-canvas/idcodec/" + key))
	b, err := aes.NewCipher(sum[:16])
	if err != nil { // 密钥长度固定 16 字节，不会失败
		panic(err)
	}
	block.Store(&b)
}

// Encode 把主键编码成 32 位十六进制串。
func Encode(id uint64) string {
	var plain, out [16]byte
	binary.BigEndian.PutUint64(plain[:8], id)
	copy(plain[8:], tag[:])
	(*block.Load()).Encrypt(out[:], plain[:])
	return hex.EncodeToString(out[:])
}

// Decode 把十六进制串还原成主键；格式不对、校验不过或 id 为 0 时返回 false。
func Decode(s string) (uint64, bool) {
	if len(s) != EncodedLen {
		return 0, false
	}
	raw, err := hex.DecodeString(s)
	if err != nil {
		return 0, false
	}
	var plain [16]byte
	(*block.Load()).Decrypt(plain[:], raw)
	if !bytes.Equal(plain[8:], tag[:]) {
		return 0, false
	}
	id := binary.BigEndian.Uint64(plain[:8])
	return id, id > 0
}

// ID 是对外暴露的主键：JSON 里是十六进制串，Go 里仍是 uint64。
type ID uint64

// MarshalJSON 输出十六进制串；0 表示没有值，输出 null。
func (i ID) MarshalJSON() ([]byte, error) {
	if i == 0 {
		return []byte("null"), nil
	}
	return json.Marshal(Encode(uint64(i)))
}

// UnmarshalJSON 只接受十六进制串（或 null / 空串，表示没有值），拒绝数字，避免绕过编码直接猜主键。
func (i *ID) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		*i = 0
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return errors.New("id 格式错误")
	}
	if s == "" {
		*i = 0
		return nil
	}
	id, ok := Decode(s)
	if !ok {
		return errors.New("id 格式错误")
	}
	*i = ID(id)
	return nil
}
