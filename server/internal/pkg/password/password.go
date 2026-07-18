// Package password 封装 bcrypt 哈希与校验。
package password

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const MaxLength = 72 // bcrypt 硬上限，按字节而非字符计算

// ErrTooLong 表示密码超过 bcrypt 的 72 字节硬上限。
// 注意是字节不是字符：一个中文字符占 3 字节，24 个中文字符就会触顶。
var ErrTooLong = errors.New("密码超过 72 字节上限")

func Hash(plain string) (string, error) {
	if len(plain) > MaxLength {
		return "", ErrTooLong
	}
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("生成密码哈希失败: %w", err)
	}
	return string(b), nil
}

// Verify 在哈希损坏或密码不匹配时统一返回 false，不区分原因。
func Verify(hash, plain string) bool {
	// bcrypt 的 CompareHashAndPassword 不做长度检查，会静默只比较前 72 字节，
	// 导致 正确密码+任意后缀 也能通过。Hash 已拒绝超长输入，
	// 因此这里超长一律判否。
	if len(plain) > MaxLength {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
