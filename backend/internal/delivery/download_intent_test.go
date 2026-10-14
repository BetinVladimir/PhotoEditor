package delivery

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/idempotency"
)

func TestDownloadIntentReceiptBindsStatusIdentityAndExpiry(t *testing.T) {
	now := time.Now().UTC()
	id := uuid.New()
	body, err := json.Marshal(downloadIntentRecord{ID: id, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	valid := idempotency.Response{Status: 201, ResourceID: &id, Body: body, Replayed: true}
	if record, ok := validDownloadIntentReceipt(valid, now); !ok || record.ID != id {
		t.Fatal("valid replay receipt rejected")
	}
	wrongStatus := valid
	wrongStatus.Status = 200
	wrongID := valid
	other := uuid.New()
	wrongID.ResourceID = &other
	missingID := valid
	missingID.ResourceID = nil
	for _, receipt := range []idempotency.Response{wrongStatus, wrongID, missingID} {
		if _, ok := validDownloadIntentReceipt(receipt, now); ok {
			t.Fatal("unbound replay receipt accepted", receipt)
		}
	}
	if _, ok := validDownloadIntentReceipt(valid, now.Add(time.Minute)); ok {
		t.Fatal("expired bearer receipt accepted")
	}
}
