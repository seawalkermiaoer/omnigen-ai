// Package password 封装 bcrypt 哈希与校验。
package password

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const MaxLength = 72 // bcrypt 硬上限

func Hash(plain string) (string, error) {
	if len(plain) > MaxLength {
		return "", fmt.Errorf("密码长度超过 %d 字节上限", MaxLength)
	}
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("生成密码哈希失败: %w", err)
	}
	return string(b), nil
}

// Verify 在哈希损坏或密码不匹配时统一返回 false，不区分原因。
func Verify(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
