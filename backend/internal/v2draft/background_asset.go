package v2draft

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"strings"
)

// PortableBackgroundAsset pins imported bytes, not normalized render pixels.
// Callers must decode/normalize them and freeze that output before rendering.
type PortableBackgroundAsset struct {
	PortableID     string
	StyleVersionID uuid.UUID
	Ref            v2input.Ref
	Object         objectstore.Object
}

type SQLBackgroundAssetResolver struct {
	DB     *pgxpool.Pool
	Bucket string
}

func (s SQLBackgroundAssetResolver) Resolve(ctx context.Context, who auth.Identity,
	tenantID, projectID uuid.UUID, styleRef v2input.Ref, portableID string) (PortableBackgroundAsset, error) {
	var out PortableBackgroundAsset
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if tenantID == uuid.Nil || projectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !isDocumentRef(styleRef) || portableID == "" || len(portableID) > 120 || strings.TrimSpace(portableID) != portableID {
		return out, v2input.ErrInvalid
	}
	if s.DB == nil || s.Bucket == "" {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
        var err error
        out, err = s.ResolveTx(ctx, tx, who, tenantID, projectID, styleRef, portableID, role)
        return err
    })
    return out, err
}

// ResolveTx reuses the caller's admission transaction, preserving project ACL
// and accepted-style portable binding authority without a second transaction.
func (s SQLBackgroundAssetResolver) ResolveTx(ctx context.Context, tx pgx.Tx, who auth.Identity, tenantID, projectID uuid.UUID, styleRef v2input.Ref, portableID string, role domain.Role) (PortableBackgroundAsset,error) {
 var out PortableBackgroundAsset
 if err:=ctx.Err();err!=nil { return out,err }
 if tx==nil || s.Bucket=="" || tenantID==uuid.Nil || projectID==uuid.Nil || who.UserID==uuid.Nil || who.SessionID==uuid.Nil || !isDocumentRef(styleRef) || portableID=="" || len(portableID)>120 || strings.TrimSpace(portableID)!=portableID { return out,v2input.ErrInvalid }
 if _,err:=project.Authorize(ctx,tx,tenantID,projectID,who.UserID,role,domain.Editor,false);err!=nil { return out,err }
 var bucket string
 var matches int64
	err := tx.QueryRow(ctx, `SELECT b.portable_asset_id,v.id,a.id,a.sha256,a.bytes,
   a.bucket,a.object_key,a.object_version,a.media_type,count(*) OVER ()
   FROM photo.styles s JOIN photo.style_versions v ON v.tenant_id=s.tenant_id AND v.style_id=s.id
   JOIN photo.style_asset_bindings b ON b.tenant_id=v.tenant_id AND b.style_version_id=v.id
   JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id AND a.sha256=b.sha256
   JOIN photo.artifacts config ON config.tenant_id=v.tenant_id AND config.id=v.export_artifact_id AND config.sha256=v.config_hash
   WHERE s.tenant_id=$1 AND s.project_id=$2 AND s.archived_at IS NULL
    AND v.approval_status='accepted' AND v.export_artifact_id=$3 AND v.config_hash=$4
    AND b.portable_asset_id=$5 AND (a.output_role='style_asset' OR
     (a.output_role='v30_import_prepared' AND photo.v30_registered_style_background($1,$2,v.id,a.id,a.sha256,$5,$6,$7) IS TRUE))
    AND v.config_json#>>'{extensions,background,mode}'='asset'
    AND v.config_json#>>'{extensions,background,asset_id}'=$5
    AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
    AND a.object_version NOT IN ('','null') AND a.bytes>0 AND a.bytes<=536870912
    AND a.media_type IN ('image/jpeg','image/png','image/tiff')
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
    AND config.state='ready' AND config.verified_at IS NOT NULL
    AND config.object_version IS NOT NULL AND config.object_version NOT IN ('','null')
    AND (config.expires_at IS NULL OR config.expires_at>clock_timestamp())`, tenantID, projectID, styleRef.ArtifactID, styleRef.SHA256, portableID, who.UserID, who.SessionID).Scan(&out.PortableID, &out.StyleVersionID, &out.Ref.ArtifactID, &out.Ref.SHA256, &out.Object.Bytes, &bucket, &out.Object.Key, &out.Object.VersionID, &out.Object.ContentType, &matches)
	if errors.Is(err, pgx.ErrNoRows) { return PortableBackgroundAsset{}, v2input.ErrInvalid }
 if err!=nil { return PortableBackgroundAsset{},err }
 if matches!=1 || bucket!=s.Bucket || !isDocumentRef(out.Ref) { return PortableBackgroundAsset{},v2input.ErrInvalid }
 out.Object.SHA256=out.Ref.SHA256
 return out,nil
}
