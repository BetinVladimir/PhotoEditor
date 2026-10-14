package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type defectParserAuthorityEntry struct {
	Labels         v2input.Ref        `json:"labels"`
	Pixels         v2input.Ref        `json:"pixels"`
	Detector       v2input.Ref        `json:"detector"`
	Expected       json.RawMessage    `json:"expected"`
	Evidence       json.RawMessage    `json:"evidence"`
	Policies       [2]json.RawMessage `json:"policies"`
	Qualifications [2]json.RawMessage `json:"qualifications"`
}

type defectParserAuthority struct {
	SchemaVersion int                          `json:"schema_version"`
	Roots         json.RawMessage              `json:"roots"`
	Parsers       []defectParserAuthorityEntry `json:"parsers"`
}

// Retains original parser/label provenance and qualified policy/report pairs.
// Root derivation, manual/source authority and atomic SQL admission are separate.
func (s SQLTransferSourceReader) marshalDefectParserAuthority(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, prepared defectMaskExecutionPreparation) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if prepared.parserCount < 0 || prepared.parserCount > len(prepared.parsers) || len(prepared.expected) < 2 || len(prepared.expected) > 4<<20 || len(prepared.roots) < 2 || len(prepared.roots) > 4<<20 {
		return nil, v2input.ErrInvalid
	}
	// Source scope must be checked even when no skin parser is required.
	if _, err := s.decodeDefectExecutionFilePins(ctx, who, tenant, binding, prepared.face.source); err != nil {
		return nil, err
	}
	var expected [4]v2qualification.Expected
	if json.Unmarshal([]byte(prepared.expected), &expected) != nil {
		return nil, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(expected)
	if err != nil || string(body) != prepared.expected {
		return nil, v2input.ErrInvalid
	}
	faceExpected, err := canonical.Marshal([2]v2qualification.Expected{expected[0], expected[1]})
	if err != nil || string(faceExpected) != prepared.face.expected || prepared.qualification[0] != prepared.face.qualification[0] || prepared.qualification[1] != prepared.face.qualification[1] {
		return nil, v2input.ErrInvalid
	}
	parserExpected, err := canonical.Marshal([2]v2qualification.Expected{expected[2], expected[3]})
	if err != nil {
		return nil, err
	}
	var roots []review.ProposalFaceMaskRoot
	if json.Unmarshal([]byte(prepared.roots), &roots) != nil {
		return nil, v2input.ErrInvalid
	}
	body, err = canonical.Marshal(roots)
	if err != nil || string(body) != prepared.roots {
		return nil, v2input.ErrInvalid
	}
	skinRoots := 0
	for _, root := range roots {
		if root.Binding.Mask.Selector == "skin" {
			skinRoots++
		}
	}
	if (skinRoots == 0) != (prepared.parserCount == 0) || prepared.parserCount > skinRoots {
		return nil, v2input.ErrInvalid
	}
	entries := make([]defectParserAuthorityEntry, 0, prepared.parserCount)
	seen := map[v2input.Ref]bool{}
	for _, proof := range prepared.parsers[:prepared.parserCount] {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ref := proof.evidence.objects[0][0].ref
		if !validFaceParsePreparationProofScope(s, who, tenant, binding, proof) || proof.evidence.expected != string(parserExpected) || proof.evidence.qualification != [2]personEvidenceObject{prepared.qualification[2], prepared.qualification[3]} || seen[ref] {
			return nil, v2input.ErrInvalid
		}
		seen[ref] = true
		evidence, err := marshalFaceParseEvidencePins(proof)
		if err != nil {
			return nil, err
		}
		policies, pins, err := marshalSelectedFaceQualificationPins(proof.evidence, [2]string{"face.parse", "face.detect"})
		if err != nil {
			return nil, err
		}
		entries = append(entries, defectParserAuthorityEntry{Labels: ref, Pixels: proof.pixels.ref, Detector: proof.evidence.objects[1][0].ref, Expected: json.RawMessage(proof.evidence.expected), Evidence: evidence, Policies: [2]json.RawMessage{json.RawMessage(policies[0]), json.RawMessage(policies[1])}, Qualifications: [2]json.RawMessage{json.RawMessage(pins[0]), json.RawMessage(pins[1])}})
	}
	body, err = canonical.Marshal(defectParserAuthority{SchemaVersion: 1, Roots: json.RawMessage(prepared.roots), Parsers: entries})
	if err != nil {
		return nil, err
	}
	if len(body) > 8<<20 {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return body, nil
}
