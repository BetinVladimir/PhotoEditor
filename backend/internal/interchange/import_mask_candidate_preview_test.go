package interchange

import (
	"testing"

	"github.com/google/uuid"
)

func TestImportMaskPreviewSelectsOneExactArchivedPair(t *testing.T) {
	overrideID, maskID := uuid.New(), uuid.New()
	candidate := ImportPersonMaskCandidate{Decision: ImportPersonMaskDecisionRequest{OverrideArtifactID: overrideID, MaskArtifactID: maskID}}
	if got, err := selectImportMaskCandidate([]ImportPersonMaskCandidate{candidate}, overrideID, maskID); err != nil || got != candidate {
		t.Fatal("lost exact candidate", err)
	}
	if _, err := selectImportMaskCandidate([]ImportPersonMaskCandidate{candidate}, uuid.New(), maskID); err == nil {
		t.Fatal("foreign override selected")
	}
	if _, err := selectImportMaskCandidate([]ImportPersonMaskCandidate{candidate, candidate}, overrideID, maskID); err == nil {
		t.Fatal("duplicate candidate selected")
	}
}
