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
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

type ImportReviewProposalItem struct {
	EntryID         uuid.UUID       `json:"entry_id"`
	TargetVersionID uuid.UUID       `json:"target_version_id"`
	Kind            string          `json:"kind"`
	Proposal        v2input.Ref     `json:"proposal"`
	Base            v2input.Ref     `json:"base"`
	Document        json.RawMessage `json:"document"`
	DecisionStatus  string          `json:"decision_status,omitempty"`
	DecisionEpoch   int64           `json:"decision_epoch"`
}

type ImportReviewProposalListService struct {
	DB      *pgxpool.Pool
	Objects ImportDocumentObjects
	Bucket  string
}

func (s ImportReviewProposalListService) List(ctx context.Context, who auth.Identity, tenantID, projectID, workflowID uuid.UUID) ([]ImportReviewProposalItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" {
		return nil, domain.NewError("INTERCHANGE_UNAVAILABLE", 503)
	}
	if tenantID == uuid.Nil || projectID == uuid.Nil || workflowID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	parsedSource := SQLImportMaskCandidateParsedSource{DB: s.DB, Tenant: tenantID, Workflow: workflowID, Who: who, Bucket: s.Bucket}
	parsed, err := ReadImportReviewParsed(ctx, parsedSource, s.Objects)
	if err != nil {
		return nil, err
	}
	items, err := s.listMetadata(ctx, who, tenantID, projectID, workflowID, parsed)
	if err != nil {
		return nil, err
	}
	for i := range items {
		item := &items[i]
		source := SQLImportReviewProposalSource{DB: s.DB, Tenant: tenantID, Workflow: workflowID, Who: who, Entry: item.EntryID, Target: item.TargetVersionID, Kind: item.Kind, Proposal: item.Proposal.ArtifactID, Parsed: parsed, Bucket: s.Bucket}
		raw, proposal, base, readErr := ReadImportReviewProposal(ctx, source, s.Objects, item.Kind)
		if readErr != nil {
			return nil, readErr
		}
		if proposal != item.Proposal || base != item.Base {
			return nil, artifactjson.ErrStale
		}
		item.Document = append(json.RawMessage(nil), raw...)
	}
	currentParsed, err := ReadImportReviewParsed(ctx, parsedSource, s.Objects)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(parsed, currentParsed) {
		return nil, artifactjson.ErrStale
	}
	current, err := s.listMetadata(ctx, who, tenantID, projectID, workflowID, currentParsed)
	if err != nil {
		return nil, err
	}
	if len(current) != len(items) {
		return nil, artifactjson.ErrStale
	}
	for i := range items {
		if current[i].EntryID != items[i].EntryID || current[i].TargetVersionID != items[i].TargetVersionID || current[i].Kind != items[i].Kind ||
			current[i].Proposal != items[i].Proposal || current[i].Base != items[i].Base || current[i].DecisionStatus != items[i].DecisionStatus || current[i].DecisionEpoch != items[i].DecisionEpoch {
			return nil, artifactjson.ErrStale
		}
	}
	return items, ctx.Err()
}

func (s ImportReviewProposalListService) listMetadata(ctx context.Context, who auth.Identity, tenantID, projectID, workflowID uuid.UUID, parsed []byte) ([]ImportReviewProposalItem, error) {
	items := make([]ImportReviewProposalItem, 0)
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
		rows, err := tx.Query(ctx, `SELECT entry_id,target_version_id,kind,proposal_id,proposal_sha,base_id,base_sha,decision_status,decision_epoch FROM photo.v30_import_review_proposal_list($1,$2,$3,$4,$5)`, tenantID, who.UserID, who.SessionID, workflowID, string(parsed))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item ImportReviewProposalItem
			var status *string
			var epoch *int64
			if err := rows.Scan(&item.EntryID, &item.TargetVersionID, &item.Kind, &item.Proposal.ArtifactID, &item.Proposal.SHA256,
				&item.Base.ArtifactID, &item.Base.SHA256, &status, &epoch); err != nil {
				return err
			}
			if len(items) >= 50 || item.EntryID == uuid.Nil || item.TargetVersionID == uuid.Nil ||
				(item.Kind != "defects" && item.Kind != "dust") || !validImportRef(item.Proposal) || !validImportRef(item.Base) {
				return domain.NewError("INTERCHANGE_PROPOSAL_LIST_INVALID", 503)
			}
			if status != nil {
				if (*status != "granted" && *status != "revoked") || epoch == nil || *epoch < 1 {
					return domain.NewError("INTERCHANGE_PROPOSAL_LIST_INVALID", 503)
				}
				item.DecisionStatus, item.DecisionEpoch = *status, *epoch
			} else if epoch != nil {
				return domain.NewError("INTERCHANGE_PROPOSAL_LIST_INVALID", 503)
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}
