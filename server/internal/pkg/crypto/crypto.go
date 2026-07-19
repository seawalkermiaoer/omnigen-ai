// Package crypto 提供 AES-256-GCM 对称加解密，用于把上游 API Key 等敏感配置
// 以密文形式存进 Postgres 的 app_settings 表，而不是像旧系统那样留在浏览器
// localStorage 里。
//
// 密钥由调用方显式传入（32 字节，来自 config.Config.AppEncryptionKey 解码后
// 的原始字节），这已经是具备完整熵的随机字节，直接拿来做 AES key，不经过
// PBKDF2/scrypt 之类的密钥派生——KDF 是为人类口令这种低熵输入设计的，用在
// 这里只会增加开销，不增加安全性。
//
// 这个包不再自己从环境变量读取密钥：整个后端的配置只有 config.yaml 一份
// 来源，crypto 包若在内部悄悄读 os.Getenv("APP_ENCRYPTION_KEY")，会让密钥
// 出现两条独立的、可能互相不一致的来源路径。密钥的校验（base64、长度）已
// 经在 config.Load 里做过一次，这里的 KeySize 检查是第二层防御——防止有人
// 绕过 config.Load 直接构造一个长度不对的 []byte 传进来（例如测试代码手滑），
// 不依赖调用方已经校验过这个前提。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// KeySize 是 AES-256 所需的原始密钥字节数。
const KeySize = 32

// ErrInvalidFormat 表示密文不是 "iv:tag:ciphertext" 三段 base64 的格式。
var ErrInvalidFormat = errors.New("crypto: 密文格式错误，应为 iv:tag:ciphertext 三段 base64")

// ErrDecryptFailed 表示解密失败。密钥错误、AAD 与加密时不一致、密文被篡改，
// 这三种情况统一报同一个错误、不做区分——区分会给调用方提供一个可用于旁路
// 探测"猜中了哪一部分"的信号，故意合并成一种失败。
var ErrDecryptFailed = errors.New("crypto: 解密失败")

// ErrInvalidKeySize 表示传入的密钥长度不是 KeySize（32）字节。
var ErrInvalidKeySize = errors.New("crypto: 密钥长度不是 32 字节")

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("%w: 实际 %d 字节", ErrInvalidKeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: 初始化 AES 失败: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: 初始化 GCM 失败: %w", err)
	}
	return gcm, nil
}

// Encrypt 用 AES-256-GCM 加密 plaintext，返回 "iv:tag:ciphertext" 三段 base64
// （标准 padding）用 ":" 拼接的字符串。key 必须是 KeySize（32）字节。
//
// aad（Additional Authenticated Data）不参与加密，但参与认证：解密时必须传入
// 完全相同的 aad，否则即便密文本身完整无损也会解密失败。调用方必须传入这份
// 密文所属的 setting key（如 "dashscope_api_key"），把密文绑定到它所属的
// 行——没有这一层绑定，把 dashscope 密钥的密文拷到 t8star 密钥那一行，
// 解密会"成功"并返回一个有效但完全错误的凭证，系统会带着这个错误凭证去打
// 错误的上游，而不是明确报错。
//
// 标准库 GCM 的 Seal 把 tag 附在 ciphertext 末尾一起返回，而不是分开给。
// 这里手动按 Overhead() 切开、分别存成两段，是为了让存储格式本身在
// iv/tag/ciphertext 的边界上自解释，不依赖"tag 固定 16 字节"这种硬编码假设
// 去反向切割存量数据。
func Encrypt(key []byte, plaintext, aad string) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}

	iv := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("crypto: 生成 IV 失败: %w", err)
	}

	sealed := gcm.Seal(nil, iv, []byte(plaintext), []byte(aad))
	tagSize := gcm.Overhead()
	ciphertext := sealed[:len(sealed)-tagSize]
	tag := sealed[len(sealed)-tagSize:]

	return strings.Join([]string{
		base64.StdEncoding.EncodeToString(iv),
		base64.StdEncoding.EncodeToString(tag),
		base64.StdEncoding.EncodeToString(ciphertext),
	}, ":"), nil
}

// Decrypt 解密 Encrypt 产出的密文。key 必须是加密时使用的同一把 KeySize
// （32）字节密钥，aad 必须与加密时传入的一致。
//
// 输入校验分三层，确保任何畸形输入都以干净的 error 返回，绝不 panic：
//  1. 段数必须恰好是 3；
//  2. 每一段必须是合法 base64；
//  3. IV 解码后的长度必须等于 GCM 的 NonceSize——标准库 cipher.gcm.Open
//     对长度不对的 nonce 是直接 panic，必须在调用前手动拦住。
func Decrypt(key []byte, ciphertext, aad string) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}

	parts := strings.Split(ciphertext, ":")
	if len(parts) != 3 {
		return "", ErrInvalidFormat
	}

	iv, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return "", ErrInvalidFormat
	}
	tag, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ErrInvalidFormat
	}
	ct, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return "", ErrInvalidFormat
	}

	if len(iv) != gcm.NonceSize() {
		return "", ErrDecryptFailed
	}

	sealed := make([]byte, 0, len(ct)+len(tag))
	sealed = append(sealed, ct...)
	sealed = append(sealed, tag...)

	plaintext, err := gcm.Open(nil, iv, sealed, []byte(aad))
	if err != nil {
		return "", ErrDecryptFailed
	}
	return string(plaintext), nil
}

// maskHeadLen、maskTailLen 控制 Mask 保留的首尾长度，例如
// "sk-abcdefghijklmnopqrstuvwxyz" -> "sk-abc...wxyz"。
const (
	maskHeadLen = 6
	maskTailLen = 4
)

// Mask 把密钥脱敏成"首部...尾部"形式用于展示（如设置页回显"已配置"的密钥），
// 不泄露中间内容。
//
// 当输入短到首尾会重叠或本身就很短（含空串）时，没有足够长度可以安全地露出
// 首尾——那样等于把整个密钥都亮出来——所以整体替换成等长的掩码字符。
func Mask(secret string) string {
	if len(secret) <= maskHeadLen+maskTailLen {
		return strings.Repeat("*", len(secret))
	}
	return secret[:maskHeadLen] + "..." + secret[len(secret)-maskTailLen:]
}
