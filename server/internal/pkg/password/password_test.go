package password_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chenhao/omnigen-ai/server/internal/pkg/password"
)

func TestHash_ThenVerify(t *testing.T) {
	hash, err := password.Hash("correct-horse")
	require.NoError(t, err)

	assert.NotEqual(t, "correct-horse", hash, "哈希不得等于明文")
	assert.True(t, password.Verify(hash, "correct-horse"))
	assert.False(t, password.Verify(hash, "wrong-password"))
}

// bcrypt 每次加盐，同一明文两次哈希必然不同。
func TestHash_IsSalted(t *testing.T) {
	a, err := password.Hash("same-input")
	require.NoError(t, err)
	b, err := password.Hash("same-input")
	require.NoError(t, err)

	assert.NotEqual(t, a, b)
	assert.True(t, password.Verify(a, "same-input"))
	assert.True(t, password.Verify(b, "same-input"))
}

// bcrypt 硬上限 72 字节，超长必须报错而非静默截断——
// 静默截断会让 73 字符与 80 字符的密码等价。
func TestHash_RejectsOverLongInput(t *testing.T) {
	_, err := password.Hash(string(make([]byte, 73)))
	require.Error(t, err)
	assert.True(t, errors.Is(err, password.ErrTooLong))
}

// 长度必须按字节而非按字符（rune）计算：一个中文字符占 3 字节，
// 25 个中文字符是 75 字节，超过上限，但只有 25 个 rune。
// 若误用 utf8.RuneCountInString 之类的按字符计数，这里会漏判。
func TestHash_RejectsMultiByteOverLimit(t *testing.T) {
	plain := strings.Repeat("中", 25) // 25 runes, 75 bytes
	require.Len(t, plain, 75)

	_, err := password.Hash(plain)
	require.Error(t, err)
	assert.True(t, errors.Is(err, password.ErrTooLong))
}

// 边界：恰好 72 字节必须成功，不能因为差一而误拒。
func TestHash_AcceptsExactly72Bytes(t *testing.T) {
	plain := strings.Repeat("a", 72)
	_, err := password.Hash(plain)
	require.NoError(t, err)
}

func TestVerify_FalseOnGarbageHash(t *testing.T) {
	assert.False(t, password.Verify("not-a-bcrypt-hash", "anything"))
}

// bcrypt.CompareHashAndPassword 本身不做长度检查，会静默只比较前 72 字节。
// 用恰好 72 字节的密码复现：bcrypt 的 key schedule 只读取前 72 字节，
// 短密码会因循环补位混入 NUL 终止符而错位，不会误报通过；
// 只有原密码恰好 72 字节时，"密码+任意后缀" 的前 72 字节才与原密码完全一致，
// 从而让底层 CompareHashAndPassword 判定为匹配。Verify 必须显式拒绝超长输入。
func TestVerify_RejectsOverLongInput(t *testing.T) {
	valid := strings.Repeat("a", 72) // 恰好等于 MaxLength，才会触发 bcrypt 的截断问题
	hash, err := password.Hash(valid)
	require.NoError(t, err)

	tooLong := valid + strings.Repeat("x", 80)
	assert.False(t, password.Verify(hash, tooLong))
}
