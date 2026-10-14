package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestImportLUTOccurrenceRequiresParentAndDerivedIdentity(t *testing.T) {
	workflow, entry := uuid.New(), uuid.New()
	original := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("lut"))}
	parent := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("resolved"))}
	mapped := v2input.Ref{ArtifactID: uuid.NewSHA1(workflow, []byte("interchange:dependency:"+entry.String()+":AbsoluteLUTArray:"+original.ArtifactID.String())), SHA256: original.SHA256}
	scope := ImportPreparedScope{Workflow: workflow, Entry: entry, Contract: "AbsoluteLUTArray", LUT: &ImportLUTOccurrence{Original: original, Parent: parent, Size: 33}}
	if err := validateImportLUTOccurrence(scope, mapped); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"contract", "parent", "size", "sha", "id", "warp"} {
		candidate := scope
		ref := mapped
		owned := *scope.LUT
		candidate.LUT = &owned
		switch change {
		case "contract":
			candidate.Contract = "MaskPixels"
		case "parent":
			owned.Parent = original
		case "size":
			owned.Size = 17
		case "sha":
			ref.SHA256 = rawSHA([]byte("foreign"))
		case "id":
			ref.ArtifactID = uuid.New()
		case "warp":
			candidate.Warp = &ImportWarpOccurrence{}
		}
		if err := validateImportLUTOccurrence(candidate, ref); err == nil {
			t.Fatal("invalid occurrence accepted", change)
		}
	}
}

func TestImportLUTPublisherRequiresOccurrencePort(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("lut"))}
	p := PreparedImportArtifactPublisher{}
	if _, err := p.PublishImportLUT(context.Background(), uuid.New(), ref, objectstore.Object{}, ref, ref, 33); err == nil {
		t.Fatal("unscoped LUT accepted")
	}
}
