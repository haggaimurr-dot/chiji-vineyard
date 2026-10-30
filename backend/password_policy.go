package main

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"time"
)

// 密码定期过期 + 全员强制二次验证（新增：账号安全）。
// PASSWORD_EXPIRY_DAYS：密码有效期天数，默认 90，0 表示不启用过期。
// MFA_ENFORCE：为 "0" 时关闭强制绑定，默认全员强制。

func passwordExpiryDays() int {
	if v := os.Getenv("PASSWORD_EXPIRY_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return 90
}

func mfaEnforced() bool {
	return os.Getenv("MFA_ENFORCE") != "0"
}

// passwordExpired 判断密码是否过期。changedAt 为空（历史数据未回填）视为未过期，
// 由迁移统一回填。
func passwordExpired(changedAt sql.NullTime, now time.Time) bool {
	days := passwordExpiryDays()
	if days <= 0 || !changedAt.Valid {
		return false
	}
	return now.Sub(changedAt.Time) > time.Duration(days)*24*time.Hour
}

// mfaBound 查询账号是否已绑定二次验证。
func mfaBound(ctx context.Context, tx *sql.Tx, adminID string) (bool, error) {
	state, err := readSecurity(ctx, tx, adminID)
	if err != nil {
		return false, err
	}
	return state.Enabled, nil
}
