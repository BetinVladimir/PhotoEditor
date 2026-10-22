package interactive

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/idempotency"
)

func TestInteractiveReceiptBindsExactTicketAndResource(t *testing.T) {
	run, other := uuid.New(), uuid.New()
	body := json.RawMessage(`{"run_id":"`+run.String()+`","status":"queued"}`)
	accepted := idempotency.Response{Status: 202, Body: body, ResourceID: &run, Replayed: true}
	if err := validateReceipt(accepted, "", "key", false); err != nil { t.Fatal(err) }
	for _, bad := range []idempotency.Response{
		{Status: 201, Body: body, ResourceID: &run},
		{Status: 202, Body: body},
		{Status: 202, Body: body, ResourceID: &other},
		{Status: 202, Body: json.RawMessage(`{"run_id":"`+run.String()+`","status":"running"}`), ResourceID: &run},
		{Status: 202, Body: json.RawMessage(`{"run_id":"`+run.String()+`","status":"queued","extra":true}`), ResourceID: &run},
	} {
		if validateReceipt(bad, "", "key", false) == nil { t.Fatal("invalid immutable admission receipt accepted", bad) }
	}
}

func TestMaskReceiptRequiresOriginalSourceDigest(t *testing.T) {
	run := uuid.New()
	sha := strings.Repeat("a", 64)
	out := idempotency.Response{Status: 202, ResourceID: &run,
		Body: json.RawMessage(`{"run_id":"`+run.String()+`","status":"queued","source_sha256":"`+sha+`"}`)}
	if err := validateReceipt(out, sha, "key", false); err != nil { t.Fatal(err) }
	if validateReceipt(out, strings.Repeat("b", 64), "key", false) == nil { t.Fatal("cross-source mask receipt accepted") }
	if validateReceipt(out, "", "key", false) == nil { t.Fatal("mask response promoted to a generation ticket") }
}

func TestGenerationAbandonmentIsExactAndCannotBecomeAdmission(t *testing.T) {
	out := idempotency.Response{Status: 200, Body: json.RawMessage(`{"status":"abandoned","key":"original"}`)}
	if err := validateReceipt(out, "", "original", true); err != nil { t.Fatal(err) }
	if validateReceipt(out, "", "other", true) == nil { t.Fatal("foreign request tombstone accepted") }
	if validateReceipt(out, "", "original", false) == nil { t.Fatal("tombstone became accepted work") }
	run := uuid.New(); out.ResourceID = &run
	if validateReceipt(out, "", "original", true) == nil { t.Fatal("resource-bearing tombstone accepted") }
	out.ResourceID = nil
	out.Body = json.RawMessage(`{"status":"abandoned","key":"original","run_id":"`+run.String()+`"}`)
	if validateReceipt(out, "", "original", true) == nil { t.Fatal("ambiguous tombstone accepted") }
}
