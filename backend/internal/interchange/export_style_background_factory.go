package interchange

import (
	"bytes"
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// ExportStyleBackgroundAuthorityFactory binds an operator-installed policy to
// one export. A process-wide authority cannot retain tenant/session identity.
type ExportStyleBackgroundAuthorityFactory interface {
	BuildExportStyleBackgroundAuthority(context.Context, CreateIntent, FrozenExportDocuments,
		ExportDocumentCatalog, ImportDocumentObjects, NativeExportImageQualifier,
	) (ExportStyleBackgroundAuthority, error)
}

// SQLAPIExportStyleBackgroundFactory derives the live editor scope from the
// catalogue already selected for this request, never from an archive field.
type SQLAPIExportStyleBackgroundFactory struct{}

func (SQLAPIExportStyleBackgroundFactory) BuildExportStyleBackgroundAuthority(ctx context.Context,
	intent CreateIntent, frozen FrozenExportDocuments, catalog ExportDocumentCatalog,
	objects ImportDocumentObjects, images NativeExportImageQualifier,
) (ExportStyleBackgroundAuthority, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scope, ok := catalog.(SQLExportDocumentCatalog)
	if !ok || scope.DB == nil || scope.Bucket == "" || scope.Tenant == uuid.Nil ||
		scope.Who.UserID == uuid.Nil || scope.Who.SessionID == uuid.Nil ||
		scope.Request.Direction != "export" ||
		objects == nil || images.Objects == nil || images.Scratch == nil || images.Probe == nil ||
		!validImportRefForSHA(images.RuntimeSHA) {
		return nil, v2input.ErrInvalid
	}
	expected, err := FreezeCreateIntent(scope.Tenant, scope.Who, scope.Request, scope.Key)
	if err != nil || expected.Identity != intent.Identity || expected.RequestSHA != intent.RequestSHA ||
		!bytes.Equal(expected.RequestJCS, intent.RequestJCS) {
		return nil, v2input.ErrInvalid
	}
	if _, _, err := DecodeFrozenExportDocuments(ctx, intent, frozen.Body()); err != nil {
		return nil, err
	}
	return SQLExportStyleBackgroundAuthority{
		Resolver: v2draft.SQLBackgroundAssetResolver{DB: scope.DB, Bucket: scope.Bucket},
		Tenant:   scope.Tenant, Project: scope.Request.ProjectID, Who: scope.Who,
		Native: images,
	}, ctx.Err()
}

var _ ExportStyleBackgroundAuthorityFactory = SQLAPIExportStyleBackgroundFactory{}
