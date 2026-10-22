package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
)

// SQLAuthoredGeometryPhaseObjects does not accept a bucket/key/version from its caller.
// It resolves only the exact predecessor producer's canonical input/output
// occurrences under current parent actor/session/project/source authority.
type SQLAuthoredGeometryPhaseObjects struct {
	API     *pgxpool.Pool
	Objects v2input.SessionAnalysisObjectReader
	Bucket  string
	current *authoredGeometryCurrentObjectScope
}

func (s SQLAuthoredGeometryPhaseObjects) Object(ctx context.Context, who auth.Identity, tenantID, projectID, parent uuid.UUID, ordinal int, ref v2input.Ref) (objectstore.Object, error) {
	var out objectstore.Object
	if e := ctx.Err(); e != nil {
		return out, e
	}
	if s.current != nil {
		object, found, e := s.currentObject(ctx, who, tenantID, projectID, parent, ordinal, ref)
		if e != nil || found {
			return object, e
		}
	}
	var bucket string
	if s.API == nil || s.Objects == nil || s.Bucket == "" || tenantID == uuid.Nil || projectID == uuid.Nil || parent == uuid.Nil || ordinal < 1 || ordinal >= 5000 || ref.ArtifactID == uuid.Nil {
		return out, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(ref.SHA256); err != nil {
		return out, v2input.ErrInvalid
	}
	err := tenant.WithTenant(ctx, s.API, who, tenantID, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		if _, err := project.Authorize(ctx, tx, tenantID, projectID, who.UserID, role, domain.Editor, false); err != nil {
			return err
		}
		// SQL additionally binds project through the immutable parent. No caller
		// project can be substituted for that parent merely to obtain authorization.
		var bound bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT FROM photo.runs WHERE tenant_id=$1 AND id=$2 AND created_by=$3 AND config_snapshot->>'project_id'=$4)`, tenantID, parent, who.UserID, projectID.String()).Scan(&bound); err != nil {
			return err
		}
		if !bound {
			return v2input.ErrInvalid
		}
		return tx.QueryRow(ctx, `SELECT bucket,object_key,object_version,sha256,bytes,media_type FROM photo.authored_render_geometry_phase_object($1,$2,$3,$4,$5,$6,$7)`, tenantID, parent, ordinal, who.UserID, who.SessionID, ref.ArtifactID, ref.SHA256).Scan(&bucket, &out.Key, &out.VersionID, &out.SHA256, &out.Bytes, &out.ContentType)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	if err != nil {
		return objectstore.Object{}, err
	}
	if bucket != s.Bucket || out.Key == "" || out.VersionID == "" || out.VersionID == "null" || out.SHA256 != ref.SHA256 || out.Bytes < 1 {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return out, nil
}

// ReadJSON closes the SQL transaction before physical IO, verifies the actual
// immutable version/length/hash, then repeats the current scope. It cannot read
// final submitted pixels as if they were the independent reconstruction input.
func (s SQLAuthoredGeometryPhaseObjects) ReadJSON(ctx context.Context, who auth.Identity, tenantID, projectID, parent uuid.UUID, ordinal int, ref v2input.Ref) ([]byte, error) {
	obj, err := s.Object(ctx, who, tenantID, projectID, parent, ordinal, ref)
	if err != nil {
		return nil, err
	}
	if obj.ContentType != "application/json" || obj.Bytes < 2 || obj.Bytes > 1048576 {
		return nil, v2input.ErrInvalid
	}
	stream, err := s.Objects.Open(ctx, obj)
	if err != nil {
		return nil, err
	}
	raw, readErr := readAdoptedRoot(ctx, stream, obj.Bytes)
	closeErr := stream.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != ref.SHA256 {
		return nil, v2input.ErrInvalid
	}
	current, err := s.Object(ctx, who, tenantID, projectID, parent, ordinal, ref)
	if err != nil {
		return nil, err
	}
	if current.Key != obj.Key || current.VersionID != obj.VersionID || current.SHA256 != obj.SHA256 || current.Bytes != obj.Bytes || current.ContentType != obj.ContentType {
		return nil, v2input.ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}
