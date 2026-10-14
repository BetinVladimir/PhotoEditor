package interchange

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/workflow"
)

// ImportPersonAcceptanceBinder installs consent only after the scheduler has
// obtained an exact candidate. An unbound installation never accepts a mask.
type ImportPersonAcceptanceBinder interface {
	ImportMaskQualifier
	BindImportPersonAcceptance(context.Context, workflow.InterchangeImportCandidate, []byte, ImportResolverDependencies) (ImportMaskQualifier, error)
}

// This authority reads the user's current, separately recorded confirmation
// and the current person-origin gate. No archive flag or operator JSON grants it.
type SQLImportPersonAcceptanceBinder struct{}

func (SQLImportPersonAcceptanceBinder) QualifyImportedMask(context.Context, review.MaskRef, v2input.SessionVersionBinding) error {
	return v2input.ErrInvalid
}

func (SQLImportPersonAcceptanceBinder) BindImportPersonAcceptance(ctx context.Context,
	c workflow.InterchangeImportCandidate, parsed []byte, deps ImportResolverDependencies,
) (ImportMaskQualifier, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if deps.DB == nil || c.Tenant == uuid.Nil || c.Run == uuid.Nil ||
		c.Workflow == uuid.Nil || c.Project == uuid.Nil || c.Bucket == "" ||
		c.Bucket != deps.Bucket || c.Artifact == uuid.Nil || rawSHA(parsed) != c.SHA {
		return nil, v2input.ErrInvalid
	}
	if _, err := validateParsedImportDocument(ctx, parsed); err != nil {
		return nil, err
	}
	return SQLImportConfirmedMaskAuthority{Store: SQLPreparedImportArtifactStore{
		DB: deps.DB, Tenant: c.Tenant,
		Identity: ImportDocumentIdentity{Run: c.Run, Workflow: c.Workflow},
		Parsed:   append([]byte(nil), parsed...), Bucket: c.Bucket,
	}}, ctx.Err()
}

var _ ImportPersonAcceptanceBinder = SQLImportPersonAcceptanceBinder{}
