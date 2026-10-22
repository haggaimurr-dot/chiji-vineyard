package main

import (
	"context"
	"encoding/base32"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTOTPStandardVectorsAndReplay(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	for _, tc := range []struct {
		timestamp int64
		code      string
	}{{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"}, {1234567890, "005924"}, {2000000000, "279037"}, {20000000000, "353130"}} {
		if actual := totpCode(secret, tc.timestamp/30); actual != tc.code {
			t.Fatalf("time %d: %s != %s", tc.timestamp, actual, tc.code)
		}
		step, ok := matchTOTP(secret, tc.code, tc.timestamp, -1)
		if !ok {
			t.Fatal("valid code rejected")
		}
		if _, ok = matchTOTP(secret, tc.code, tc.timestamp, step); ok {
			t.Fatal("replayed code accepted")
		}
	}
	if _, ok := matchTOTP(secret, "", 59, -1); ok {
		t.Fatal("empty code accepted")
	}
	if _, ok := matchTOTP(secret, "287082", 150, -1); ok {
		t.Fatal("expired code accepted")
	}
}
func TestMFAEncryptionBindsAccountAndRejectsTampering(t *testing.T) {
	t.Setenv("TOKEN_SECRET", strings.Repeat("secret", 10))
	cipher, err := sealMFA("SECRET", "alice", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cipher, "SECRET") {
		t.Fatal("plaintext leaked")
	}
	if plain, err := sealMFA(cipher, "alice", true); err != nil || plain != "SECRET" {
		t.Fatal(plain, err)
	}
	if _, err := sealMFA(cipher, "bob", true); err == nil {
		t.Fatal("account substitution accepted")
	}
	if _, err := sealMFA(cipher[:5], "alice", true); err == nil {
		t.Fatal("truncation accepted")
	}
}
func personalTestActor(t *testing.T) (*Server, *accountFixture, Claims) {
	s, state, _ := newAccountServer(t)
	a := state.accounts["operator"]
	a.PasswordHash, _ = hashPassword("original-password")
	state.accounts[a.ID] = a
	return s, state, Claims{Subject: a.ID, Username: a.Username, Role: a.Role, Credential: adminCredential(a.PasswordHash)}
}
func securityCall(s *Server, actor Claims, action string, data M) (any, error) {
	return s.personalSecurity(httptest.NewRequest("POST", "/api", nil), actor, Request{Action: action, Data: json.RawMessage(jsonValue(data))})
}
func TestPersonalPasswordValidationRevocationAndAuditRollback(t *testing.T) {
	for _, scenario := range []string{"wrong-password", "same-password", "short-password", "audit-failure", "success"} {
		t.Run(scenario, func(t *testing.T) {
			s, state, actor := personalTestActor(t)
			old := state.accounts[actor.Subject]
			data := M{"currentPassword": "original-password", "password": "replacement-password", "accountId": "super"}
			switch scenario {
			case "wrong-password":
				data["currentPassword"] = "incorrect"
			case "same-password":
				data["password"] = "original-password"
			case "short-password":
				data["password"] = "short"
			case "audit-failure":
				state.failAudit = true
			}
			result, err := securityCall(s, actor, "admin.account.changePassword", data)
			if scenario != "success" {
				if err == nil || state.accounts[actor.Subject].PasswordHash != old.PasswordHash || len(state.audits) != 0 {
					t.Fatal("rejected change escaped rollback", err)
				}
				return
			}
			if err != nil || result.(M)["sessionRevoked"] != true {
				t.Fatal(result, err)
			}
			if !verifyPassword("replacement-password", state.accounts[actor.Subject].PasswordHash) || state.accounts["super"].PasswordHash != "seed-super-hash" {
				t.Fatal("changed wrong account")
			}
			token, _ := adminToken(old)
			r := httptest.NewRequest("POST", "/api", nil)
			r.Header.Set("Authorization", token)
			if _, err = s.store.requireAdmin(r); err == nil {
				t.Fatal("old session survived")
			}
			if strings.Contains(strings.Join(state.audits, ""), "replacement-password") {
				t.Fatal("password leaked in audit")
			}
		})
	}
}
func TestMFASetupEnableChallengeRecoveryAndDisable(t *testing.T) {
	s, state, actor := personalTestActor(t)
	data := M{"currentPassword": "original-password"}
	result, err := securityCall(s, actor, "admin.security.setup", data)
	if err != nil {
		t.Fatal(err)
	}
	secret := result.(M)["secret"].(string)
	if strings.Contains(state.configs["security:"+actor.Subject], secret) {
		t.Fatal("setup secret stored as plaintext")
	}
	data["code"] = totpCode(secret, time.Now().Unix()/30)
	result, err = securityCall(s, actor, "admin.security.enable", data)
	if err != nil {
		t.Fatal(err)
	}
	codes := result.(M)["recoveryCodes"].([]string)
	if len(codes) != 8 {
		t.Fatal(codes)
	}
	a := state.accounts[actor.Subject]
	actor.Credential = adminCredential(a.PasswordHash)
	response, apiErr := s.admin(httptest.NewRequest("POST", "/api", nil), Request{Action: "admin.login", Data: json.RawMessage(jsonValue(M{"username": a.Username, "password": "original-password"}))})
	if apiErr != nil || response.(M)["mfaRequired"] != true || response.(M)["token"] != nil {
		t.Fatal("login returned a token before second factor", response, apiErr)
	}
	required, err := s.loginSecondFactor(context.Background(), a, "")
	if err != nil || !required {
		t.Fatal("password alone bypassed MFA", required, err)
	}
	if _, err = s.loginSecondFactor(context.Background(), a, data["code"].(string)); err == nil {
		t.Fatal("enrollment code replayed")
	}
	for i := 0; i < 2; i++ {
		_, err = s.loginSecondFactor(context.Background(), a, codes[0])
		if (i == 0) != (err == nil) {
			t.Fatal("recovery reuse semantics", err)
		}
	}
	status, err := securityCall(s, actor, "admin.security.status", M{})
	if err != nil || status.(M)["recoveryRemaining"] != 7 {
		t.Fatal(status, err)
	}
	data["code"] = codes[1]
	data["password"] = "replacement-password"
	state.failAudit = true
	if _, err = securityCall(s, actor, "admin.account.changePassword", data); err == nil {
		t.Fatal("audit failure accepted")
	}
	state.failAudit = false
	if _, err = s.loginSecondFactor(context.Background(), a, codes[1]); err != nil {
		t.Fatal("rollback consumed recovery code", err)
	}
	data["code"] = codes[2]
	if _, err = securityCall(s, actor, "admin.security.disable", data); err != nil {
		t.Fatal(err)
	}
	if required, err = s.loginSecondFactor(context.Background(), state.accounts[a.ID], ""); err != nil || required {
		t.Fatal(required, err)
	}
	for _, code := range codes {
		if strings.Contains(state.configs["security:"+a.ID], code) || strings.Contains(strings.Join(state.audits, ""), code) {
			t.Fatal("recovery plaintext leaked")
		}
	}
}
func TestMFAExpiredSetupAndStaleSessionRejected(t *testing.T) {
	s, state, actor := personalTestActor(t)
	_, err := securityCall(s, actor, "admin.security.setup", M{"currentPassword": "original-password"})
	if err != nil {
		t.Fatal(err)
	}
	var security accountSecurity
	json.Unmarshal([]byte(state.configs["security:"+actor.Subject]), &security)
	security.PendingUntil = time.Now().Unix() - 1
	state.configs["security:"+actor.Subject] = jsonValue(security)
	if _, err = securityCall(s, actor, "admin.security.enable", M{"currentPassword": "original-password", "code": "123456"}); err == nil {
		t.Fatal("expired setup accepted")
	}
	actor.Credential = "obsolete"
	if _, err = securityCall(s, actor, "admin.security.status", M{}); err == nil {
		t.Fatal("stale session accepted")
	}
}
