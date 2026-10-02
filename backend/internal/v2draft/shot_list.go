package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"photoeditor/backend/internal/idempotency"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type ShotListView struct {
	SessionID    uuid.UUID                        `json:"session_id"`
	Revision     string                           `json:"revision"`
	Document     *v2input.Ref                     `json:"document"`
	Requirements []v2input.CaptureShotRequirement `json:"requirements"`
}

// GetShotList returns an empty list when no document exists. Existing documents
// are read by pinned object version, then checked against live identity consent.
func (s Service) GetShotList(ctx context.Context, who auth.Identity, tenantID, sessionID uuid.UUID) (ShotListView, error) {
	view := ShotListView{SessionID: sessionID, Requirements: []v2input.CaptureShotRequirement{}}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" {
		return view, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	var projectID uuid.UUID
	var revision int64
	var obj objectstore.Object
	var bucket string
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var documentID *uuid.UUID
		e := tx.QueryRow(ctx, `SELECT project_id,revision,document_id FROM photo.read_v24_shot_snapshot($1,$2,$3,$4)`, tenantID, sessionID, who.UserID, who.SessionID).Scan(&projectID, &revision, &documentID)
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if e != nil {
			return e
		}
		if _, e = project.Authorize(ctx, tx, tenantID, projectID, who.UserID, role, domain.Viewer, false); e != nil {
			return e
		}
		if documentID == nil {
			return nil
		}
		ref := v2input.Ref{}
		e = tx.QueryRow(ctx, `SELECT d.artifact_id,d.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
		 FROM photo.v2_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.sha256=d.sha256
		 WHERE d.tenant_id=$1 AND d.project_id=$2 AND d.id=$3 AND d.document_type='ShotList'
		 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
		 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())`, tenantID, projectID, documentID).Scan(&ref.ArtifactID, &ref.SHA256, &obj.Bytes, &bucket, &obj.Key, &obj.VersionID, &obj.ContentType)
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("DOCUMENT_INVALID", 422)
		}
		if e != nil {
			return e
		}
		view.Document = &ref
		obj.SHA256 = ref.SHA256
		return nil
	})
	if err != nil {
		return view, draftMutationError(err)
	}
	view.Revision = strconv.FormatInt(revision, 10)
	if view.Document == nil {
		return view, nil
	}
	if bucket != s.Bucket {
		return view, domain.NewError("DOCUMENT_INVALID", 422)
	}
	raw, err := readJSONArtifact(ctx, s.Objects, obj, *view.Document)
	if err != nil {
		return view, draftMutationError(err)
	}
	shots, err := s.validateShotRead(ctx, who, tenantID, projectID, raw)
	if err != nil {
		return ShotListView{}, err
	}
	view.Requirements = shots.Requirements
	return view, nil
}

type ShotListReplaceRequest struct {
	ExpectedRevision string                           `json:"expected_revision"`
	Requirements     []v2input.CaptureShotRequirement `json:"requirements"`
}

// ReplaceShotList publishes before an idempotent, session-revision CAS. Failed
// commits leave a temporary publication handled by the existing draft janitor.
func (s Service) ReplaceShotList(ctx context.Context, who auth.Identity, tenantID, sessionID uuid.UUID,
	key string, req ShotListReplaceRequest) (idempotency.Response, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" {
		return idempotency.Response{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if sessionID == uuid.Nil {
		return idempotency.Response{}, domain.NewError("NOT_FOUND", 404)
	}
	if !idempotency.ValidKey(key) {
		return idempotency.Response{}, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	expected, err := replaceRevision(req.ExpectedRevision)
	if err != nil || expected < 1 {
		return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	shots := v2input.CaptureShotList{SchemaVersion: 1, Requirements: req.Requirements}
	body, err := v2input.CompileCaptureShotList(shots)
	if err != nil {
		return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	documentID := stableDraftID(tenantID, who.UserID, sessionID, key, "shot-list")
	ref, err := (Publisher{DB: s.DB, Objects: s.Objects, Bucket: s.Bucket}).PublishShotList(ctx, who, tenantID, sessionID, documentID, shots)
	if err != nil {
		return idempotency.Response{}, draftMutationError(err)
	}
	people := []uuid.UUID{}
	seen := map[uuid.UUID]bool{}
	for _, r := range req.Requirements {
		for _, person := range r.PersonIDs {
			if !seen[person] {
				people = append(people, person)
				seen[person] = true
			}
		}
	}
	var out idempotency.Response
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		var projectID uuid.UUID
		e := tx.QueryRow(ctx, `SELECT project_id FROM photo.shoot_sessions WHERE tenant_id=$1 AND id=$2`, tenantID, sessionID).Scan(&projectID)
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if e != nil {
			return e
		}
		if _, e = project.Authorize(ctx, tx, tenantID, projectID, who.UserID, role, domain.Editor, false); e != nil {
			return e
		}
		var live bool
		e = tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT FROM unnest($3::uuid[]) required(id) WHERE NOT EXISTS(
   SELECT FROM photo.project_persons p JOIN photo.consent_grants g ON g.tenant_id=p.tenant_id AND g.project_id=p.project_id AND g.person_id=p.id
   WHERE p.tenant_id=$1 AND p.project_id=$2 AND p.id=required.id AND p.state='active' AND g.purpose='identity_matching' AND g.status='granted'))`, tenantID, projectID, people).Scan(&live)
		if e != nil {
			return e
		}
		if !live {
			return domain.NewError("CONSENT_REVOKED", 409)
		}
		out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tenantID, Principal: who.UserID, PrincipalType: "user", Route: "ShotList.Replace." + sessionID.String(), Key: key}, req, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
			var result json.RawMessage
			e := tx.QueryRow(ctx, `SELECT photo.replace_v24_shot_list($1,$2,$3,$4,$5,$6,$7,$8,$9)`, tenantID, sessionID, expected, documentID, ref.ArtifactID, ref.SHA256, string(body), who.UserID, who.SessionID).Scan(&result)
			return idempotency.Response{Status: 200, Body: result, ResourceID: &sessionID}, e
		})
		return e
	})
	return out, draftMutationError(err)
}

// validateShotRead validates immutable ShotList bytes and repeats authorization
// after storage I/O, including current consent for every referenced person.
func (s Service) validateShotRead(ctx context.Context, who auth.Identity, tenantID, projectID uuid.UUID, raw []byte) (v2input.CaptureShotList, error) {
	var shots v2input.CaptureShotList
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&shots) != nil {
		return v2input.CaptureShotList{}, domain.NewError("DOCUMENT_INVALID", 422)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return v2input.CaptureShotList{}, domain.NewError("DOCUMENT_INVALID", 422)
	}
	if _, err := v2input.CompileCaptureShotList(shots); err != nil {
		return v2input.CaptureShotList{}, domain.NewError("DOCUMENT_INVALID", 422)
	}
	people := []uuid.UUID{}
	seen := map[uuid.UUID]bool{}
	for _, requirement := range shots.Requirements {
		for _, person := range requirement.PersonIDs {
			if !seen[person] {
				people = append(people, person)
				seen[person] = true
			}
		}
	}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		if _, e := project.Authorize(ctx, tx, tenantID, projectID, who.UserID, role, domain.Viewer, false); e != nil {
			return e
		}
		var live bool
		e := tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT FROM unnest($3::uuid[]) required(id) WHERE NOT EXISTS(
		 SELECT FROM photo.project_persons p JOIN photo.consent_grants g ON g.tenant_id=p.tenant_id AND g.project_id=p.project_id AND g.person_id=p.id
		 WHERE p.tenant_id=$1 AND p.project_id=$2 AND p.id=required.id AND p.state='active' AND g.purpose='identity_matching' AND g.status='granted'))`, tenantID, projectID, people).Scan(&live)
		if e != nil {
			return e
		}
		if !live {
			return domain.NewError("CONSENT_REVOKED", 409)
		}
		return nil
	})
	if err != nil {
		return v2input.CaptureShotList{}, draftMutationError(err)
	}
	return shots, nil
}
