package v2input

import (
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestCaptureHistoryRequiresExactFrozenDigestOrder(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	seed := CaptureSelectionSeed{1, uuid.New(), "1", "1", nil, []string{a, b}}
	version := uuid.New()
	rows := []CaptureHistoryVersion{{uuid.New(), version, a}, {uuid.New(), version, b}}
	raw, _ := json.Marshal(rows)
	if history, err := CompileCaptureHistory(raw, seed); err != nil || len(history) != 2 {
		t.Fatalf("history=%v error=%v", history, err)
	}
	rows[0], rows[1] = rows[1], rows[0]
	raw, _ = json.Marshal(rows)
	if _, err := CompileCaptureHistory(raw, seed); err == nil {
		t.Fatal("reordered digests accepted")
	}
	rows[0], rows[1] = rows[1], rows[0]
	rows[1].ReceiptID = rows[0].ReceiptID
	raw, _ = json.Marshal(rows)
	if _, err := CompileCaptureHistory(raw, seed); err == nil {
		t.Fatal("duplicate receipt accepted")
	}
	if _, err := CompileCaptureHistory([]byte(`[]`), seed); err == nil {
		t.Fatal("missing history accepted")
	}
	seed.RecentContentSHAs = []string{}
	if rows, err := CompileCaptureHistory([]byte(`[]`), seed); err != nil || rows == nil {
		t.Fatal("empty history rejected", err)
	}
}

func TestCaptureHistoryRejectsDuplicateAndMissingKeys(t *testing.T) {
	sha := strings.Repeat("a", 64)
	seed := CaptureSelectionSeed{1, uuid.New(), "1", "1", nil, []string{sha}}
	row := CaptureHistoryVersion{uuid.New(), uuid.New(), sha}
	raw, _ := json.Marshal([]CaptureHistoryVersion{row})
	for _, bad := range []string{
		strings.Replace(string(raw), `"content_sha":"`+sha+`"`, `"content_sha":"`+sha+`","content_sha":"`+sha+`"`, 1),
		strings.Replace(string(raw), `"receipt_id":`, `"receipt_id":"`+uuid.NewString()+`","receipt_id":`, 1),
		strings.Replace(string(raw), `"content_sha":"`+sha+`"`, `"content_sha":"`+sha+`","unexpected":null`, 1),
	} {
		if _, err := CompileCaptureHistory([]byte(bad), seed); err == nil {
			t.Fatal("ambiguous history accepted")
		}
	}
}
