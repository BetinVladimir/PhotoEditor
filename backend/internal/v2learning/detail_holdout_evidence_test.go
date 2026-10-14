package v2learning

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func detailEvidenceRef(value byte) v2input.Ref {
	var id uuid.UUID
	id[15] = value
	return v2input.Ref{ArtifactID: id, SHA256: strings.Repeat("a", 63) + "b"}
}

func detailEvidenceOrigin() PortraitDetailHoldoutOrigin {
	identity := func(value byte) uuid.UUID { return detailEvidenceRef(value).ArtifactID }
	return PortraitDetailHoldoutOrigin{
		TenantID: identity(1), EventID: identity(2), AssetVersionID: identity(3),
		RunID: identity(4), TaskID: identity(5), ResultID: identity(6),
		AttemptID: identity(7), Source: detailEvidenceRef(16),
		Image:      detailEvidenceRef(8),
		Protection: detailEvidenceRef(9), Params: detailEvidenceRef(10),
		Resolved: detailEvidenceRef(11),
	}
}

func TestFrozenPortraitDetailEvidenceOwnsMasksAndBindsProducer(t *testing.T) {
	origin := detailEvidenceOrigin()
	masks := []v2input.Ref{detailEvidenceRef(12), detailEvidenceRef(13)}
	origin.MaskArtifacts = append([]v2input.Ref(nil), masks...)
	frozen, body, digest, err := CompileFrozenPortraitDetailHoldoutEvidence(origin, masks)
	if err != nil || len(body) == 0 || len(digest) != 64 {
		t.Fatal("valid frozen detail evidence refused", err)
	}
	masks[0] = detailEvidenceRef(14)
	if frozen.MaskArtifacts[0] != detailEvidenceRef(12) {
		t.Fatal("caller changed frozen mask inventory")
	}
	ref := detailEvidenceRef(15)
	ref.SHA256 = digest
	if _, err := DecodeFrozenPortraitDetailHoldoutEvidence(body, ref, origin); err != nil {
		t.Fatal("exact producer evidence refused", err)
	}
	changed := origin
	changed.ResultID = uuid.New()
	if _, err := DecodeFrozenPortraitDetailHoldoutEvidence(body, ref, changed); err == nil {
		t.Fatal("changed producer result admitted")
	}
	changed = origin
	changed.Source = detailEvidenceRef(17)
	if _, err := DecodeFrozenPortraitDetailHoldoutEvidence(body, ref, changed); err == nil {
		t.Fatal("changed training source admitted")
	}
	changed = origin
	changed.MaskArtifacts = []v2input.Ref{detailEvidenceRef(12), detailEvidenceRef(14)}
	if _, err := DecodeFrozenPortraitDetailHoldoutEvidence(body, ref, changed); err == nil {
		t.Fatal("changed producer mask inventory admitted")
	}
	if _, _, _, err := CompileFrozenPortraitDetailHoldoutEvidence(changed, origin.MaskArtifacts); err == nil {
		t.Fatal("compiler accepted masks outside the producer inventory")
	}
	for _, inventory := range [][]v2input.Ref{
		{detailEvidenceRef(13), detailEvidenceRef(12)},
		{detailEvidenceRef(12), detailEvidenceRef(12)},
		{detailEvidenceRef(8)},
	} {
		if _, _, _, err := CompileFrozenPortraitDetailHoldoutEvidence(origin, inventory); err == nil {
			t.Fatal("invalid mask inventory admitted")
		}
	}
}
