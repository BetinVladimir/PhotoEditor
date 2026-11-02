package mail

import (
	"strings"
	"testing"
)

func TestRecentAuthMailNamesSensitivePurposeInEveryLocale(t *testing.T) {
	for _, locale := range []string{"en", "ru", "bg"} {
		t.Run(locale, func(t *testing.T) {
			deletion, err := RecentAuthMessage("fixture@example.invalid", "<fixture@example.invalid>", locale, "123456", "delete_profile")
			if err != nil {
				t.Fatal(err)
			}
			ownership, err := RecentAuthMessage("fixture@example.invalid", "<fixture@example.invalid>", locale, "123456", "resolve_ownership")
			if err != nil {
				t.Fatal(err)
			}
			login, err := OTPMessage("fixture@example.invalid", "<fixture@example.invalid>", locale, "123456")
			if err != nil {
				t.Fatal(err)
			}
			if deletion.Subject == ownership.Subject || deletion.Subject == login.Subject || !strings.Contains(deletion.Body, "123456") || !strings.Contains(deletion.Body, "5") {
				t.Fatal("sensitive purpose or lifetime missing")
			}
		})
	}
	for _, purpose := range []string{"", "login", "unknown"} {
		if _, err := RecentAuthMessage("fixture@example.invalid", "<fixture@example.invalid>", "en", "123456", purpose); err == nil {
			t.Fatal("unsupported purpose accepted")
		}
	}
}
