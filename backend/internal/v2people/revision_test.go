package v2people

import "testing"

// These are authored contract cases, not execution evidence.
func TestConsentRevisionRequiresCanonicalSafePositiveDecimal(t *testing.T) {
 for _, raw := range []string{"1", "42", "9007199254740991"} {
  if n, err := revision(raw); err != nil || n < 1 { t.Fatalf("valid revision %q: %d %v", raw, n, err) }
 }
 for _, raw := range []string{"", "+1", "01", "0", "-0", "-1", " 1", "1 ", "1.0", "9007199254740992", "9223372036854775808"} {
  if _, err := revision(raw); err == nil { t.Fatalf("accepted noncanonical or unsafe revision %q", raw) }
 }
}
