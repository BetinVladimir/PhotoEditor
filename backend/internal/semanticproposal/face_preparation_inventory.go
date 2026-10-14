package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type preparedFaceCollection struct {
	candidate facePreparationCandidate
	faces     []review.Face68
	proof     facePreparationProof
}

func sameFacePreparationCandidates(a, b []facePreparationCandidate) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Selected profile inventory only. All discovered collections must independently
// pass original content/reports/source gates. No partial set is returned. Full
// required policy discovery and transactional barrier repeat remain separate.
func (s SQLTransferSourceReader) prepareQualifiedFaceInventory(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, reports [2]v2input.Ref, expected [2]v2qualification.Expected, color TransferColorProfile) ([]preparedFaceCollection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for i := range expected {
		expected[i] = ownQualificationExpected(expected[i])
	}
	before, err := s.facePreparationCandidates(ctx, who, tenantID, binding, expected)
	if err != nil {
		return nil, err
	}
	if len(before) == 0 {
		return nil, v2input.ErrInvalid
	}
	prepared := make([]preparedFaceCollection, 0, len(before))
	for _, candidate := range before {
		faces, proof, err := s.readQualifiedFacePreparationProof(ctx, who, tenantID, binding, candidate.landmark, candidate.detection, reports, expected, color)
		if err != nil {
			return nil, err
		}
		prepared = append(prepared, preparedFaceCollection{candidate, faces, proof})
	}
	for _, collection := range prepared {
		if err := s.recheckFacePreparationProof(ctx, who, tenantID, binding, collection.proof); err != nil {
			return nil, err
		}
	}
	after, err := s.facePreparationCandidates(ctx, who, tenantID, binding, expected)
	if err != nil {
		return nil, err
	}
	if !sameFacePreparationCandidates(before, after) {
		return nil, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return prepared, nil
}
