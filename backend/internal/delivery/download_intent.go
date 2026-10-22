package delivery

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
 "errors"
	"io"
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
	Kind     string     `json:"kind"`
	Ordinal  *int       `json:"ordinal,omitempty"`
	PresetID *uuid.UUID `json:"preset_id,omitempty"`
}

// The manifest and slot variants are closed wire shapes. In particular, null
// must not disguise a slot field as an omitted manifest field.
func (request *DownloadIntentRequest) UnmarshalJSON(body []byte) error {
	fields, ok := closedJSONObject(body)
	if !ok { return domain.NewError("VALIDATION_FAILED", 422) }
	var kind string
	if json.Unmarshal(fields["kind"], &kind) != nil { return domain.NewError("VALIDATION_FAILED", 422) }
	if kind == "manifest" {
		if len(fields) != 1 { return domain.NewError("VALIDATION_FAILED", 422) }
	} else if kind == "slot" {
		if len(fields) != 3 || fields["ordinal"] == nil || fields["preset_id"] == nil ||
			bytes.Equal(bytes.TrimSpace(fields["ordinal"]), []byte("null")) ||
			bytes.Equal(bytes.TrimSpace(fields["preset_id"]), []byte("null")) {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
	} else { return domain.NewError("VALIDATION_FAILED", 422) }
	type wire DownloadIntentRequest
	var decoded wire
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&decoded) != nil { return domain.NewError("VALIDATION_FAILED", 422) }
	if err := DownloadIntentRequest(decoded).Validate(); err != nil { return err }
	*request = DownloadIntentRequest(decoded)
	return nil
}

// Validate distinguishes a complete manifest request from an exact output slot.
func (request DownloadIntentRequest) Validate() error {
	if request.Kind == "manifest" && request.Ordinal == nil && request.PresetID == nil {
		return nil
	}
	if request.Kind == "slot" && request.Ordinal != nil && *request.Ordinal >= 0 && *request.Ordinal <= 4999 && request.PresetID != nil && *request.PresetID != uuid.Nil {
		return nil
	}
	return domain.NewError("VALIDATION_FAILED", 422)
}

type DownloadIntent struct {
	Token     string    `json:"intent_token"`
	ExpiresAt time.Time `json:"expires_at"`
	Replayed  bool      `json:"-"`
}

type downloadIntentRecord struct {
	ID        uuid.UUID `json:"intent_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

func validDownloadIntentReceipt(saved idempotency.Response, now time.Time) (downloadIntentRecord, bool) {
	var record downloadIntentRecord
	if saved.Status != 201 || saved.ResourceID == nil || len(saved.Body) == 0 || len(saved.Body) > 512 {
		return downloadIntentRecord{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(saved.Body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&record) != nil || decoder.Decode(new(any)) != io.EOF || record.ID == uuid.Nil ||
		*saved.ResourceID != record.ID || !record.ExpiresAt.After(now) || record.ExpiresAt.After(now.Add(time.Minute)) {
		return downloadIntentRecord{}, false
	}
	return record, true
}

// A missing authorized row is a domain refusal. Infrastructure failures must
// retain their retryable identity instead of looking like a definitive revoke.
func downloadIntentLookupError(err error, missingCode string) error {
 if err == nil { return nil }
 if errors.Is(err, pgx.ErrNoRows) { return domain.NewError(missingCode, 409) }
 return err
}

type DownloadIntentService struct {
	DB     *pgxpool.Pool
	Pepper []byte
	Bucket string
	Now    func() time.Time
}

func (s DownloadIntentService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s DownloadIntentService) Create(ctx context.Context, who auth.Identity,
	tenantID, packageID uuid.UUID, key string,
	request DownloadIntentRequest) (DownloadIntent, error) {
	if err := ctx.Err(); err != nil {
		return DownloadIntent{}, err
	}
	if packageID == uuid.Nil || tenantID == uuid.Nil || who.UserID == uuid.Nil ||
		who.SessionID == uuid.Nil || len(s.Pepper) < 32 || s.DB == nil ||
		len(s.Bucket) < 3 || len(s.Bucket) > 63 || !idempotency.ValidKey(key) {
		return DownloadIntent{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if err := request.Validate(); err != nil {
		return DownloadIntent{}, err
	}
	now := s.now()
	expires := now.Add(45 * time.Second)
	var saved idempotency.Response
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor,
		func(tx pgx.Tx, _ domain.Role) error {
			var err error
			saved, err = idempotency.Use(ctx, tx, idempotency.Scope{
				Tenant: tenantID, Principal: who.UserID, PrincipalType: "user",
				Route: "Delivery.Download.Intent", Key: key,
			}, struct {
				Package uuid.UUID             `json:"package_id"`
				Session uuid.UUID             `json:"session_id"`
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
				if err != nil {
					return idempotency.Response{}, err
				}
				if !created {
					return idempotency.Response{}, domain.NewError("DOWNLOAD_NOT_READY", 409)
				}
				body, err := canonical.Marshal(downloadIntentRecord{id, expires})
				return idempotency.Response{Status: 201, Body: body, ResourceID: &id}, err
			})
			if err != nil {
				return err
			}
			record, valid := validDownloadIntentReceipt(saved, s.now())
			if !valid {
				return domain.NewError("DOWNLOAD_INTENT_INVALID", 404)
			}
			var artifact uuid.UUID
			var sha, version string
			err = tx.QueryRow(ctx, `SELECT artifact_id,sha256,object_version FROM photo.v30_delivery_download_target(
			 $1,$2,$3,$4,$5,$6,$7,$8)`, tenantID, who.UserID, who.SessionID,
				packageID, request.Kind, request.Ordinal, request.PresetID, s.Bucket).Scan(&artifact, &sha, &version)
            if lookupErr := downloadIntentLookupError(err, "DOWNLOAD_REVOKED"); lookupErr != nil { return lookupErr }
			if artifact == uuid.Nil {
				return domain.NewError("DOWNLOAD_REVOKED", 409)
			}
			var pinnedArtifact uuid.UUID
			var pinnedSHA, pinnedVersion string
			var consumed *time.Time
			err = tx.QueryRow(ctx, `SELECT artifact_id,delivery_sha,delivery_object_version,consumed_at
			 FROM photo.download_intents WHERE tenant_id=$1 AND id=$2
			 AND principal_type='user' AND principal_id=$3 AND delivery_session_id=$4
			 AND delivery_package_id=$5 AND delivery_kind=$6
			 AND delivery_ordinal IS NOT DISTINCT FROM $7
			 AND delivery_preset_id IS NOT DISTINCT FROM $8 AND intent_kind='delivery'
		 AND expires_at=$9 AND expires_at>clock_timestamp()
			 FOR SHARE`, tenantID, record.ID, who.UserID, who.SessionID,
				packageID, request.Kind, request.Ordinal, request.PresetID, record.ExpiresAt).
				Scan(&pinnedArtifact, &pinnedSHA, &pinnedVersion, &consumed)
            if lookupErr := downloadIntentLookupError(err, "DOWNLOAD_INTENT_INVALID"); lookupErr != nil { return lookupErr }
			if consumed != nil || pinnedArtifact != artifact ||
				pinnedSHA != sha || pinnedVersion != version {
				return domain.NewError("DOWNLOAD_INTENT_INVALID", 409)
			}
			return ctx.Err()
		})
	if err != nil {
		return DownloadIntent{}, err
	}
	if err := ctx.Err(); err != nil {
		return DownloadIntent{}, err
	}
	record, valid := validDownloadIntentReceipt(saved, s.now())
	if !valid {
		return DownloadIntent{}, domain.NewError("DOWNLOAD_INTENT_INVALID", 404)
	}
	token := base64.RawURLEncoding.EncodeToString(
		auth.MAC(s.Pepper, "portal-download-bearer", record.ID.String()))
	return DownloadIntent{Token: token, ExpiresAt: record.ExpiresAt,
		Replayed: saved.Replayed}, nil
}
