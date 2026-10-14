package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type defectFaceAuthorityEntry struct {
	Landmarks      v2input.Ref        `json:"landmarks"`
	Detector       v2input.Ref        `json:"detector"`
	Expected       json.RawMessage    `json:"expected"`
	Evidence       json.RawMessage    `json:"evidence"`
	Policies       [2]json.RawMessage `json:"policies"`
	Qualifications [2]json.RawMessage `json:"qualifications"`
}

// Exact original qualified face producers, one per authorized landmark file.
// Empty faces are allowed; omitting a retained landmark producer is rejected.
// No object IO or queue authority; source/policy/SQL inventory locks are separate.
func (s SQLTransferSourceReader) marshalDefectFaceAuthority(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, prepared defectFaceExecutionPreparation) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if prepared.producerCount < 0 || prepared.producerCount > len(prepared.producers) {
		return nil, v2input.ErrInvalid
	}
	files, err := s.decodeDefectExecutionFilePins(ctx, who, tenant, binding, prepared.source)
	if err != nil {
		return nil, err
	}
	landmarks := map[v2input.Ref]bool{}
	for _, file := range files {
		if file.Contract == "FaceLandmarks" {
			landmarks[file.Ref] = false
		}
	}
	if len(landmarks) != prepared.producerCount {
		return nil, v2input.ErrInvalid
	}
	entries := make([]defectFaceAuthorityEntry, 0, prepared.producerCount)
	for _, proof := range prepared.producers[:prepared.producerCount] {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ref := proof.objects[0][0].ref
		seen, exists := landmarks[ref]
		if !exists || seen || !validFacePreparationProofScope(s, who, tenant, binding, proof) || proof.expected != prepared.expected || proof.qualification != prepared.qualification {
			return nil, v2input.ErrInvalid
		}
		landmarks[ref] = true
		for _, file := range files {
			if file.Ref == ref && file.Object != proof.objects[0][0].object {
				return nil, v2input.ErrConflict
			}
		}
		evidence, err := marshalFaceEvidencePins(proof)
		if err != nil {
			return nil, err
		}
		policies, pins, err := marshalFaceQualificationPins(proof)
		if err != nil {
			return nil, err
		}
		entries = append(entries, defectFaceAuthorityEntry{Landmarks: ref, Detector: proof.objects[1][0].ref, Expected: json.RawMessage(proof.expected), Evidence: evidence, Policies: [2]json.RawMessage{json.RawMessage(policies[0]), json.RawMessage(policies[1])}, Qualifications: [2]json.RawMessage{json.RawMessage(pins[0]), json.RawMessage(pins[1])}})
	}
	body, err := canonical.Marshal(entries)
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
