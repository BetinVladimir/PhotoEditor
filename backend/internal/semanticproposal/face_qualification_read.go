package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// VerifyFaceQualification checks one independently configured trusted operation
// report and repeats catalog/policy authority after original object IO. It does
// not bind producer execution profiles or qualify a different face operation.
func (s SQLTransferSourceReader) VerifyFaceQualification(ctx context.Context, who auth.Identity, tenantID uuid.UUID, report v2input.Ref, expected v2qualification.Expected) (v2qualification.Report, error) {
	empty := v2qualification.Report{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	expected = ownQualificationExpected(expected)
	pin, err := s.faceQualificationObject(ctx, who, tenantID, expected, report)
	if err != nil {
		return empty, err
	}
	body, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, pin.object, pin.ref)
	if err != nil {
		return empty, err
	}
	verified, err := v2qualification.Verify(body, pin.ref.SHA256, expected)
	if err != nil {
		return empty, err
	}
	current, err := s.faceQualificationObject(ctx, who, tenantID, expected, report)
	if err != nil {
		return empty, err
	}
	if current != pin {
		return empty, v2qualification.ErrUnqualified
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return verified, nil
}
