package v2input

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCaptureSelectionRejectsCorruptScopeAndPinsHistory(t *testing.T) {
	session := uuid.New()
	seed := CaptureSelectionSeed{SchemaVersion: 1, SessionID: session, SessionRevision: "12", ConsentEpoch: "3", RecentContentSHAs: []string{strings.Repeat("a", 64)}}
	seed.ShotDocument = &CaptureShotDocument{uuid.New(), uuid.New(), strings.Repeat("b", 64)}
	raw, _ := json.Marshal(seed)
	parsed, err := CompileCaptureSelection(raw, session)
	if err != nil || parsed.SessionRevision != "12" || parsed.ShotDocument.DocumentID != seed.ShotDocument.DocumentID || parsed.RecentContentSHAs[0] != seed.RecentContentSHAs[0] {
		t.Fatalf("seed=%v error=%v", parsed, err)
	}
	if _, err = CompileCaptureSelection(raw, uuid.New()); err == nil {
		t.Fatal("foreign session accepted")
	}
	for _, modify := range []func(*CaptureSelectionSeed){
		func(s *CaptureSelectionSeed) { s.ConsentEpoch = "0" },
		func(s *CaptureSelectionSeed) { s.SessionRevision = "+12" },
		func(s *CaptureSelectionSeed) { s.SessionRevision = "012" },
		func(s *CaptureSelectionSeed) { s.RecentContentSHAs = nil },
		func(s *CaptureSelectionSeed) { s.RecentContentSHAs = []string{"broken"} },
		func(s *CaptureSelectionSeed) {
			s.ShotDocument = &CaptureShotDocument{uuid.Nil, uuid.New(), strings.Repeat("b", 64)}
		},
	} {
		bad := seed
		modify(&bad)
		body, _ := json.Marshal(bad)
		if _, err = CompileCaptureSelection(body, session); err == nil {
			t.Fatalf("invalid seed accepted: %s", body)
		}
	}
	seed.ShotDocument = nil
	raw, _ = json.Marshal(seed)
	if _, err = CompileCaptureSelection(raw, session); err != nil {
		t.Fatal("absent shot list must be valid", err)
	}
	var fields map[string]any
	json.Unmarshal(raw, &fields)
	delete(fields, "shot_document")
	raw, _ = json.Marshal(fields)
	if _, err = CompileCaptureSelection(raw, session); err == nil {
		t.Fatal("missing nullable field accepted")
	}
}

func TestCaptureSelectionRejectsDuplicateNestedDocumentKeys(t *testing.T) {
	session := uuid.New()
	seed := CaptureSelectionSeed{1, session, "1", "1", &CaptureShotDocument{uuid.New(), uuid.New(), strings.Repeat("a", 64)}, []string{}}
	raw, _ := json.Marshal(seed)
	bad := strings.Replace(string(raw), `"sha256":`, `"sha256":"`+strings.Repeat("b", 64)+`","sha256":`, 1)
	if _, err := CompileCaptureSelection([]byte(bad), session); err == nil {
		t.Fatal("duplicate nested SHA accepted")
	}
}
