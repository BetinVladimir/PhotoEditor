package v2observation

import "testing"

func TestObservationRevisionAndConsentEpochCanonicalDecimal(t *testing.T) {
 for _, raw := range []string{"1", "9007199254740991"} {
  if _, err := positiveRevision(raw); err != nil { t.Fatalf("valid %q: %v", raw, err) }
 }
 for _, raw := range []string{"+1", "01", "0", "-0", " 1", "1 ", "9007199254740992", "9223372036854775808"} {
  if _, err := positiveRevision(raw); err == nil { t.Fatalf("accepted noncanonical revision/epoch %q", raw) }
 }
}
