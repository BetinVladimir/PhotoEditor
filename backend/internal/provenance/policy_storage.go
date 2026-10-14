package provenance

import (
	"context"

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

type SQLSigningPolicyStore struct {
	DB     *pgxpool.Pool
	Bucket string
}

func (s SQLSigningPolicyStore) pins(who auth.Identity, ready ReadySigningExport, expected v2input.Ref, raw []byte) (SigningPolicy, []byte, error) {
	var zero SigningPolicy
	if who.UserID == uuid.Nil || who.SessionID == uuid.Nil || ready.Tenant == uuid.Nil || ready.Project == uuid.Nil || ready.ExportID == uuid.Nil || ready.Run == uuid.Nil || ready.Version == uuid.Nil || ready.Task == uuid.Nil || !ref(ready.Ref) || !ref(expected) || len(s.Bucket) < 3 || !text(s.Bucket, 63) {
		return zero, nil, ErrInvalid
	}
	if ready.Object.Key == "" || ready.Object.VersionID == "" || ready.Object.VersionID == "null" || len(ready.Object.VersionID) > 1024 || ready.Object.SHA256 != ready.Ref.SHA256 || ready.Object.Bytes < 1 || ready.Object.Bytes > 512<<20 || (ready.Object.ContentType != "image/jpeg" && ready.Object.ContentType != "image/png") {
		return zero, nil, ErrInvalid
	}
	var header SigningPolicy
	if decode(raw, &header, 1<<20) != nil {
		return zero, nil, ErrInvalid
	}
	policy, err := DecodeSigningPolicy(raw, expected, header.ID)
	if err != nil {
		return zero, nil, err
	}
	if expected.ArtifactID != uuid.NewSHA1(ready.Tenant, []byte("signing:policy:"+policy.ID.String()+":"+expected.SHA256)) {
		return zero, nil, ErrInvalid
	}
	pins, err := signingExportStoragePins(ready)
	return policy, pins, err
}
func (s SQLSigningPolicyStore) ReserveSigningPolicy(ctx context.Context, who auth.Identity, ready ReadySigningExport, expected v2input.Ref, raw []byte) (SigningPolicyReservation, error) {
	var result SigningPolicyReservation
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if s.DB == nil {
		return result, ErrInvalid
	}
	policy, pins, err := s.pins(who, ready, expected, raw)
	if err != nil {
		return result, err
	}
	err = tenant.WithTenant(ctx, s.DB, who, ready.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var outcome string
		var id, token, run *uuid.UUID
		var replay bool
		if err := tx.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready,storage_run FROM photo.reserve_v31_signing_policy($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,$10)`, ready.Tenant, who.UserID, who.SessionID, ready.ExportID, string(pins), policy.ID, expected.ArtifactID, expected.SHA256, int64(len(raw)), s.Bucket).Scan(&outcome, &id, &token, &replay, &run); err != nil {
			return err
		}
		reservation, err := signingLineageReservation(outcome, id, token, replay, expected)
		if err != nil {
			return err
		}
		if run == nil || *run == uuid.Nil {
			return objectstore.ErrIntegrity
		}
		result = SigningPolicyReservation{Reservation: reservation, StorageRun: *run}
		if reservation.Ready != nil {
			var version *string
			if err := tx.QueryRow(ctx, `SELECT photo.v31_ready_signing_policy_version($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,$10)`, ready.Tenant, who.UserID, who.SessionID, ready.ExportID, string(pins), policy.ID, expected.ArtifactID, expected.SHA256, int64(len(raw)), s.Bucket).Scan(&version); err != nil {
				return err
			}
			if version == nil || *version == "" || *version == "null" || len(*version) > 1024 {
				return objectstore.ErrIntegrity
			}
			result.ReadyVersion = *version
		}
		return nil
	})
	if err != nil {
		return SigningPolicyReservation{}, err
	}
	return result, nil
}
func (s SQLSigningPolicyStore) CompleteSigningPolicy(ctx context.Context, who auth.Identity, ready ReadySigningExport, expected v2input.Ref, raw []byte, saved SigningPolicyReservation, object objectstore.Object) (v2input.Ref, error) {
	var zero v2input.Ref
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil {
		return zero, ErrInvalid
	}
	policy, pins, err := s.pins(who, ready, expected, raw)
	if err != nil {
		return zero, err
	}
	key, err := objectstore.DocumentKey(ready.Tenant, saved.StorageRun, expected.ArtifactID)
	if err != nil || saved.StorageRun == uuid.Nil || saved.Reservation.ArtifactID != expected.ArtifactID || saved.Reservation.Token == uuid.Nil || saved.Reservation.Ready != nil || object.Key != key || object.SHA256 != expected.SHA256 || object.Bytes != int64(len(raw)) || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 {
		return zero, objectstore.ErrIntegrity
	}
	err = tenant.WithTenant(ctx, s.DB, who, ready.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.complete_v31_signing_policy($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,$10,$11,$12,$13)`, ready.Tenant, who.UserID, who.SessionID, ready.ExportID, string(pins), policy.ID, expected.ArtifactID, expected.SHA256, int64(len(raw)), s.Bucket, saved.Reservation.Token, saved.StorageRun, object.VersionID).Scan(&accepted); err != nil {
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

var _ SigningPolicyStore = SQLSigningPolicyStore{}
