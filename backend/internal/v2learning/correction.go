package v2learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// CorrectionCreate contains only client-owned fields. The project, approver,
// event time and changed paths are derived from immutable server records.
type CorrectionCreate struct {
	ClientEventID          uuid.UUID                        `json:"client_event_id"`
	SessionID              uuid.UUID                        `json:"session_id"`
	AssetVersionID         uuid.UUID                        `json:"asset_version_id"`
	BaseDocumentID         uuid.UUID                        `json:"base_document_id"`
	ApprovedDocumentID     uuid.UUID                        `json:"approved_document_id"`
	Reason                 string                           `json:"reason"`
	Learn                  bool                             `json:"learn"`
	PersonID               *uuid.UUID                       `json:"person_id"`
	SceneGroupID           *uuid.UUID                       `json:"scene_group_id"`
	ConsentID              *uuid.UUID                       `json:"consent_id"`
	ConsentEpoch           *string                          `json:"consent_epoch"`
	PortraitDetailProducer *PortraitDetailProducerSelection `json:"portrait_detail_producer,omitempty"`
}

// PortraitDetailProducerSelection is selected from the completed Apply graph
// by the editor; SQL independently reselects and binds it in this transaction.
type PortraitDetailProducerSelection struct {
	RunID     uuid.UUID `json:"run_id"`
	TaskID    uuid.UUID `json:"task_id"`
	ResultID  uuid.UUID `json:"result_id"`
	AttemptID uuid.UUID `json:"attempt_id"`
}

func (r *CorrectionCreate) UnmarshalJSON(body []byte) error {
	fields, err := closedLearningObject(body)
	if err != nil {
		return err
	}
	if len(fields) != 11 && len(fields) != 12 {
		return fmt.Errorf("CorrectionCreate requires 11 or 12 exact fields")
	}
	for _, key := range []string{"client_event_id", "session_id",
		"asset_version_id", "base_document_id", "approved_document_id",
		"reason", "learn", "person_id", "scene_group_id", "consent_id",
		"consent_epoch"} {
		if _, ok := fields[key]; !ok {
			return fmt.Errorf("CorrectionCreate missing %s", key)
		}
	}
	if string(fields["learn"]) != "true" && string(fields["learn"]) != "false" {
		return fmt.Errorf("CorrectionCreate.learn must be boolean")
	}
	if len(fields) == 12 && (fields["portrait_detail_producer"] == nil ||
		string(fields["portrait_detail_producer"]) == "null") {
		return fmt.Errorf("CorrectionCreate has unknown or null producer")
	}
	if raw := fields["portrait_detail_producer"]; raw != nil {
		producerFields, err := closedLearningObject(raw)
		if err != nil || len(producerFields) != 4 {
			return fmt.Errorf("CorrectionCreate has invalid producer")
		}
		for _, key := range []string{"run_id", "task_id", "result_id", "attempt_id"} {
			if producerFields[key] == nil {
				return fmt.Errorf("CorrectionCreate producer missing %s", key)
			}
		}
	}
	type wire CorrectionCreate
	var decoded wire
	if err := json.Unmarshal(body, &decoded); err != nil {
		return err
	}
	*r = CorrectionCreate(decoded)
	return nil
}

type CorrectionService struct {
	DB     *pgxpool.Pool
	Drafts v2draft.Service
}

func validCorrection(req CorrectionCreate) bool {
	if req.ClientEventID == uuid.Nil || req.SessionID == uuid.Nil ||
		req.AssetVersionID == uuid.Nil || req.BaseDocumentID == uuid.Nil ||
		req.ApprovedDocumentID == uuid.Nil ||
		req.BaseDocumentID == req.ApprovedDocumentID ||
		(req.PersonID != nil && *req.PersonID == uuid.Nil) ||
		(req.SceneGroupID != nil && *req.SceneGroupID == uuid.Nil) {
		return false
	}
	switch req.Reason {
	case "exposure", "color", "crop", "retouch", "selection", "other":
	default:
		return false
	}
	if req.Learn != (req.ConsentID != nil && req.ConsentEpoch != nil) ||
		(req.ConsentID == nil) != (req.ConsentEpoch == nil) {
		return false
	}
	if req.ConsentID != nil && *req.ConsentID == uuid.Nil {
		return false
	}
	if req.ConsentEpoch != nil {
		value := *req.ConsentEpoch
		if value == "" || len(value) > 19 || value[0] == '0' {
			return false
		}
		for _, digit := range value {
			if digit < '0' || digit > '9' {
				return false
			}
		}
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return false
		}
	}
	if producer := req.PortraitDetailProducer; producer != nil &&
		(!req.Learn || req.Reason != "retouch" ||
			producer.RunID == uuid.Nil || producer.TaskID == uuid.Nil ||
			producer.ResultID == uuid.Nil || producer.AttemptID == uuid.Nil) {
		return false
	}
	return true
}

func correctionError(err error) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	switch pg.Code {
	case "22023":
		return domain.NewError("VALIDATION_FAILED", 422)
	case "23514", "40001", "23505":
		return domain.NewError("REVISION_CONFLICT", 409)
	case "42501":
		return domain.NewError("FORBIDDEN", 403)
	case "42P01", "42883":
		return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	return err
}

// RecordCorrection verifies both artifact bodies before asking the database
// to atomically re-check provenance, consent and replay identity.
func (s CorrectionService) RecordCorrection(ctx context.Context, who auth.Identity,
	tenantID uuid.UUID, key string, req CorrectionCreate) (idempotency.Response, error) {
	var out idempotency.Response
	if s.DB == nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if !validCorrection(req) {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	if !idempotency.ValidKey(key) {
		return out, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	if replay, found, err := s.readCorrectionReplay(ctx,who,tenantID,key,req); err != nil || found { return replay,err }
	if s.Drafts.Objects == nil || s.Drafts.Bucket == "" { return out,domain.NewError("DEPENDENCY_UNAVAILABLE",503) }
	base, err := s.Drafts.GetDocument(ctx, who, tenantID, req.BaseDocumentID)
	if err != nil {
		return out, err
	}
	approved, err := s.Drafts.GetDocument(ctx, who, tenantID, req.ApprovedDocumentID)
	if err != nil {
		return out, err
	}
	var source struct {
		Binding v2input.SessionVersionBinding `json:"source"`
	}
	if json.Unmarshal(approved, &source) != nil ||
		source.Binding.AssetVersionID != req.AssetVersionID {
		return out, domain.NewError("DOCUMENT_INVALID", 422)
	}
	paths, err := v2input.DiffCorrectionParameters(base, approved, source.Binding)
	if err != nil {
		return out, domain.NewError("DOCUMENT_INVALID", 422)
	}
	sqlRequest := req
	sqlRequest.PortraitDetailProducer = nil
	requestBody, err := json.Marshal(sqlRequest)
	if err != nil {
		return out, err
	}
	pathsBody, err := json.Marshal(paths)
	if err != nil {
		return out, err
	}
	baseDigest, approvedDigest := sha256.Sum256(base), sha256.Sum256(approved)
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor,
		func(tx pgx.Tx, role domain.Role) error {
			// Persisted shoot identity is sufficient for replay authorization;
			// no consumed/expired document needs to be reopened for this gate.
			var projectID uuid.UUID
			e := tx.QueryRow(ctx, `SELECT project_id FROM photo.shoot_sessions WHERE tenant_id=$1 AND id=$2`, tenantID, req.SessionID).Scan(&projectID)
			if errors.Is(e, pgx.ErrNoRows) { return domain.NewError("NOT_FOUND",404) }
			if e != nil { return e }
			if _, e = project.Authorize(ctx,tx,tenantID,projectID,who.UserID,role,domain.Editor,false); e != nil { return e }
			out, e = idempotency.Use(ctx, tx, idempotency.Scope{
				Tenant: tenantID, Principal: who.UserID, PrincipalType: "user",
				Route: "V23.RecordCorrection", Key: key,
			}, req, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
				var body json.RawMessage
				e := tx.QueryRow(ctx, `SELECT photo.record_v2_correction(
				 $1,$2,$3,$4::jsonb,$5::jsonb,$6,$7)`, tenantID,
					who.UserID, who.SessionID, requestBody, pathsBody,
					hex.EncodeToString(baseDigest[:]),
					hex.EncodeToString(approvedDigest[:])).Scan(&body)
				if e != nil {
					return idempotency.Response{}, correctionError(e)
				}
				var event struct {
					ID        uuid.UUID `json:"id"`
					ProjectID uuid.UUID `json:"project_id"`
				}
				if e = json.Unmarshal(body, &event); e != nil || event.ID == uuid.Nil {
					return idempotency.Response{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
				}
				if producer := req.PortraitDetailProducer; producer != nil {
					if event.ProjectID == uuid.Nil {
						return idempotency.Response{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
					}
					var linked bool
					e = tx.QueryRow(ctx, `SELECT photo.bind_v23_portrait_detail_event(
					 $1,$2,$3,$4,$5,$6,$7,$8,$9)`, tenantID, event.ProjectID,
						who.UserID, who.SessionID, event.ID, producer.RunID, producer.TaskID,
						producer.ResultID, producer.AttemptID).Scan(&linked)
					if e != nil {
						return idempotency.Response{}, correctionError(e)
					}
					if !linked {
						return idempotency.Response{},
							domain.NewError("REVISION_CONFLICT", 409)
					}
				}
				return idempotency.Response{Status: 201, Body: body,
					ResourceID: &event.ID}, nil
			})
			return e
		})
	return out, correctionError(err)
}
