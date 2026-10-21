package control

import "errors"

const accountPasswordHint = "密码至少 6 位，且须同时包含大写字母、小写字母和数字"

// validAccountPassword 只用于设置新密码。登录仍只核对已保存的哈希。
func validAccountPassword(password string) bool {
	if len(password) < 6 || len(password) > 72 {
		return false
	}
	var upper, lower, digit bool
	for _, r := range password {
		switch {
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= '0' && r <= '9':
			digit = true
		}
	}
	return upper && lower && digit
}

func accountPasswordError() error {
	return errors.New(accountPasswordHint)
}
