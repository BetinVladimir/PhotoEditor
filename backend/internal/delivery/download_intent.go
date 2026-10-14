package delivery

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/tenant"
)

type DownloadIntentRequest struct {
	Kind string `json:"kind"`
	Ordinal *int `json:"ordinal,omitempty"`
	PresetID *uuid.UUID `json:"preset_id,omitempty"`
}

type DownloadIntent struct {
	Token string `json:"intent_token"`
	ExpiresAt time.Time `json:"expires_at"`
	Replayed bool `json:"-"`
}

type downloadIntentRecord struct {
	ID uuid.UUID `json:"intent_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type DownloadIntentService struct {
	DB *pgxpool.Pool
	Pepper []byte
	Bucket string
	Now func() time.Time
}

func (s DownloadIntentService) Create(ctx context.Context, who auth.Identity,
	tenantID, packageID uuid.UUID, key string,
	request DownloadIntentRequest) (DownloadIntent, error) {
	if err := ctx.Err(); err != nil { return DownloadIntent{}, err }
	if packageID == uuid.Nil || tenantID == uuid.Nil || who.UserID == uuid.Nil ||
		who.SessionID == uuid.Nil || len(s.Pepper) < 32 || s.DB == nil ||
		len(s.Bucket) < 3 || len(s.Bucket) > 63 || !idempotency.ValidKey(key) {
		return DownloadIntent{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if request.Kind == "manifest" {
		if request.Ordinal != nil || request.PresetID != nil {
			return DownloadIntent{}, domain.NewError("VALIDATION_FAILED", 422)
		}
	} else if request.Kind == "slot" {
		if request.Ordinal == nil || *request.Ordinal < 0 || *request.Ordinal > 4999 ||
			request.PresetID == nil || *request.PresetID == uuid.Nil {
			return DownloadIntent{}, domain.NewError("VALIDATION_FAILED", 422)
		}
	} else { return DownloadIntent{}, domain.NewError("VALIDATION_FAILED", 422) }
	now := time.Now().UTC()
	if s.Now != nil { now = s.Now().UTC() }
	expires := now.Add(45 * time.Second)
	var saved idempotency.Response
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Viewer,
		func(tx pgx.Tx, _ domain.Role) error {
			var err error
			saved, err = idempotency.Use(ctx, tx, idempotency.Scope{
				Tenant: tenantID, Principal: who.UserID, PrincipalType: "user",
				Route: "Delivery.Download.Intent", Key: key,
			}, struct {
				Package uuid.UUID `json:"package_id"`
				Session uuid.UUID `json:"session_id"`
				Request DownloadIntentRequest `json:"request"`
			}{packageID, who.SessionID, request}, now.Add(30*24*time.Hour), func() (idempotency.Response, error) {
				id := uuid.New()
				token := base64.RawURLEncoding.EncodeToString(
					auth.MAC(s.Pepper, "portal-download-bearer", id.String()))
				var created bool
				err := tx.QueryRow(ctx, `SELECT photo.create_v30_delivery_download_intent(
				 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, tenantID, who.UserID,
					who.SessionID, packageID, request.Kind, request.Ordinal,
					request.PresetID, s.Bucket, id,
					auth.MAC(s.Pepper, "portal-download-intent", token), expires).Scan(&created)
				if err != nil { return idempotency.Response{}, err }
				if !created { return idempotency.Response{}, domain.NewError("DOWNLOAD_NOT_READY", 409) }
				body, err := canonical.Marshal(downloadIntentRecord{id, expires})
				return idempotency.Response{Status: 201, Body: body, ResourceID: &id}, err
			})
			if err != nil { return err }
			var record downloadIntentRecord
			if json.Unmarshal(saved.Body, &record) != nil || record.ID == uuid.Nil ||
				!record.ExpiresAt.After(now) { return domain.NewError("DOWNLOAD_INTENT_INVALID", 404) }
			var artifact uuid.UUID
			var sha, version string
			err = tx.QueryRow(ctx, `SELECT artifact_id,sha256,object_version FROM photo.v30_delivery_download_target(
			 $1,$2,$3,$4,$5,$6,$7,$8)`, tenantID, who.UserID, who.SessionID,
				packageID, request.Kind, request.Ordinal, request.PresetID, s.Bucket).Scan(&artifact,&sha,&version)
			if err != nil || artifact == uuid.Nil { return domain.NewError("DOWNLOAD_REVOKED", 409) }
			var pinnedArtifact uuid.UUID
			var pinnedSHA, pinnedVersion string
			var consumed *time.Time
			err = tx.QueryRow(ctx, `SELECT artifact_id,delivery_sha,delivery_object_version,consumed_at
			 FROM photo.download_intents WHERE tenant_id=$1 AND id=$2
			 AND principal_type='user' AND principal_id=$3 AND delivery_session_id=$4
			 AND delivery_package_id=$5 AND delivery_kind=$6
			 AND delivery_ordinal IS NOT DISTINCT FROM $7
			 AND delivery_preset_id IS NOT DISTINCT FROM $8 AND intent_kind='delivery'
			 FOR SHARE`,tenantID,record.ID,who.UserID,who.SessionID,
				packageID,request.Kind,request.Ordinal,request.PresetID).
				Scan(&pinnedArtifact,&pinnedSHA,&pinnedVersion,&consumed)
			if err != nil || consumed != nil || pinnedArtifact != artifact ||
				pinnedSHA != sha || pinnedVersion != version {
				return domain.NewError("DOWNLOAD_INTENT_INVALID", 409)
			}
			return ctx.Err()
		})
	if err != nil { return DownloadIntent{}, err }
	var record downloadIntentRecord
	if json.Unmarshal(saved.Body, &record) != nil || record.ID == uuid.Nil {
		return DownloadIntent{}, domain.NewError("DOWNLOAD_INTENT_INVALID", 404)
	}
	token := base64.RawURLEncoding.EncodeToString(
		auth.MAC(s.Pepper, "portal-download-bearer", record.ID.String()))
	return DownloadIntent{Token: token, ExpiresAt: record.ExpiresAt,
		Replayed: saved.Replayed}, nil
}
