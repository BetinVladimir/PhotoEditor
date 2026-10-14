package review

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestOriginalDefectDiameterPreservesRangeAndRejectsChangedSource(t *testing.T) {
	fixture, err := os.ReadFile("testdata/proposal-report-multiple-faces-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var report proposalReportDocument
	if err := decodeExact(fixture, &report); err != nil {
		t.Fatal(err)
	}
	report.Params.Diameter = []float64{.003, .008}
	raw, err := canonical.Marshal(report.Params)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	ref := ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(digest[:])}
	source := DefectSource{Image: report.Params.Image, Canvas: report.Protection.Canvas, TransformChain: report.Protection.TransformChain}
	diameter, err := DefectProposalDiameterFromParams(ref, raw, source)
	if err != nil || diameter != ([2]float64{.003, .008}) {
		t.Fatal("original custom range lost", err)
	}
	policy, err := DefectProposalInputPolicyFromParams(ref, raw, source)
	if err != nil || policy.Diameter != diameter || len(policy.Faces) != len(report.Params.Faces) {
		t.Fatal("original face/mask policy lost", err)
	}
	before, err := canonical.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Permanent != nil {
		policy.Permanent.Artifact.ID = uuid.New()
	}
	if len(policy.Faces) > 0 {
		policy.Faces[0].Landmarks.ID = uuid.New()
	}
	if len(policy.Protections) > 0 {
		policy.Protections[0].Artifact.ID = uuid.New()
	}
	owned, err := DefectProposalInputPolicyFromParams(ref, raw, source)
	if err != nil {
		t.Fatal(err)
	}
	after, err := canonical.Marshal(owned)
	if err != nil || string(before) != string(after) {
		t.Fatal("mutating selected policy changed original declarations")
	}
	for _, field := range []string{"sha", "image", "canvas", "bytes", "bounds", "unknown"} {
		pin, body, image := ref, raw, source
		switch field {
		case "sha":
			pin.SHA256 = ""
		case "image":
			image.Image.ID = uuid.New()
		case "canvas":
			image.Canvas.Width++
		case "bytes":
			body = append(append([]byte(nil), raw...), byte(' '))
		case "bounds":
			params := report.Params
			params.Diameter = []float64{.008, .003}
			body, err = canonical.Marshal(params)
		case "unknown":
			body = append([]byte(`{"unknown":true,`), raw[1:]...)
		}
		if err != nil {
			t.Fatal(err)
		}
		if field == "bounds" || field == "unknown" {
			sha := sha256.Sum256(body)
			pin.SHA256 = hex.EncodeToString(sha[:])
		}
		if got, err := DefectProposalDiameterFromParams(pin, body, image); err == nil || got != ([2]float64{}) {
			t.Fatal("changed original Params accepted", field)
		}
	}
}
