package v2capture

import (
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestReceiptSequenceAndDeviceBoundary(t *testing.T) {
	principal := DevicePrincipal{TenantID: uuid.New(), ProjectID: uuid.New(), DeviceID: uuid.New(), CredentialEpoch: 1}
	sessionID := uuid.New()
	request := ReceiptRequest{DeviceID: principal.DeviceID, UploadID: uuid.New(), ContentSHA: strings.Repeat("a", 64)}
	for _, sequence := range []string{"0", "1", "9223372036854775807"} {
		request.Sequence = sequence
		if _, err := validateReceipt(principal, sessionID, request); err != nil {
			t.Fatalf("valid sequence %s: %v", sequence, err)
		}
	}
	for _, sequence := range []string{"", "00", "01", "-1", "+1", "1e2", " 1", "9223372036854775808"} {
		request.Sequence = sequence
		if _, err := validateReceipt(principal, sessionID, request); err == nil {
			t.Fatalf("accepted sequence %q", sequence)
		}
	}
	request.Sequence = "1"
	request.DeviceID = uuid.New()
	if _, err := validateReceipt(principal, sessionID, request); err == nil {
		t.Fatal("accepted another device's receipt")
	}
	request.DeviceID = principal.DeviceID
	request.ContentSHA = strings.Repeat("A", 64)
	if _, err := validateReceipt(principal, sessionID, request); err == nil {
		t.Fatal("accepted noncanonical SHA")
	}
}
