package v2sessions

import "testing"

// Reuse the complete scene plan/correction/recipe fixture and real parent commit.
func TestIntegrationAnalysisApplyBilling(t *testing.T) {
	t.Setenv("PHOTO_BILLING_ANALYSIS_INTEGRATION", "1")
	for _, blocked := range []string{"", "parent", "event_cursor"} {
		t.Run("billing_"+blocked, func(t *testing.T) { testIntegrationAnalysisApply(t, blocked) })
	}
}
