package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type preparedFaceParseCollection struct {
	candidate faceParsePreparationCandidate
	labels    review.FaceLabels19
	counts    [19]int64
	proof     faceParsePreparationProof
}

func sameFaceParsePreparationCandidates(a, b []faceParsePreparationCandidate) bool {
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
func (s SQLTransferSourceReader) prepareQualifiedFaceParseInventory(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, reports [2]v2input.Ref, expected [2]v2qualification.Expected, color TransferColorProfile) ([]preparedFaceParseCollection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for i := range expected {
		expected[i] = ownQualificationExpected(expected[i])
	}
	before, err := s.faceParsePreparationCandidates(ctx, who, tenantID, binding, expected)
	if err != nil {
		return nil, err
	}
	if len(before) == 0 {
		return nil, v2input.ErrInvalid
	}
	prepared := make([]preparedFaceParseCollection, 0, len(before))
	for _, candidate := range before {
		labels, counts, proof, err := s.readQualifiedFaceParsePreparationProof(ctx, who, tenantID, binding, candidate.parse, candidate.detection, candidate.pixels, reports, expected, color)
		if err != nil {
			return nil, err
		}
		prepared = append(prepared, preparedFaceParseCollection{candidate, labels, counts, proof})
	}
	for _, collection := range prepared {
		if err := s.recheckFaceParsePreparationProof(ctx, who, tenantID, binding, collection.proof); err != nil {
			return nil, err
		}
	}
	after, err := s.faceParsePreparationCandidates(ctx, who, tenantID, binding, expected)
	if err != nil {
		return nil, err
	}
	if !sameFaceParsePreparationCandidates(before, after) {
		return nil, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return prepared, nil
}
