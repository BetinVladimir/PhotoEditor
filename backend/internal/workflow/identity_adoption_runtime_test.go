package workflow

import "testing"

func TestIdentityAdoptionEnvironmentFailsClosed(t *testing.T) {
	for _, mode := range []string{"", "false", "true", "invalid"} {
		enabled, err := IdentityAdoptionEnabled(func(key string) string {
			if key != "PHOTO_V23_IDENTITY_ADOPTION_ENABLED" {
				t.Fatal("unexpected environment key")
			}
			return mode
		})
		if enabled != (mode == "true") || (err != nil) != (mode == "invalid") {
			t.Fatalf("mode %q: %v %v", mode, enabled, err)
		}
	}
}
