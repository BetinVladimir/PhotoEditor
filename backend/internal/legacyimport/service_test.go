package legacyimport

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
	"testing"
)

func TestNormalizeReconcile(t *testing.T) {
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	request, body, err := normalize(ReconcileRequest{PlanHash: hash, SourceRelative: "runs/one/out.jpg", SourceSHA256: hash, UploadID: uuid.New(), Provenance: json.RawMessage(`{"run_id":"one"}`)})
	if err != nil || request.SourceRelative != "runs/one/out.jpg" || string(body) != `{"run_id":"one"}` {
		t.Fatal(request, string(body), err)
	}
}

func TestNormalizeReconcileRejectsUnsafeIdentity(t *testing.T) {
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, request := range []ReconcileRequest{
		{PlanHash: "bad", SourceRelative: "a.jpg", SourceSHA256: hash, UploadID: uuid.New()},
		{PlanHash: hash, SourceRelative: "../a.jpg", SourceSHA256: hash, UploadID: uuid.New()},
		{PlanHash: hash, SourceRelative: "..", SourceSHA256: hash, UploadID: uuid.New()},
		{PlanHash: hash, SourceRelative: "a.jpg", SourceSHA256: hash, UploadID: uuid.Nil},
		{PlanHash: hash, SourceRelative: "a.jpg", SourceSHA256: hash, UploadID: uuid.New(), Provenance: json.RawMessage(`[]`)},
	} {
		if _, _, err := normalize(request); domain.AsError(err).Code != "VALIDATION_FAILED" {
			t.Fatal("accepted invalid request", request, err)
		}
	}
}
