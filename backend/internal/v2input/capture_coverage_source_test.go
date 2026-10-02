package v2input

import (
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestCaptureCoverageEvidenceRequiresConfirmationAndFrozenVersion(t *testing.T) {
	current, version, receipt := uuid.New(), uuid.New(), uuid.New()
	quality := .8
	row := CaptureCoverageEvidence{uuid.New(), receipt, version, &quality, Ref{uuid.New(), strings.Repeat("a", 64)}, []CaptureCoverageIdentity{{uuid.New(), uuid.New(), "1"}}}
	history := []CaptureHistoryVersion{{receipt, version, strings.Repeat("b", 64)}}
	raw, _ := json.Marshal([]CaptureCoverageEvidence{row})
	frames, err := CompileCaptureCoverageEvidence(raw, current, history)
	if err != nil || len(frames) != 1 || *frames[0].Quality != quality || len(frames[0].PersonIDs) != 1 {
		t.Fatalf("frames=%v err=%v", frames, err)
	}
	if _, err = CompileCaptureCoverageEvidence(raw, version, history); err == nil {
		t.Fatal("current frame counted")
	}
	history[0].AssetVersionID = uuid.New()
	if _, err = CompileCaptureCoverageEvidence(raw, current, history); err == nil {
		t.Fatal("foreign version accepted")
	}
	history[0].AssetVersionID = version
	var packets []map[string]any
	json.Unmarshal(raw, &packets)
	delete(packets[0], "quality")
	raw, _ = json.Marshal(packets)
	if _, err = CompileCaptureCoverageEvidence(raw, current, history); err == nil {
		t.Fatal("missing quality confused with explicit unknown quality")
	}
	row.Quality = nil
	raw, _ = json.Marshal([]CaptureCoverageEvidence{row})
	if frames, err = CompileCaptureCoverageEvidence(raw, current, history); err != nil || frames[0].Quality != nil {
		t.Fatal("unknown quality replaced", err)
	}
	row.IdentityEvidence[0].Epoch = "0"
	raw, _ = json.Marshal([]CaptureCoverageEvidence{row})
	if _, err = CompileCaptureCoverageEvidence(raw, current, history); err == nil {
		t.Fatal("invalid consent epoch accepted")
	}
}

func TestCaptureCoverageRejectsAmbiguousNestedPackets(t *testing.T) {
	current, version, receipt := uuid.New(), uuid.New(), uuid.New()
	row := CaptureCoverageEvidence{uuid.New(), receipt, version, nil, Ref{uuid.New(), strings.Repeat("a", 64)}, []CaptureCoverageIdentity{{uuid.New(), uuid.New(), "1"}}}
	raw, _ := json.Marshal([]CaptureCoverageEvidence{row})
	history := []CaptureHistoryVersion{{receipt, version, strings.Repeat("b", 64)}}
	cases := []string{
		strings.Replace(string(raw), `"epoch":"1"`, `"epoch":"1","epoch":"2"`, 1),
		strings.Replace(string(raw), `"epoch":"1"`, `"epoch":"1","extra":true`, 1),
		strings.Replace(string(raw), `"sha256":"`+strings.Repeat("a", 64)+`"`, `"sha256":"`+strings.Repeat("a", 64)+`","sha256":"`+strings.Repeat("b", 64)+`"`, 1),
		strings.Replace(string(raw), `"quality":null`, `"quality":null,"quality":1`, 1),
	}
	for i, packet := range cases {
		if _, err := CompileCaptureCoverageEvidence([]byte(packet), current, history); err == nil {
			t.Fatalf("ambiguous packet %d accepted", i)
		}
	}
}

func TestCaptureCoverageRejectsReusedConsentAndInvalidHistoryDigest(t *testing.T) {
	current, version, receipt, consent := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	row := CaptureCoverageEvidence{uuid.New(), receipt, version, nil, Ref{uuid.New(), strings.Repeat("a", 64)}, []CaptureCoverageIdentity{{uuid.New(), consent, "1"}, {uuid.New(), consent, "1"}}}
	history := []CaptureHistoryVersion{{receipt, version, strings.Repeat("b", 64)}}
	raw, _ := json.Marshal([]CaptureCoverageEvidence{row})
	if _, err := CompileCaptureCoverageEvidence(raw, current, history); err == nil {
		t.Fatal("one grant reused for two people")
	}
	row.IdentityEvidence = row.IdentityEvidence[:1]
	raw, _ = json.Marshal([]CaptureCoverageEvidence{row})
	history[0].ContentSHA = "invalid"
	if _, err := CompileCaptureCoverageEvidence(raw, current, history); err == nil {
		t.Fatal("invalid history digest accepted")
	}
}
