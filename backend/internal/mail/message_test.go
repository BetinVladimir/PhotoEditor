package mail

import (
	"strings"
	"testing"
)

func TestOTPMessage_invalidCode(t *testing.T) {
	for _, bad := range []string{"", "12345", "1234567", "abcdef", "12 345"} {
		if _, err := OTPMessage("a@example.test", "<id>", "en", bad); err == nil {
			t.Errorf("invalid OTP %q was accepted", bad)
		}
	}
}

func TestOTPMessage_invalidLocale(t *testing.T) {
	if _, err := OTPMessage("a@example.test", "<id>", "fr", "123456"); err == nil {
		t.Fatal("invalid locale must return error")
	}
}

func TestOTPMessage_locales(t *testing.T) {
	for _, locale := range []string{"en", "ru", "bg"} {
		m, err := OTPMessage("a@example.test", "<id>", locale, "123456")
		if err != nil || !strings.Contains(m.Body, "123456") {
			t.Errorf("OTPMessage locale %q failed: %v", locale, err)
		}
	}
}

func TestInviteMessage_emptyToken(t *testing.T) {
	if _, err := InviteMessage("a@example.test", "<id>", "en", "Tenant", ""); err == nil {
		t.Fatal("empty token must return error")
	}
}

func TestInviteMessage_emptyTenant(t *testing.T) {
	if _, err := InviteMessage("a@example.test", "<id>", "en", "", "token"); err == nil {
		t.Fatal("empty tenant must return error")
	}
}

func TestInviteMessage_invalidLocale(t *testing.T) {
	if _, err := InviteMessage("a@example.test", "<id>", "fr", "Tenant", "token"); err == nil {
		t.Fatal("invalid locale must return error")
	}
}

func TestInviteMessage_locales(t *testing.T) {
	for _, locale := range []string{"en", "ru", "bg"} {
		m, err := InviteMessage("a@example.test", "<id>", locale, "Studio", "tok123")
		if err != nil || !strings.Contains(m.Body, "tok123") {
			t.Errorf("InviteMessage locale %q failed: %v", locale, err)
		}
	}
}
