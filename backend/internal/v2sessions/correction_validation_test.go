package v2sessions

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func TestValidateCorrectionPlanAgainstFrozenGroup(t *testing.T) {
	const digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	bindings := map[int]v2input.SessionVersionBinding{
		3: {AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: digest}, CanvasHash: digest},
		4: {AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: digest}, CanvasHash: digest},
	}
	type item struct {
		Binding     v2input.SessionVersionBinding `json:"binding"`
		Ordinal     int                           `json:"ordinal"`
		Delta       v2draft.CorrectionVector      `json:"delta"`
		Disposition string                        `json:"disposition"`
		Reason      *string                       `json:"reason"`
	}
	type document struct {
		SchemaVersion  int    `json:"schema_version"`
		SnapshotDigest string `json:"snapshot_digest"`
		Items          []item `json:"items"`
	}
	group := sceneGroup{ID: uuid.New(), Ordinals: []int{3, 4}, ReferenceOrdinal: 3,
		Confidence: 1}
	makeRef := func(doc document) ([]byte, ArtifactRef) {
		t.Helper()
		raw, err := canonical.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		return raw, ArtifactRef{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	base := document{SchemaVersion: 1, SnapshotDigest: digest,
		Items: []item{{Binding: bindings[3], Ordinal: 3, Delta: v2draft.CorrectionVector{EV: .2}, Disposition: "apply"},
			{Binding: bindings[4], Ordinal: 4, Disposition: "manual_locked"}}}
	raw, ref := makeRef(base)
	if err := validateCorrectionPlan(raw, ref, digest, group, bindings); err != nil {
		t.Fatal(err)
	}
	cases := map[string]document{}
	missing := base
	missing.Items = append([]item(nil), base.Items[:1]...)
	cases["missing member"] = missing
	rebound := base
	rebound.Items = append([]item(nil), base.Items...)
	rebound.Items[1].Binding.AssetVersionID = uuid.New()
	cases["rebound version"] = rebound
	duplicate := base
	duplicate.Items = append([]item(nil), base.Items...)
	duplicate.Items[1].Ordinal = 3
	cases["duplicate ordinal"] = duplicate
	locked := base
	locked.Items = append([]item(nil), base.Items...)
	locked.Items[1].Delta.EV = .1
	cases["nonzero locked delta"] = locked
	wrongDigest := base
	wrongDigest.SnapshotDigest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	cases["wrong snapshot"] = wrongDigest
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			body, ref := makeRef(doc)
			if err := validateCorrectionPlan(body, ref, digest, group, bindings); err == nil {
				t.Fatal("invalid correction accepted")
			}
		})
	}
	ref.SHA256 = digest
	if err := validateCorrectionPlan(raw, ref, digest, group, bindings); err == nil {
		t.Fatal("changed object SHA accepted")
	}
}
