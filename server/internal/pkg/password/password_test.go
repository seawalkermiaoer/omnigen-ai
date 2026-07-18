package password_test

import (
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
}

func TestVerify_FalseOnGarbageHash(t *testing.T) {
	assert.False(t, password.Verify("not-a-bcrypt-hash", "anything"))
}
