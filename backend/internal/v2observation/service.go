package v2observation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2resource"
)

type ObjectReader interface {
	Open(context.Context, objectstore.Object) (io.ReadCloser, error)
}

type Service struct {
	DB      *pgxpool.Pool
	Objects ObjectReader
	Bucket  string
}

type ConfirmRequest struct {
	ExpectedRevision  string      `json:"expected_revision"`
	AssetVersionID    uuid.UUID   `json:"asset_version_id"`
	FaceObservationID uuid.UUID   `json:"face_observation_id"`
	Evidence          ArtifactRef `json:"evidence"`
	ConsentID         uuid.UUID   `json:"consent_id"`
	ConsentEpoch      string      `json:"consent_epoch"`
}

func positiveRevision(raw string) (int64, error) {
	if len(raw) == 0 || len(raw) > 19 || raw[0] == '0' {
		return 0, ErrEvidence
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 || n > 9007199254740991 {
		return 0, ErrEvidence
	}
	return n, nil
}

func readPinned(ctx context.Context, reader ObjectReader, object objectstore.Object) ([]byte, error) {
	if object.Bytes < 1 || object.Bytes > 1_048_576 || object.ContentType != "application/json" {
		return nil, ErrEvidence
	}
	body, err := reader.Open(ctx, object)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	raw, err := io.ReadAll(io.LimitReader(body, 1_048_577))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) != object.Bytes {
		return nil, ErrEvidence
	}
	return raw, nil
}

func artifactObject(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, ref ArtifactRef,
	role, bucket string) (objectstore.Object, error) {
	var object objectstore.Object
	var actualBucket string
	err := tx.QueryRow(ctx, `SELECT bucket,object_key,object_version,sha256,bytes,media_type
	 FROM photo.artifacts WHERE tenant_id=$1 AND id=$2 AND sha256=$3
	  AND output_role=$4 AND state='ready' AND verified_at IS NOT NULL
	  AND object_version IS NOT NULL
	  AND (expires_at IS NULL OR expires_at>clock_timestamp())`,
		tenantID, ref.ArtifactID, ref.SHA256, role).Scan(&actualBucket, &object.Key,
		&object.VersionID, &object.SHA256, &object.Bytes, &object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) || actualBucket != bucket {
		return object, domain.NewError("EVIDENCE_INVALID", 409)
	}
	return object, err
}

func sqlError(err error) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	switch pg.Code {
	case "40001", "23505", "23514":
		return domain.NewError("OBSERVATION_CONFLICT", 409)
	case "42501":
		return domain.NewError("FORBIDDEN", 403)
	case "P0002":
		return domain.NewError("NOT_FOUND", 404)
	case "22023":
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	return err
}

// Confirm binds an observation to a person without accepting a client face_id.
// SQL rechecks live consent, CAS, provenance and artifact state before commit.
func (s Service) Confirm(ctx context.Context, who auth.Identity, tenantID,
	personID uuid.UUID, key string, req ConfirmRequest) (idempotency.Response, error) {
	var output idempotency.Response
	if s.DB == nil || s.Objects == nil || s.Bucket == "" {
		return output, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	revision, revErr := positiveRevision(req.ExpectedRevision)
	epoch, epochErr := positiveRevision(req.ConsentEpoch)
	if revErr != nil || epochErr != nil || personID == uuid.Nil ||
		req.AssetVersionID == uuid.Nil || req.FaceObservationID == uuid.Nil ||
		req.Evidence.ArtifactID == uuid.Nil || !shaPattern.MatchString(req.Evidence.SHA256) ||
		req.ConsentID == uuid.Nil {
		return output, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		var projectID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT project_id FROM photo.project_persons
		 WHERE tenant_id=$1 AND id=$2`, tenantID, personID).Scan(&projectID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.NewError("NOT_FOUND", 404)
			}
			return err
		}
		if _, err := project.Authorize(ctx, tx, tenantID, projectID, who.UserID,
			role, domain.Editor, false); err != nil {
			return err
		}
		var useErr error
		output, useErr = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tenantID,
			Principal: who.UserID, PrincipalType: "user",
			Route: "V22.ConfirmObservation:" + personID.String(), Key: key},
			req, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
				body, e := s.confirmTx(ctx, tx, who, tenantID, projectID, personID,
					revision, epoch, req)
				if e != nil {
					return idempotency.Response{}, e
				}
				id, e := v2resource.ID(body)
				if e != nil {
					return idempotency.Response{}, e
				}
				body, e = v2resource.Load(ctx, tx, tenantID, id, "person_observations")
				return idempotency.Response{Status: 201, Body: body, ResourceID: &id}, e
			})
		return useErr
	})
	return output, err
}

func (s Service) confirmTx(ctx context.Context, tx pgx.Tx, who auth.Identity,
	tenantID, projectID, personID uuid.UUID, revision, epoch int64,
	req ConfirmRequest) (json.RawMessage, error) {
	var output json.RawMessage
	evidenceObject, err := artifactObject(ctx, tx, tenantID, req.Evidence, "faces", s.Bucket)
	if err != nil {
		return nil, err
	}
	evidenceRaw, err := readPinned(ctx, s.Objects, evidenceObject)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Binding Binding `json:"binding"`
	}
	if json.Unmarshal(evidenceRaw, &envelope) != nil ||
		envelope.Binding.AssetVersionID != req.AssetVersionID {
		return nil, domain.NewError("EVIDENCE_INVALID", 409)
	}
	faceID, err := ResolveFaceID(evidenceRaw, req.Evidence.SHA256,
		envelope.Binding, req.FaceObservationID)
	if err != nil {
		return nil, domain.NewError("EVIDENCE_INVALID", 409)
	}
	var contentSHA string
	err = tx.QueryRow(ctx, `SELECT v.content_hash FROM photo.asset_versions v
		 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
		 WHERE v.tenant_id=$1 AND v.id=$2 AND a.project_id=$3 AND a.deleted_at IS NULL`,
		tenantID, req.AssetVersionID, projectID).Scan(&contentSHA)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		return nil, err
	}
	sourceObject, err := artifactObject(ctx, tx, tenantID,
		envelope.Binding.Source, "working_image", s.Bucket)
	if err != nil {
		return nil, err
	}
	sourceRaw, err := readPinned(ctx, s.Objects, sourceObject)
	if err != nil {
		return nil, err
	}
	if VerifyWorkingImageBinding(sourceRaw, envelope.Binding, contentSHA) != nil {
		return nil, domain.NewError("EVIDENCE_INVALID", 409)
	}
	err = sqlError(tx.QueryRow(ctx, `SELECT photo.confirm_v22_observation(
		 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, tenantID, projectID,
		personID, revision, req.AssetVersionID, envelope.Binding.Source.ArtifactID,
		envelope.Binding.Source.SHA256, faceID, req.Evidence.ArtifactID,
		req.Evidence.SHA256, req.ConsentID, epoch, who.UserID, who.SessionID).Scan(&output))
	return output, err
}
