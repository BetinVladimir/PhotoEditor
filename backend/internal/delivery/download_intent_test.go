package delivery

import (
	"encoding/json"
 "context"
 "errors"
 "fmt"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
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

func TestDownloadIntentRequestExactSelection(t *testing.T) {
	zero, last, negative, overflow := 0, 4999, -1, 5000
	preset, empty := uuid.New(), uuid.Nil
	for _, tc := range []struct {
		name    string
		request DownloadIntentRequest
		valid   bool
	}{
		{"manifest", DownloadIntentRequest{Kind: "manifest"}, true},
		{"first", DownloadIntentRequest{Kind: "slot", Ordinal: &zero, PresetID: &preset}, true},
		{"last", DownloadIntentRequest{Kind: "slot", Ordinal: &last, PresetID: &preset}, true},
		{"manifest-slot", DownloadIntentRequest{Kind: "manifest", Ordinal: &zero}, false},
		{"manifest-preset", DownloadIntentRequest{Kind: "manifest", PresetID: &preset}, false},
		{"missing-preset", DownloadIntentRequest{Kind: "slot", Ordinal: &zero}, false},
		{"missing-ordinal", DownloadIntentRequest{Kind: "slot", PresetID: &preset}, false},
		{"zero-preset", DownloadIntentRequest{Kind: "slot", Ordinal: &zero, PresetID: &empty}, false},
		{"negative", DownloadIntentRequest{Kind: "slot", Ordinal: &negative, PresetID: &preset}, false},
		{"overflow", DownloadIntentRequest{Kind: "slot", Ordinal: &overflow, PresetID: &preset}, false},
		{"unknown", DownloadIntentRequest{Kind: "all"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if (tc.request.Validate() == nil) != tc.valid {
				t.Fatal("selection validation mismatch")
			}
		})
	}
}

func TestDownloadIntentRequestClosedWireVariants(t *testing.T) {
	preset := "12345678-1234-4234-8234-123456789abc"
	for _, body := range []string{
		`{"kind":"manifest","ordinal":null}`,
		`{"kind":"manifest","preset_id":null}`,
		`{"kind":"slot","ordinal":null,"preset_id":"`+preset+`"}`,
		`{"kind":"slot","ordinal":0,"preset_id":null}`,
		`{"kind":"manifest","kind":"slot"}`,
		`{"kind":"manifest","unknown":true}`,
		`{"kind":"manifest"} {}`,
	} {
		original := DownloadIntentRequest{Kind: "manifest"}
		if json.Unmarshal([]byte(body), &original) == nil { t.Fatalf("accepted invalid wire request: %s", body) }
		if original.Kind != "manifest" || original.Ordinal != nil || original.PresetID != nil { t.Fatal("failed decode mutated receiver") }
	}
	for _, body := range []string{`{"kind":"manifest"}`, `{"kind":"slot","ordinal":0,"preset_id":"`+preset+`"}`, `{"kind":"slot","ordinal":4999,"preset_id":"`+preset+`"}`} {
		var request DownloadIntentRequest
		if err := json.Unmarshal([]byte(body), &request); err != nil { t.Fatal(body, err) }
		encoded, err := json.Marshal(request)
		if err != nil || string(encoded) != body { t.Fatal("wire shape changed", string(encoded), err) }
	}
}

func TestDownloadIntentReplayRejectsMalformedOrUnboundedReceipt(t *testing.T) {
 now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
 id := uuid.New()
 valid, err := json.Marshal(downloadIntentRecord{ID: id, ExpiresAt: now.Add(45*time.Second)})
 if err != nil { t.Fatal(err) }
 future, err := json.Marshal(downloadIntentRecord{ID: id, ExpiresAt: now.Add(61*time.Second)})
 if err != nil { t.Fatal(err) }
 extra := append(append([]byte(nil), valid[:len(valid)-1]...), []byte(`,"token":"should-not-be-stored"}`)...)
 trailing := append(append([]byte(nil), valid...), []byte(` {}`)...)
 for _, body := range [][]byte{nil, []byte(`null`), []byte(`{}`), extra, trailing, future, make([]byte, 513)} {
  if _, ok := validDownloadIntentReceipt(idempotency.Response{Status: 201, ResourceID: &id, Body: body}, now); ok { t.Fatal("malformed replay receipt accepted", string(body)) }
 }
 if _, ok := validDownloadIntentReceipt(idempotency.Response{Status: 201, ResourceID: &id, Body: valid}, now); !ok { t.Fatal("valid receipt rejected") }
}

// A receipt accepted before a slow transaction must be refused at response time.
func TestDownloadIntentReceiptExpiresWhileRequestIsInFlight(t *testing.T) {
	started := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	current := started
	service := DownloadIntentService{Now: func() time.Time { return current }}
	id := uuid.New()
	body, err := json.Marshal(downloadIntentRecord{ID: id, ExpiresAt: started.Add(45 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	receipt := idempotency.Response{Status: 201, ResourceID: &id, Body: body}
	if _, ok := validDownloadIntentReceipt(receipt, service.now()); !ok {
		t.Fatal("fresh receipt rejected")
	}
	current = started.Add(45 * time.Second)
	if _, ok := validDownloadIntentReceipt(receipt, service.now()); ok {
		t.Fatal("expired receipt accepted after transaction delay")
	}
}


func TestDownloadIntentLookupPreservesRetryableFailure(t *testing.T) {
 outage:=errors.New("database unavailable")
 for _,cause:=range []error{outage,context.Canceled,context.DeadlineExceeded} {
  if got:=downloadIntentLookupError(cause,"DOWNLOAD_REVOKED");got!=cause {t.Fatal("infrastructure failure became definitive revoke",got)}
 }
 for _,code:=range []string{"DOWNLOAD_REVOKED","DOWNLOAD_INTENT_INVALID"} {
  for _,missing:=range []error{pgx.ErrNoRows,fmt.Errorf("lookup: %w",pgx.ErrNoRows)} {
   err:=domain.AsError(downloadIntentLookupError(missing,code))
   if err.Code!=code {t.Fatal("missing authorized row did not fail closed",err)}
  }
 }
 if downloadIntentLookupError(nil,"DOWNLOAD_REVOKED")!=nil {t.Fatal("successful lookup rejected")}
}
