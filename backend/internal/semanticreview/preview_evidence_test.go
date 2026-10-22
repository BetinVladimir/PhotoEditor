package semanticreview

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestPreviewEvidenceBindsExactRequestAndDistinctOutputs(t *testing.T) {
	params := previewParamsFixture()
	for _, mutation := range []string{"valid", "params", "pixels_alias", "evidence_alias", "protection", "error", "algorithm", "hash"} {
		evidence := ExposurePreviewEvidence{SchemaVersion: 1, Params: params, WorkingImage: v2input.Ref{ArtifactID: uuid.New(), SHA256: params.Plan.SHA256}, Pixels: v2input.Ref{ArtifactID: uuid.New(), SHA256: params.Plan.SHA256}, Algorithm: "local_exposure_exact_replay_v1", ProtectedPixelsUnchanged: true}
		ref := v2input.Ref{ArtifactID: uuid.New()}
		switch mutation {
		case "params":
			evidence.Params.TargetRevision = "2"
		case "pixels_alias":
			evidence.Pixels = evidence.WorkingImage
		case "evidence_alias":
			ref.ArtifactID = params.Candidate.ArtifactID
		case "protection":
			evidence.ProtectedPixelsUnchanged = false
		case "error":
			evidence.OutsideMaskMaxError = 0.01
		case "algorithm":
			evidence.Algorithm = "unverified"
		}
		raw, err := canonical.Marshal(evidence)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		ref.SHA256 = hex.EncodeToString(sum[:])
		if mutation == "hash" {
			raw = append(raw, ' ')
		}
		_, err = DecodeExposurePreviewEvidence(ref, raw, params)
		if (err == nil) != (mutation == "valid") {
			t.Fatal(mutation, err)
		}
	}
}
