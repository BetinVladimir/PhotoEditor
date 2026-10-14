package semanticproposal

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestManualProtectionDeclarationRequiresCompleteOriginalPins(t *testing.T) {
	left := defectEvidenceObject{ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}}
	right := defectEvidenceObject{ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}}
	makeSelection := func() nativeInputManualSelection {
		s := nativeInputManualSelection{}
		for _, child := range []defectEvidenceObject{left, right} {
			proof := manualPreparationProof{}
			proof.children[0] = child
			s.proofs = append(s.proofs, ownDefectManualExecutionProof(proof))
			s.policy.Protections = append(s.policy.Protections, review.ProtectionMask{Artifact: review.ArtifactRef{ID: child.ref.ArtifactID, SHA256: child.ref.SHA256}, Protected: true})
		}
		return s
	}
	good := makeSelection()
	got, err := nativeInputManualDeclaredProtectionPins(context.Background(), good, good.policy.Protections)
	if err != nil || !reflect.DeepEqual(got, []defectEvidenceObject{left, right}) {
		t.Fatal(got, err)
	}
	for _, mode := range []string{"omitted", "substituted", "duplicate", "geometry", "unknown", "conflicting_pin", "permanent"} {
		t.Run(mode, func(t *testing.T) {
			s := makeSelection()
			declared := append([]review.ProtectionMask(nil), s.policy.Protections...)
			switch mode {
			case "omitted":
				declared = declared[:1]
			case "substituted":
				declared[0].Artifact.SHA256 = strings.Repeat("c", 64)
			case "duplicate":
				declared[1] = declared[0]
				s.policy.Protections[1] = declared[0]
			case "geometry":
				declared[0].Canvas.Width++
			case "unknown":
				declared[0].Artifact.ID = uuid.New()
				s.policy.Protections[0] = declared[0]
			case "conflicting_pin":
				p := s.proofs[0]
				p.proof.children[0].object.Key = "changed"
				s.proofs = append(s.proofs, p)
			case "permanent":
				s.policy.Permanent = &declared[0]
			}
			out, err := nativeInputManualDeclaredProtectionPins(context.Background(), s, declared)
			if err == nil || out != nil {
				t.Fatal("invalid declaration exposed pins", out, err)
			}
		})
	}
}
