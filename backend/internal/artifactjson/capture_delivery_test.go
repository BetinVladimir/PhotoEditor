package artifactjson

import (
	"testing"

	"github.com/google/uuid"
)

// v24 capture purposes ────────────────────────────────────────────────────────

func TestV24CapturePurposesAreAccepted(t *testing.T) {
	s := Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New()}
	for _, p := range []string{
		"v24_capture_prepare_params",
		"v24_capture_shots",
		"v24_capture_consents",
		"v24_capture_context",
		"v24_capture_assess_params",
	} {
		s.Purpose = p
		if _, err := DocumentKey(s, uuid.New()); err != nil {
			t.Errorf("v24 purpose %q rejected: %v", p, err)
		}
	}
}

func TestV24CaptureLargePurposesHave8MiBLimit(t *testing.T) {
	for _, p := range []string{"v24_capture_context", "v24_capture_consents"} {
		if got := maxDocumentBytes(p); got != 8*1048576 {
			t.Errorf("maxDocumentBytes(%q) = %d, want %d", p, got, 8*1048576)
		}
	}
}

func TestV24CaptureOtherPurposesHave1MiBLimit(t *testing.T) {
	for _, p := range []string{"v24_capture_prepare_params", "v24_capture_shots", "v24_capture_assess_params"} {
		if got := maxDocumentBytes(p); got != 1048576 {
			t.Errorf("maxDocumentBytes(%q) = %d, want %d", p, got, 1048576)
		}
	}
}

// v30 delivery maxDocumentBytes branches ─────────────────────────────────────

func TestMaxDocumentBytesV30DeliveryBranches(t *testing.T) {
	if got := maxDocumentBytes("v30_delivery_signed_closure"); got != 8*1048576 {
		t.Errorf("signed_closure = %d, want %d", got, 8*1048576)
	}
	if got := maxDocumentBytes("v30_delivery_signed_envelope"); got != 65536 {
		t.Errorf("signed_envelope = %d, want %d", got, 65536)
	}
}
