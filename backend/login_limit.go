package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
)

type loginLimitError struct{ RetryAfter int }

func (e *loginLimitError) Error() string {
	return fmt.Sprintf("登录尝试过于频繁，请 %d 秒后重试", e.RetryAfter)
}

// Forwarded headers are used only behind explicitly configured trusted proxies.
func loginPeer(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil {
		trusted := func(address net.IP) bool {
			for _, value := range strings.Split(os.Getenv("LOGIN_TRUSTED_PROXY_CIDRS"), ",") {
				_, network, e := net.ParseCIDR(strings.TrimSpace(value))
				if e == nil && network.Contains(address) {
					return true
				}
			}
			return false
		}
		if trusted(ip) {
			hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
			for i := len(hops) - 1; i >= 0; i-- {
				candidate := net.ParseIP(strings.TrimSpace(hops[i]))
				if candidate == nil {
					return ip.String()
				}
				if !trusted(candidate) {
					return candidate.String()
				}
			}
		}
		return ip.String()
	}
	return "unknown"
}

func limitKey(scope, value string) string {
	hash := sha256.Sum256([]byte(scope + "\x00" + value))
	return hex.EncodeToString(hash[:])
}

// Atomic MySQL counters are shared by all instances and survive restarts.
// Count all attempts, including successful logins, before password derivation.
func (s *Store) limitLogin(r *http.Request, scope, value string, maximum, seconds int) error {
	key := limitKey(scope, value)
	_, err := s.db.ExecContext(r.Context(), `INSERT INTO login_limits(bucket_key,attempts,expires_at) VALUES(?,1,TIMESTAMPADD(SECOND,?,NOW(3)))
 ON DUPLICATE KEY UPDATE attempts=IF(expires_at<=NOW(3),1,LEAST(attempts+1,?)),expires_at=IF(expires_at<=NOW(3),TIMESTAMPADD(SECOND,?,NOW(3)),expires_at)`, key, seconds, maximum+1, seconds)
	if err != nil {
		return err
	}
	var attempts int
	var retry int
	if err = s.db.QueryRowContext(r.Context(), "SELECT attempts,GREATEST(1,CEIL(TIMESTAMPDIFF(MICROSECOND,NOW(3),expires_at)/1000000)) FROM login_limits WHERE bucket_key=?", key).Scan(&attempts, &retry); err != nil {
		return err
	}
	if attempts > maximum {
		return &loginLimitError{RetryAfter: retry}
	}
	// Bounded cleanup avoids accumulating expired account/source hashes.
	_, err = s.db.ExecContext(r.Context(), "DELETE FROM login_limits WHERE expires_at<=NOW(3) LIMIT 100")
	return err
}

func (s *Server) checkLoginLimit(r *http.Request, req Request) error {
	switch req.Action {
	case "admin.login", "admin.bootstrap":
		if err := s.store.limitLogin(r, "admin-source", loginPeer(r), 30, 900); err != nil {
			return err
		}
		if req.Action == "admin.login" {
			var d struct{ Username string }
			if err := decode(req.Data, &d); err != nil {
				return err
			}
			return s.store.limitLogin(r, "admin-account", strings.ToLower(clean(d.Username, 40)), 5, 900)
		}
	case "auth.login":
		return s.store.limitLogin(r, "mini-source", loginPeer(r), 30, 60)
	}
	return nil
}

func writeAccessError(w http.ResponseWriter, err error) bool {
	var limit *loginLimitError
	if errors.As(err, &limit) {
		w.Header().Set("Retry-After", fmt.Sprint(limit.RetryAfter))
		writeJSON(w, http.StatusTooManyRequests, M{"ok": false, "error": APIError{Code: "LOGIN_RATE_LIMITED", Message: limit.Error()}, "retryAfter": limit.RetryAfter})
		return true
	}
	if errors.Is(err, errPermission) {
		writeJSON(w, http.StatusForbidden, M{"ok": false, "error": APIError{Code: "PERMISSION_DENIED", Message: err.Error()}})
		return true
	}
	return false
}
