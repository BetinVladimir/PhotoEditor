package interchange

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

type ImportReviewDecisionService struct {
	DB      *pgxpool.Pool
	Objects ImportDocumentObjects
	Bucket  string
}

type ImportReviewDecisionReceipt struct {
	WorkflowID      uuid.UUID `json:"workflow_id"`
	EntryID         uuid.UUID `json:"entry_id"`
	Kind            string    `json:"kind"`
	TargetVersionID uuid.UUID `json:"target_version_id"`
	ProposalID      uuid.UUID `json:"proposal_id"`
	Status          string    `json:"status"`
	Epoch           int64     `json:"epoch"`
}

type ImportReviewDecisionStatus struct {
	EntryID         uuid.UUID    `json:"entry_id"`
	TargetVersionID uuid.UUID    `json:"target_version_id"`
	Kind            string       `json:"kind"`
	Proposal        v2input.Ref  `json:"proposal"`
	Base            v2input.Ref  `json:"base"`
	LabelMap        *v2input.Ref `json:"label_map"`
	Decision        string       `json:"decision"`
	Accepted        []string     `json:"accepted_candidate_ids"`
	Rejected        []string     `json:"rejected_candidate_ids"`
	Status          string       `json:"status"`
	Epoch           int64        `json:"epoch"`
}

func (s ImportReviewDecisionService) ListSaved(ctx context.Context, who auth.Identity, tenantID, projectID, workflowID uuid.UUID) ([]ImportReviewDecisionStatus, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil {
		return nil, domain.NewError("INTERCHANGE_UNAVAILABLE", 503)
	}
	if tenantID == uuid.Nil || projectID == uuid.Nil || workflowID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	items := make([]ImportReviewDecisionStatus, 0)
	err := database.TenantTransaction(ctx, s.DB, tenantID, func(tx pgx.Tx) error {
		var actual uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT project_id FROM photo.v2_workflows WHERE tenant_id=$1 AND id=$2 AND workflow_type='interchange'`, tenantID, workflowID).Scan(&actual); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.NewError("NOT_FOUND", 404)
			}
			return err
		}
		if actual != projectID {
			return domain.NewError("NOT_FOUND", 404)
		}
		rows, err := tx.Query(ctx, `SELECT photo.v30_import_review_saved_list($1,$2,$3,$4,$5)`, tenantID, who.UserID, who.SessionID, projectID, workflowID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			var item ImportReviewDecisionStatus
			if len(items) >= 500 || json.Unmarshal(raw, &item) != nil || item.EntryID == uuid.Nil || item.TargetVersionID == uuid.Nil ||
				(item.Kind != "defects" && item.Kind != "dust") || !validImportRef(item.Proposal) || !validImportRef(item.Base) ||
				(item.Kind == "defects" && (item.LabelMap == nil || !validImportRef(*item.LabelMap))) ||
				(item.Kind == "dust" && item.LabelMap != nil) ||
				(item.Decision != "accept" && item.Decision != "reject" && item.Decision != "skip") ||
				(item.Status != "granted" && item.Status != "revoked") || item.Epoch < 1 || len(item.Accepted)+len(item.Rejected) > 4096 {
				return domain.NewError("INTERCHANGE_DECISION_INVALID_RECEIPT", 503)
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}

func (s ImportReviewDecisionService) ReadStatus(ctx context.Context, who auth.Identity, tenantID, projectID, workflowID, entryID, targetID uuid.UUID, kind string, proposalID uuid.UUID) (ImportReviewDecisionStatus, error) {
	var result ImportReviewDecisionStatus
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if s.DB == nil {
		return result, domain.NewError("INTERCHANGE_UNAVAILABLE", 503)
	}
	if tenantID == uuid.Nil || projectID == uuid.Nil || workflowID == uuid.Nil || entryID == uuid.Nil || targetID == uuid.Nil || proposalID == uuid.Nil ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil || kind != "defects" && kind != "dust" {
		return result, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := database.TenantTransaction(ctx, s.DB, tenantID, func(tx pgx.Tx) error {
		var actual uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT project_id FROM photo.v2_workflows WHERE tenant_id=$1 AND id=$2 AND workflow_type='interchange'`, tenantID, workflowID).Scan(&actual); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.NewError("NOT_FOUND", 404)
			}
			return err
		}
		if actual != projectID {
			return domain.NewError("NOT_FOUND", 404)
		}
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT photo.v30_import_review_status($1,$2,$3,$4,$5,$6,$7,$8)`, tenantID, who.UserID, who.SessionID, workflowID, entryID, targetID, kind, proposalID).Scan(&raw); err != nil {
			return err
		}
		if len(raw) == 0 {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			return err
		}
		if result.EntryID != entryID || result.TargetVersionID != targetID || result.Kind != kind || result.Proposal.ArtifactID != proposalID ||
			!validImportRef(result.Proposal) || !validImportRef(result.Base) || result.Epoch < 1 || result.Status != "granted" && result.Status != "revoked" {
			return domain.NewError("INTERCHANGE_DECISION_INVALID_RECEIPT", 503)
		}
		return nil
	})
	if err != nil {
		return ImportReviewDecisionStatus{}, err
	}
	return result, ctx.Err()
}

func (s ImportReviewDecisionService) checkProject(ctx context.Context, tenantID, projectID, workflowID uuid.UUID) error {
	return database.TenantTransaction(ctx, s.DB, tenantID, func(tx pgx.Tx) error {
		var actual uuid.UUID
		err := tx.QueryRow(ctx, `SELECT project_id FROM photo.v2_workflows WHERE tenant_id=$1 AND id=$2 AND workflow_type='interchange'`, tenantID, workflowID).Scan(&actual)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && actual != projectID {
			return domain.NewError("NOT_FOUND", 404)
		}
		return err
	})
}

func (s ImportReviewDecisionService) Decide(ctx context.Context, who auth.Identity, tenantID, projectID, workflowID, requestID uuid.UUID, req ImportReviewDecisionRequest) (ImportReviewDecisionReceipt, error) {
	var zero ImportReviewDecisionReceipt
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil {
		return zero, domain.NewError("INTERCHANGE_UNAVAILABLE", 503)
	}
	if tenantID == uuid.Nil || projectID == uuid.Nil || workflowID == uuid.Nil || requestID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, domain.NewError("VALIDATION_FAILED", 422)
	}
	if err := validateImportReviewDecisionRequest(ctx, req); err != nil {
		return zero, err
	}
	if err := s.checkProject(ctx, tenantID, projectID, workflowID); err != nil {
		return zero, err
	}
	request, err := canonical.Marshal(req)
	if err != nil {
		return zero, err
	}
	var parsed, proposalRaw []byte
	if req.Action == "grant" {
		if s.Objects == nil || s.Bucket == "" {
			return zero, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
		}
		parsedSource := SQLImportMaskCandidateParsedSource{DB: s.DB, Tenant: tenantID, Workflow: workflowID, Who: who, Bucket: s.Bucket}
		parsed, err = ReadImportReviewParsed(ctx, parsedSource, s.Objects)
		if err != nil {
			return zero, err
		}
		proposalSource := SQLImportReviewProposalSource{DB: s.DB, Tenant: tenantID, Workflow: workflowID, Who: who, Entry: req.EntryID, Target: req.TargetVersionID, Kind: req.Kind, Proposal: req.Proposal.ArtifactID, Parsed: parsed, Bucket: s.Bucket}
		var proposal, base v2input.Ref
		proposalRaw, proposal, base, err = ReadImportReviewProposal(ctx, proposalSource, s.Objects, req.Kind)
		if err != nil {
			return zero, err
		}
		if proposal != req.Proposal || base != req.Base {
			return zero, artifactjson.ErrStale
		}
		if req.Kind == "defects" {
			var doc struct {
				LabelMap v2input.Ref `json:"label_map"`
			}
			if json.Unmarshal(proposalRaw, &doc) != nil || req.LabelMap == nil || doc.LabelMap != *req.LabelMap {
				return zero, artifactjson.ErrStale
			}
		}
		current, readErr := ReadImportReviewParsed(ctx, parsedSource, s.Objects)
		if readErr != nil {
			return zero, readErr
		}
		if !bytes.Equal(parsed, current) {
			return zero, artifactjson.ErrStale
		}
	}
	var epoch *int64
	err = database.TenantTransaction(ctx, s.DB, tenantID, func(tx pgx.Tx) error {
		var actual uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT project_id FROM photo.v2_workflows WHERE tenant_id=$1 AND id=$2 AND workflow_type='interchange'`, tenantID, workflowID).Scan(&actual); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.NewError("NOT_FOUND", 404)
			}
			return err
		}
		if actual != projectID {
			return domain.NewError("NOT_FOUND", 404)
		}
		if req.Action == "grant" {
			return tx.QueryRow(ctx, `SELECT photo.grant_v30_import_review($1,$2,$3,$4,$5,$6,$7,$8::jsonb)`, tenantID, who.UserID, who.SessionID, workflowID, requestID, string(parsed), string(proposalRaw), string(request)).Scan(&epoch)
		}
		return tx.QueryRow(ctx, `SELECT photo.revoke_v30_import_review($1,$2,$3,$4,$5,$6::jsonb)`, tenantID, who.UserID, who.SessionID, workflowID, requestID, string(request)).Scan(&epoch)
	})
	if err != nil {
		return zero, err
	}
	if epoch == nil || *epoch < 1 {
		return zero, domain.NewError("REVISION_CONFLICT", 409)
	}
	status := "granted"
	if req.Action == "revoke" {
		status = "revoked"
	}
	return ImportReviewDecisionReceipt{WorkflowID: workflowID, EntryID: req.EntryID, Kind: req.Kind, TargetVersionID: req.TargetVersionID, ProposalID: req.Proposal.ArtifactID, Status: status, Epoch: *epoch}, ctx.Err()
}
