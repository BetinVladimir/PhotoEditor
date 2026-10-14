package provenance

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// SQLSigningLineageStore reserves dedicated, export-owned documents. Claim
// verification remains the responsibility of CompiledExportLineageSource.
// Object I/O must occur between these transactions, never inside them.
type SQLSigningLineageStore struct {
	DB     *pgxpool.Pool
	Bucket string
}

// Exact export pins are compared in SQL while locking the current canonical
// export, task result, source artifact and project. Do not serialize Go's field
// names implicitly: this wire shape is shared with v31_signing_lineage_scope.
func signingLineageStoragePins(who auth.Identity, ready ReadySigningExport, expected v2input.Ref, raw []byte, bucket string) ([]byte, error) {
	if who.UserID == uuid.Nil || who.SessionID == uuid.Nil || ready.Tenant == uuid.Nil || ready.Project == uuid.Nil || ready.ExportID == uuid.Nil || ready.Version == uuid.Nil || ready.Task == uuid.Nil || ready.Run == uuid.Nil || !ref(ready.Ref) || !ref(expected) || !text(bucket, 63) || len(bucket) < 3 || len(raw) < 2 || len(raw) > 8<<20 {
		return nil, ErrInvalid
	}
	if ready.Object.Key == "" || ready.Object.VersionID == "" || ready.Object.VersionID == "null" || len(ready.Object.VersionID) > 1024 || ready.Object.SHA256 != ready.Ref.SHA256 || ready.Object.Bytes < 1 || ready.Object.Bytes > 512<<20 || ready.Object.ContentType != "image/jpeg" && ready.Object.ContentType != "image/png" {
		return nil, ErrInvalid
	}
	if _, err := DecodeSigningLineage(raw, expected); err != nil {
		return nil, err
	}
	if expected.ArtifactID != uuid.NewSHA1(ready.ExportID, []byte("signing:lineage:"+expected.SHA256)) {
		return nil, ErrInvalid
	}
	return signingExportStoragePins(ready)
}

func signingExportStoragePins(ready ReadySigningExport) ([]byte, error) {
	return json.Marshal(map[string]any{"project": ready.Project, "version": ready.Version, "task": ready.Task, "run": ready.Run, "artifact": ready.Ref.ArtifactID, "sha256": ready.Ref.SHA256, "key": ready.Object.Key, "object_version": ready.Object.VersionID, "bytes": ready.Object.Bytes, "media_type": ready.Object.ContentType})
}

func signingLineageReservation(outcome string, id, token *uuid.UUID, ready bool, expected v2input.Ref) (artifactjson.Reservation, error) {
	var zero artifactjson.Reservation
	switch outcome {
	case "quota":
		return zero, artifactjson.ErrQuota
	case "conflict":
		return zero, artifactjson.ErrConflict
	case "created", "resumed", "replayed":
	default:
		return zero, artifactjson.ErrStale
	}
	if id == nil || *id != expected.ArtifactID || token == nil || *token == uuid.Nil || ready != (outcome == "replayed") {
		return zero, objectstore.ErrIntegrity
	}
	result := artifactjson.Reservation{ArtifactID: *id, Token: *token}
	if ready {
		result.Ready = &artifactjson.Ref{ID: *id, SHA256: expected.SHA256}
	}
	return result, nil
}

func (s SQLSigningLineageStore) ReserveSigningLineage(ctx context.Context, who auth.Identity, ready ReadySigningExport, expected v2input.Ref, raw []byte) (artifactjson.Reservation, error) {
	var result artifactjson.Reservation
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if s.DB == nil {
		return result, ErrInvalid
	}
	pins, err := signingLineageStoragePins(who, ready, expected, raw, s.Bucket)
	if err != nil {
		return result, err
	}
	err = tenant.WithTenant(ctx, s.DB, who, ready.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var outcome string
		var id, token *uuid.UUID
		var replay bool
		if err := tx.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v31_signing_lineage($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9)`, ready.Tenant, who.UserID, who.SessionID, ready.ExportID, string(pins), expected.ArtifactID, expected.SHA256, int64(len(raw)), s.Bucket).Scan(&outcome, &id, &token, &replay); err != nil {
			return err
		}
		var err error
		result, err = signingLineageReservation(outcome, id, token, replay, expected)
		return err
	})
	if err != nil {
		return artifactjson.Reservation{}, err
	}
	return result, nil
}
func (s SQLSigningLineageStore) ReadySigningLineageVersion(ctx context.Context, who auth.Identity, ready ReadySigningExport, expected v2input.Ref, raw []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.DB == nil {
		return "", ErrInvalid
	}
	pins, err := signingLineageStoragePins(who, ready, expected, raw, s.Bucket)
	if err != nil {
		return "", err
	}
	var version string
	err = tenant.WithTenant(ctx, s.DB, who, ready.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var saved *string
		if err := tx.QueryRow(ctx, `SELECT photo.v31_ready_signing_lineage_version($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9)`,
			ready.Tenant, who.UserID, who.SessionID, ready.ExportID, string(pins), expected.ArtifactID,
			expected.SHA256, int64(len(raw)), s.Bucket).Scan(&saved); err != nil {
			return err
		}
		if saved == nil || *saved == "" || *saved == "null" || len(*saved) > 1024 {
			return objectstore.ErrIntegrity
		}
		version = *saved
		return nil
	})
	if err != nil {
		return "", err
	}
	return version, nil
}
func (s SQLSigningLineageStore) CompleteSigningLineage(ctx context.Context, who auth.Identity, ready ReadySigningExport, expected v2input.Ref, raw []byte, reservation artifactjson.Reservation, object objectstore.Object) (v2input.Ref, error) {
	var zero v2input.Ref
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil {
		return zero, ErrInvalid
	}
	pins, err := signingLineageStoragePins(who, ready, expected, raw, s.Bucket)
	if err != nil {
		return zero, err
	}
	key, err := objectstore.DocumentKey(ready.Tenant, ready.Run, expected.ArtifactID)
	if err != nil || reservation.ArtifactID != expected.ArtifactID || reservation.Token == uuid.Nil || reservation.Ready != nil || object.Key != key || object.SHA256 != expected.SHA256 || object.Bytes != int64(len(raw)) || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 {
		return zero, objectstore.ErrIntegrity
	}
	err = tenant.WithTenant(ctx, s.DB, who, ready.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.complete_v31_signing_lineage($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,$10,$11)`, ready.Tenant, who.UserID, who.SessionID, ready.ExportID, string(pins), expected.ArtifactID, expected.SHA256, int64(len(raw)), s.Bucket, reservation.Token, object.VersionID).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return artifactjson.ErrStale
		}
		return nil
	})
	if err != nil {
		return zero, err
	}
	return expected, nil
}

var _ SigningLineageStore = SQLSigningLineageStore{}
