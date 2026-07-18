package crypto_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/pkg/crypto"
)

// testKey 是合法的 32 字节密钥（base64 编码），供测试使用。
// 通过 setKey 写入 APP_ENCRYPTION_KEY。
const testKey = "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=" // base64("01234567890123456789012345678901"), 32 raw bytes

func setKey(t *testing.T) {
	t.Helper()
	t.Setenv(crypto.KeyEnvVar, testKey)
}

func TestEncryptDecrypt_RoundTrips(t *testing.T) {
	setKey(t)

	ciphertext, err := crypto.Encrypt("sk-super-secret-value", "dashscope_api_key")
	require.NoError(t, err)
	require.NotEmpty(t, ciphertext)

	plaintext, err := crypto.Decrypt(ciphertext, "dashscope_api_key")
	require.NoError(t, err)
	assert.Equal(t, "sk-super-secret-value", plaintext)
}

func TestEncrypt_EmptyPlaintextRoundTrips(t *testing.T) {
	setKey(t)

	ciphertext, err := crypto.Encrypt("", "dashscope_api_key")
	require.NoError(t, err)

	plaintext, err := crypto.Decrypt(ciphertext, "dashscope_api_key")
	require.NoError(t, err)
	assert.Equal(t, "", plaintext)
}

// 密文必须是三段 base64 用 ":" 拼接。
func TestEncrypt_ProducesThreeColonSeparatedSegments(t *testing.T) {
	setKey(t)

	ciphertext, err := crypto.Encrypt("hello", "some_key")
	require.NoError(t, err)

	parts := strings.Split(ciphertext, ":")
	require.Len(t, parts, 3)
	for _, p := range parts {
		_, err := base64.StdEncoding.DecodeString(p)
		assert.NoError(t, err, "每段都必须是合法 base64: %q", p)
	}
}

// 同一明文两次加密必须产出不同密文：IV 每次都要重新生成，
// 否则相同明文会暴露相同密文，泄露"两处配置用的是同一个 key"这类信息。
func TestEncrypt_SamePlaintextProducesDifferentCiphertext(t *testing.T) {
	setKey(t)

	a, err := crypto.Encrypt("same-secret", "k")
	require.NoError(t, err)
	b, err := crypto.Encrypt("same-secret", "k")
	require.NoError(t, err)

	assert.NotEqual(t, a, b)

	// 但两者都必须能正确解密回同一明文。
	pa, err := crypto.Decrypt(a, "k")
	require.NoError(t, err)
	pb, err := crypto.Decrypt(b, "k")
	require.NoError(t, err)
	assert.Equal(t, "same-secret", pa)
	assert.Equal(t, "same-secret", pb)
}

// AAD 是本任务的核心要求：密文必须绑定到它所属的行（setting key）。
// 把 dashscope 的密文拿去用 t8star 的 AAD 解密，必须失败——
// 否则把一行密文拷到另一行，解密会"成功"并返回一个有效但错误的凭证。
func TestDecrypt_FailsOnAADMismatch(t *testing.T) {
	setKey(t)

	ciphertext, err := crypto.Encrypt("sk-dashscope-value", "dashscope_api_key")
	require.NoError(t, err)

	_, err = crypto.Decrypt(ciphertext, "t8star_api_key")
	require.Error(t, err)

	// 正确的 AAD 必须仍然可用，证明失败确实是因为 AAD 不匹配，
	// 不是密文本身已经损坏。
	plaintext, err := crypto.Decrypt(ciphertext, "dashscope_api_key")
	require.NoError(t, err)
	assert.Equal(t, "sk-dashscope-value", plaintext)
}

func TestDecrypt_FailsOnEmptyAADWhenEncryptedWithNonEmptyAAD(t *testing.T) {
	setKey(t)

	ciphertext, err := crypto.Encrypt("value", "some_key")
	require.NoError(t, err)

	_, err = crypto.Decrypt(ciphertext, "")
	require.Error(t, err)
}

// 篡改三段中的任意一段都必须让 Decrypt 失败，不能 panic。
func TestDecrypt_FailsOnTamperedSegments(t *testing.T) {
	setKey(t)

	ciphertext, err := crypto.Encrypt("sk-tamper-me", "k")
	require.NoError(t, err)
	parts := strings.Split(ciphertext, ":")
	require.Len(t, parts, 3)

	flip := func(b64 string) string {
		raw, err := base64.StdEncoding.DecodeString(b64)
		require.NoError(t, err)
		require.NotEmpty(t, raw)
		tampered := append([]byte{}, raw...)
		tampered[0] ^= 0xFF
		return base64.StdEncoding.EncodeToString(tampered)
	}

	cases := map[string]string{
		"iv":         strings.Join([]string{flip(parts[0]), parts[1], parts[2]}, ":"),
		"tag":        strings.Join([]string{parts[0], flip(parts[1]), parts[2]}, ":"),
		"ciphertext": strings.Join([]string{parts[0], parts[1], flip(parts[2])}, ":"),
	}

	for name, tampered := range cases {
		t.Run(name, func(t *testing.T) {
			require.NotPanics(t, func() {
				_, err := crypto.Decrypt(tampered, "k")
				assert.Error(t, err)
			})
		})
	}
}

// 段数不是 3 必须是干净的格式错误，不能 panic。
func TestDecrypt_FailsCleanlyOnWrongSegmentCount(t *testing.T) {
	setKey(t)

	cases := []string{
		"",
		"onlyonesegment",
		"two:segments",
		"four:segments:here:extra",
	}

	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			require.NotPanics(t, func() {
				_, err := crypto.Decrypt(c, "k")
				assert.Error(t, err)
			})
		})
	}
}

// 任一段是非法 base64 必须报错，不能 panic。
func TestDecrypt_FailsCleanlyOnGarbageBase64(t *testing.T) {
	setKey(t)

	cases := map[string]string{
		"iv":         "not!base64:AAAA:AAAA",
		"tag":        "AAAA:not!base64:AAAA",
		"ciphertext": "AAAA:AAAA:not!base64",
		"all":        "!!!:!!!:!!!",
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			require.NotPanics(t, func() {
				_, err := crypto.Decrypt(c, "k")
				assert.Error(t, err)
			})
		})
	}
}

// 合法 base64 但长度错误的 IV（GCM 的 nonce）必须报错而非 panic——
// 标准库 cipher.gcm.Open 对长度不对的 nonce 是 panic，必须在调用前拦住。
func TestDecrypt_FailsCleanlyOnWrongLengthIV(t *testing.T) {
	setKey(t)

	shortIV := base64.StdEncoding.EncodeToString([]byte("short"))
	tampered := shortIV + ":AAAAAAAAAAAAAAAA:AAAAAAAAAAAAAAAA"

	require.NotPanics(t, func() {
		_, err := crypto.Decrypt(tampered, "k")
		assert.Error(t, err)
	})
}

func TestEncrypt_FailsWithoutKey(t *testing.T) {
	t.Setenv(crypto.KeyEnvVar, "")

	_, err := crypto.Encrypt("secret", "k")
	require.Error(t, err)
}

func TestEncrypt_FailsOnInvalidBase64Key(t *testing.T) {
	t.Setenv(crypto.KeyEnvVar, "not-valid-base64!!!")

	_, err := crypto.Encrypt("secret", "k")
	require.Error(t, err)
}

func TestEncrypt_FailsOnWrongLengthKey(t *testing.T) {
	// 合法 base64，但解码后只有 16 字节，不满足 AES-256 的 32 字节要求。
	shortKey := base64.StdEncoding.EncodeToString(make([]byte, 16))
	t.Setenv(crypto.KeyEnvVar, shortKey)

	_, err := crypto.Encrypt("secret", "k")
	require.Error(t, err)
}

func TestMask_HidesMiddleKeepsHeadAndTail(t *testing.T) {
	masked := crypto.Mask("sk-abcdefghijklmnopqrstuvwxyz")
	assert.Contains(t, masked, "...")
	assert.NotContains(t, masked, "ghijklmnopqrstuv", "中间部分不得出现在脱敏结果里")
	assert.True(t, strings.HasPrefix(masked, "sk-abc"))
	assert.True(t, strings.HasSuffix(masked, "wxyz"))
}

func TestMask_EmptyStringDoesNotPanic(t *testing.T) {
	require.NotPanics(t, func() {
		got := crypto.Mask("")
		assert.Equal(t, "", got)
	})
}

func TestMask_ShortStringDoesNotPanicOrLeak(t *testing.T) {
	require.NotPanics(t, func() {
		got := crypto.Mask("sk-1")
		assert.NotContains(t, got, "sk-1")
	})
}

func TestMask_DoesNotEqualInputForNonTrivialSecret(t *testing.T) {
	secret := "sk-abcdefghijklmnop"
	masked := crypto.Mask(secret)
	assert.NotEqual(t, secret, masked)
}

// 错误信息不得包含明文或密钥材料。
func TestErrors_DoNotLeakPlaintextOrKeyMaterial(t *testing.T) {
	setKey(t)

	const plaintext = "sk-super-duper-secret-plaintext-marker"
	ciphertext, err := crypto.Encrypt(plaintext, "k")
	require.NoError(t, err)

	_, err = crypto.Decrypt(ciphertext, "wrong-aad")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), plaintext)
	assert.NotContains(t, err.Error(), testKey)

	_, err = crypto.Decrypt("garbage", "k")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), plaintext)
	assert.NotContains(t, err.Error(), testKey)

	t.Setenv(crypto.KeyEnvVar, "")
	_, err = crypto.Encrypt(plaintext, "k")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), plaintext)
}
