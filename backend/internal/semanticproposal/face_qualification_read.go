package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/facequalification"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// VerifyFaceQualification checks one independently configured trusted operation
// report and repeats catalog/policy authority after original object IO. It does
// not bind producer execution profiles or qualify a different face operation.
func (s SQLTransferSourceReader) VerifyFaceQualification(ctx context.Context, who auth.Identity, tenantID uuid.UUID, report v2input.Ref, expected v2qualification.Expected) (v2qualification.Report, error) {
	return (facequalification.Reader{DB: s.DB, Objects: s.Objects, Bucket: s.Bucket, ProjectID: s.ProjectID}).Verify(ctx, who, tenantID, report, expected)
}

// VerifyFaceQualificationTyped repeats the same physical and SQL authority as
// the legacy API while retaining MPS allocator-bound resource semantics.
func (s SQLTransferSourceReader) VerifyFaceQualificationTyped(ctx context.Context, who auth.Identity, tenantID uuid.UUID, report v2input.Ref, expected v2qualification.Expected) (v2qualification.Verification,error) {
 return (facequalification.Reader{DB:s.DB,Objects:s.Objects,Bucket:s.Bucket,ProjectID:s.ProjectID}).VerifyTyped(ctx,who,tenantID,report,expected)
}
