package auth

import (
	"testing"

	"tokhub/internal/store"
)

func TestRequiresEmailVerificationForLoginFollowsSitePolicy(t *testing.T) {
	user := store.User{Role: "user", EmailVerified: false}
	if requiresEmailVerificationForLogin(user, false) {
		t.Fatal("email verification disabled should allow an unverified normal user to sign in")
	}
	if !requiresEmailVerificationForLogin(user, true) {
		t.Fatal("email verification enabled should block an unverified normal user")
	}
}

func TestRequiresEmailVerificationForLoginOnlyAppliesToNormalUsers(t *testing.T) {
	admin := store.User{Role: "admin", EmailVerified: false}
	if requiresEmailVerificationForLogin(admin, true) {
		t.Fatal("admin users should not be blocked by the normal-user email verification gate")
	}

	verified := store.User{Role: "user", EmailVerified: true}
	if requiresEmailVerificationForLogin(verified, true) {
		t.Fatal("verified normal users should not be blocked")
	}
}
