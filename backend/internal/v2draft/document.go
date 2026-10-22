package v2draft

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// GetDocument returns exact immutable bytes after a fresh project ACL check.
// The object version and SHA are pinned in the read transaction; a storage
// swap or a non-canonical body fails before any bytes are sent to the client.
func (s Service) GetDocument(ctx context.Context, who auth.Identity, tenantID,
	documentID uuid.UUID) (json.RawMessage, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" {
		return nil, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if documentID == uuid.Nil {
		return nil, domain.NewError("NOT_FOUND", 404)
	}
	var projectID uuid.UUID
	var ref v2input.Ref
	var object objectstore.Object
	var bucket, documentType string
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Viewer,
		func(tx pgx.Tx, role domain.Role) error {
			e := tx.QueryRow(ctx, `SELECT d.project_id,d.document_type,d.artifact_id,d.sha256,
			 a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
			 FROM photo.v2_documents d JOIN photo.artifacts a
			 ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.sha256=d.sha256
			 WHERE d.tenant_id=$1 AND d.id=$2 AND a.state='ready'
			 AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
			 AND a.object_version NOT IN('','null')
			 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())`,
				tenantID, documentID).Scan(&projectID, &documentType,
				&ref.ArtifactID, &ref.SHA256, &object.Bytes, &bucket,
				&object.Key, &object.VersionID, &object.ContentType)
			if errors.Is(e, pgx.ErrNoRows) {
				return domain.NewError("NOT_FOUND", 404)
			}
			if e != nil {
				return e
			}
			_, e = project.Authorize(ctx, tx, tenantID, projectID,
				who.UserID, role, domain.Viewer, false)
			return e
		})
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "42P01" {
			return nil, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
		}
		return nil, err
	}
	if bucket != s.Bucket || !isDocumentRef(ref) ||
		(documentType != "EditingRecipeV3" && documentType != "LayerStack" && documentType != "ShotList") {
		return nil, domain.NewError("DOCUMENT_INVALID", 422)
	}
	object.SHA256 = ref.SHA256
	body, err := readJSONArtifact(ctx, s.Objects, object, ref)
	if err != nil {
		return nil, draftMutationError(err)
	}
	if documentType == "ShotList" {
		if _, err := s.validateShotRead(ctx, who, tenantID, projectID, body); err != nil {
			return nil, err
		}
	}
	if documentType == "EditingRecipeV3" {
		var recipe struct {
			LearningVersionID *uuid.UUID `json:"learning_version_id"`
		}
		if json.Unmarshal(body, &recipe) != nil {
			return nil, domain.NewError("DOCUMENT_INVALID", 422)
		}
		err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Viewer,
			func(tx pgx.Tx, role domain.Role) error {
				if _, e := project.Authorize(ctx, tx, tenantID, projectID,
					who.UserID, role, domain.Viewer, false); e != nil {
					return e
				}
				var live bool
				e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1
				 FROM photo.model_use_links used
				 JOIN photo.learning_versions version ON version.tenant_id=used.tenant_id
				  AND version.project_id=used.project_id AND version.id=used.version_id
				 JOIN photo.learning_profiles profile ON profile.tenant_id=version.tenant_id
				  AND profile.project_id=version.project_id AND profile.id=version.profile_id
				 JOIN photo.version_eligibility state ON state.tenant_id=version.tenant_id
				  AND state.project_id=version.project_id AND state.version_id=version.id
				 WHERE used.tenant_id=$1 AND used.project_id=$2 AND used.document_id=$3
				  AND used.version_id=$4 AND state.status='eligible'
				  AND EXISTS(SELECT 1 FROM photo.learning_example_links link
				   WHERE link.tenant_id=$1 AND link.version_id=version.id)
				  AND NOT EXISTS(SELECT 1 FROM photo.learning_example_links link
				   LEFT JOIN photo.consent_grants grant_row ON grant_row.tenant_id=link.tenant_id
				    AND grant_row.project_id=link.project_id AND grant_row.id=link.consent_id
				   WHERE link.tenant_id=$1 AND link.version_id=version.id
				    AND (grant_row.id IS NULL OR grant_row.status<>'granted'
				     OR grant_row.epoch<>link.consent_epoch
				     OR grant_row.purpose<>CASE WHEN profile.person_id IS NULL
				      THEN 'learning_photographer' ELSE 'learning_person' END
				     OR grant_row.person_id IS DISTINCT FROM profile.person_id))
				  AND (profile.person_id IS NULL OR EXISTS(
				   SELECT 1 FROM photo.project_persons person
				   WHERE person.tenant_id=$1 AND person.project_id=$2
				    AND person.id=profile.person_id AND person.state='active'))
				 )`, tenantID, projectID, documentID,
					recipe.LearningVersionID).Scan(&live)
				if e != nil {
					return e
				}
				if recipe.LearningVersionID != nil && !live {
					return domain.NewError("PROFILE_CONSENT_REVOKED", 409)
				}
				if recipe.LearningVersionID == nil {
					var linked bool
					if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM photo.model_use_links
					 WHERE tenant_id=$1 AND project_id=$2 AND document_id=$3)`,
						tenantID, projectID, documentID).Scan(&linked); e != nil {
						return e
					}
					if linked {
						return domain.NewError("DOCUMENT_INVALID", 422)
					}
				}
				return nil
			})
		if err != nil {
			return nil, err
		}
	}
 // Object IO must not expose bytes after project withdrawal, expiry, or a
 // catalogue pin change. Historical documents remain readable: no latest-draft
 // or latest-asset requirement is introduced at this generic endpoint.
 err = tenant.WithTenant(ctx,s.DB,who,tenantID,domain.Viewer,func(tx pgx.Tx,role domain.Role)error{
  var live bool
  if e:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT FROM photo.v2_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.sha256=d.sha256 WHERE d.tenant_id=$1 AND d.id=$2 AND d.project_id=$3 AND d.document_type=$4 AND a.id=$5 AND a.sha256=$6 AND a.bucket=$7 AND a.object_key=$8 AND a.object_version=$9 AND a.bytes=$10 AND a.media_type=$11 AND a.state='ready' AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))`,tenantID,documentID,projectID,documentType,ref.ArtifactID,ref.SHA256,bucket,object.Key,object.VersionID,object.Bytes,object.ContentType).Scan(&live);e!=nil{return e}
  if !live{return domain.NewError("DOCUMENT_INVALID",422)}
  currentRole,e:=tenant.AuthorizeInTransaction(ctx,tx,who,tenantID,domain.Viewer);if e!=nil{return e}
  _,e=project.Authorize(ctx,tx,tenantID,projectID,who.UserID,currentRole,domain.Viewer,false);return e
 });if err!=nil{return nil,err};if err=ctx.Err();err!=nil{return nil,err}
 return json.RawMessage(body), nil
}
