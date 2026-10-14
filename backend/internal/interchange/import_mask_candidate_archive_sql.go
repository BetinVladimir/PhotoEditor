package interchange

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// SQLImportMaskCandidateArchiveReader reads only recipe and override documents
// authorized by the exact parsed manifest of a live import workflow.
type SQLImportMaskCandidateArchiveReader struct {
	DB               *pgxpool.Pool
	Tenant, Workflow uuid.UUID
	Who              auth.Identity
	Bucket           string
	Parsed           []byte
	Objects          ImportDocumentObjects
}

func (s SQLImportMaskCandidateArchiveReader) lookup(ctx context.Context, ref v2input.Ref, contract string) (objectstore.Object, error) {
	if err := ctx.Err(); err != nil {
		return objectstore.Object{}, err
	}
	limit := int64(1 << 20)
	if contract == "Override" {
		limit = 8 << 20
	} else if contract != "EditingRecipeV3" {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	if s.DB == nil || s.Tenant == uuid.Nil || s.Workflow == uuid.Nil || s.Who.UserID == uuid.Nil || s.Who.SessionID == uuid.Nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 || !validImportRef(ref) || len(s.Parsed) < 2 || len(s.Parsed) > 8<<20 {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	parsed := append([]byte(nil), s.Parsed...)
	normalized, err := canonical.Bytes(parsed)
	if err != nil || !bytes.Equal(normalized, parsed) {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	var object objectstore.Object
	err = tenant.WithTenant(ctx, s.DB, s.Who, s.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, `SELECT artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.v30_import_mask_archive_document($1,$2,$3,$4,$5,$6,$7,$8)`, s.Tenant, s.Who.UserID, s.Who.SessionID, s.Workflow, string(parsed), ref.ArtifactID, ref.SHA256, contract)
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return err
			}
			return artifactjson.ErrStale
		}
		var artifact uuid.UUID
		var bucket string
		if err := rows.Scan(&artifact, &object.SHA256, &bucket, &object.Key, &object.VersionID, &object.Bytes, &object.ContentType); err != nil {
			return err
		}
		if rows.Next() {
			return v2input.ErrInvalid
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if artifact != ref.ArtifactID || bucket != s.Bucket || object.SHA256 != ref.SHA256 || object.Key == "" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 || object.Bytes < 2 || object.Bytes > limit || object.ContentType != "application/octet-stream" {
			return objectstore.ErrIntegrity
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return objectstore.Object{}, artifactjson.ErrStale
	}
	if err != nil {
		return objectstore.Object{}, err
	}
	return object, ctx.Err()
}

func (s SQLImportMaskCandidateArchiveReader) Read(ctx context.Context, ref v2input.Ref, contract string) ([]byte, error) {
	if s.Objects == nil {
		return nil, v2input.ErrInvalid
	}
	object, err := s.lookup(ctx, ref, contract)
	if err != nil {
		return nil, err
	}
	reader, err := s.Objects.Open(ctx, object)
	if err != nil {
		if reader != nil {
			_ = reader.Close()
		}
		return nil, err
	}
	if reader == nil {
		return nil, v2input.ErrInvalid
	}
	raw, readErr := io.ReadAll(io.LimitReader(exportContextReader{ctx: ctx, reader: reader}, object.Bytes+1))
	closeErr := reader.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if int64(len(raw)) != object.Bytes || rawSHA(raw) != ref.SHA256 {
		return nil, objectstore.ErrIntegrity
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return nil, v2input.ErrInvalid
	}
	current, err := s.lookup(ctx, ref, contract)
	if err != nil {
		return nil, err
	}
	if current != object {
		return nil, artifactjson.ErrStale
	}
	return raw, ctx.Err()
}
