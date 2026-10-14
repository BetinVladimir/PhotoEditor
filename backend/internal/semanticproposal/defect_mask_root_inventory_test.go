package semanticproposal

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestDefectMaskRootInventoryRetainsOrderedFaceBinding(t *testing.T) {
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	landmarks := review.ArtifactRef{ID: uuid.New(), SHA256: sha}
	mask := review.ProtectionMask{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: sha}, Selector: "skin"}
	face := review.DefectProposalFaceInput{Landmarks: landmarks, Skin: &mask}
	params, _ := json.Marshal(map[string]any{"faces": []review.DefectProposalFaceInput{face}})
	content, _ := json.Marshal(defectActivationContent{ParamsText: string(params)})
	root := review.ProposalFaceMaskRoot{Root: review.ArtifactRef{ID: uuid.New(), SHA256: sha}, Binding: review.ProposalFaceMask{Landmarks: landmarks, Mask: mask}}
	for changed := 0; changed < 9; changed++ {
		candidate := root
		roots := []review.ProposalFaceMaskRoot{candidate}
		switch changed {
		case 1:
			roots = []review.ProposalFaceMaskRoot{}
		case 2:
			roots[0].Binding.Ordinal++
		case 3:
			roots[0].Binding.Landmarks.ID = uuid.New()
		case 4:
			roots[0].Binding.Mask.Selector = "manual"
		case 5:
			roots = append(roots, root)
		case 6:
			roots[0].Binding.ManualMode = true
		case 7:
			foreign := uuid.New()
			roots[0].Binding.ObjectID = &foreign
		case 8:
			roots[0].Binding.Mask.Confidence = 0.5
		}
		body, _ := json.Marshal(roots)
		parser, _ := json.Marshal(defectParserAuthority{SchemaVersion: 1, Roots: body})
		err := validateDefectMaskRootInventory(content, parser)
		if changed == 0 && err != nil {
			t.Fatal("valid root binding rejected", err)
		}
		if changed != 0 && err != v2input.ErrInvalid {
			t.Fatal("substituted mask root accepted", changed, err)
		}
	}
}

func TestDefectMaskRootInventoryAllowsEmptyAndRejectsMissingOrConflictingRoots(t *testing.T) {
	emptyContent, _ := json.Marshal(defectActivationContent{ParamsText: `{"faces":[]}`})
	emptyParser, _ := json.Marshal(defectParserAuthority{SchemaVersion: 1, Roots: json.RawMessage("[]")})
	if err := validateDefectMaskRootInventory(emptyContent, emptyParser); err != nil {
		t.Fatal("empty mask inventory rejected", err)
	}
	wrongVersion, _ := json.Marshal(defectParserAuthority{SchemaVersion: 2, Roots: json.RawMessage("[]")})
	if err := validateDefectMaskRootInventory(emptyContent, wrongVersion); err != v2input.ErrInvalid {
		t.Fatal("unknown root packet schema accepted", err)
	}
	nullParser, _ := json.Marshal(defectParserAuthority{SchemaVersion: 1, Roots: json.RawMessage("null")})
	if err := validateDefectMaskRootInventory(emptyContent, nullParser); err != v2input.ErrInvalid {
		t.Fatal("null mask inventory accepted", err)
	}
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	landmarks := review.ArtifactRef{ID: uuid.New(), SHA256: sha}
	mask := review.ProtectionMask{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: sha}, Selector: "skin"}
	face := review.DefectProposalFaceInput{Landmarks: landmarks, Skin: &mask}
	params, _ := json.Marshal(map[string]any{"faces": []review.DefectProposalFaceInput{face, face}})
	content, _ := json.Marshal(defectActivationContent{ParamsText: string(params)})
	ref := review.ArtifactRef{ID: uuid.New(), SHA256: sha}
	roots := []review.ProposalFaceMaskRoot{{Root: ref, Binding: review.ProposalFaceMask{Ordinal: 0, Landmarks: landmarks, Mask: mask}}, {Root: ref, Binding: review.ProposalFaceMask{Ordinal: 1, Landmarks: landmarks, Mask: mask}}}
	check := func(roots []review.ProposalFaceMaskRoot) error {
		body, _ := json.Marshal(roots)
		parser, _ := json.Marshal(defectParserAuthority{SchemaVersion: 1, Roots: body})
		return validateDefectMaskRootInventory(content, parser)
	}
	if err := check(roots); err != nil {
		t.Fatal("consistent shared root rejected", err)
	}
	roots[1].Root.SHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := check(roots); err != v2input.ErrInvalid {
		t.Fatal("shared root with contradictory SHA accepted", err)
	}
}
