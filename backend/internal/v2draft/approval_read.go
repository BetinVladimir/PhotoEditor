package v2draft

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type ApprovalDraft struct {
	ProjectID      uuid.UUID
	AssetVersionID uuid.UUID
	StreamID       uuid.UUID
	DocumentID     uuid.UUID
	Revision       int64
	Approved       bool
	Binding        v2input.SessionVersionBinding
	Image          review.WorkingImage
	Recipe         []byte
}

// ReadApprovalDraft pins the current recipe and its exact object version.
// approve_v2_draft repeats the document/revision/source CAS after the resolved
// artifact is staged, so a concurrent replace cannot approve these stale bytes.
func (s Service) ReadApprovalDraft(ctx context.Context, who auth.Identity,
	tenantID, assetID, documentID uuid.UUID, expected int64) (ApprovalDraft, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" {
		return ApprovalDraft{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if assetID == uuid.Nil || documentID == uuid.Nil || expected < 1 {
		return ApprovalDraft{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	binding, projectID, image, err := (BindingResolver{DB: s.DB,
		Objects: s.Objects, Bucket: s.Bucket}).CurrentScopedImage(ctx, who,
		tenantID, assetID)
	if err != nil {
		return ApprovalDraft{}, draftMutationError(err)
	}
	out := ApprovalDraft{ProjectID: projectID, AssetVersionID: binding.AssetVersionID,
		DocumentID: documentID, Binding: binding, Image: image}
	var ref v2input.Ref
	var object objectstore.Object
	var bucket string
	var approved *uuid.UUID
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor,
		func(tx pgx.Tx, role domain.Role) error {
			if _, e := project.Authorize(ctx, tx, tenantID, projectID,
				who.UserID, role, domain.Editor, false); e != nil {
				return e
			}
			e := tx.QueryRow(ctx, `SELECT s.id,s.revision,s.approved_document_id,
			 d.artifact_id,d.sha256,
			 a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
			 FROM photo.v2_document_streams s
			 JOIN photo.v2_documents d ON d.tenant_id=s.tenant_id AND d.id=s.current_document_id
			  AND d.stream_id=s.id AND d.project_id=s.project_id
			 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
			  AND a.sha256=d.sha256
			 WHERE s.tenant_id=$1 AND s.project_id=$2 AND s.asset_version_id=$3
			  AND s.stream_type='draft' AND d.id=$4
			  AND d.document_type='EditingRecipeV3' AND a.state='ready'
			  AND a.output_role='document' AND a.verified_at IS NOT NULL
			  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
			  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())`,
				tenantID, projectID, binding.AssetVersionID, documentID).Scan(
				&out.StreamID, &out.Revision, &approved,
				&ref.ArtifactID, &ref.SHA256,
				&object.Bytes, &bucket, &object.Key, &object.VersionID,
				&object.ContentType)
			if errors.Is(e, pgx.ErrNoRows) {
				return domain.NewError("REVISION_CONFLICT", 409)
			}
			return e
		})
	if err != nil {
		return ApprovalDraft{}, draftMutationError(err)
	}
	if out.Revision != expected && !(approved != nil && *approved == documentID &&
		out.Revision > 1 && out.Revision-1 == expected) {
		return ApprovalDraft{}, domain.NewError("REVISION_CONFLICT", 409)
	}
	out.Approved = approved != nil && *approved == documentID
	if bucket != s.Bucket || !isDocumentRef(ref) {
		return ApprovalDraft{}, domain.NewError("DOCUMENT_INVALID", 422)
	}
	object.SHA256 = ref.SHA256
	out.Recipe, err = readJSONArtifact(ctx, s.Objects, object, ref)
	if err != nil {
		return ApprovalDraft{}, draftMutationError(err)
	}
	if err = v2input.ValidateEditingRecipeV3(out.Recipe, binding); err != nil {
		return ApprovalDraft{}, domain.NewError("DOCUMENT_INVALID", 422)
	}
	return out, nil
}
