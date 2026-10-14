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

type ImportReviewProposalPage struct {
	Items     []ImportReviewProposalItem `json:"items"`
	NextAfter string                     `json:"next_after,omitempty"`
}

func importReviewProposalCursor(item ImportReviewProposalItem) string {
	return item.EntryID.String() + "." + item.TargetVersionID.String() + "." + item.Kind + "." + item.Proposal.ArtifactID.String()
}

func (s ImportReviewProposalListService) List(ctx context.Context, who auth.Identity, tenantID, projectID, workflowID uuid.UUID) ([]ImportReviewProposalItem, error) {
	page, err := s.ListPage(ctx, who, tenantID, projectID, workflowID, "")
	return page.Items, err
}

func (s ImportReviewProposalListService) ListPage(ctx context.Context, who auth.Identity, tenantID, projectID, workflowID uuid.UUID, after string) (ImportReviewProposalPage, error) {
	var zero ImportReviewProposalPage
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" {
		return zero, domain.NewError("INTERCHANGE_UNAVAILABLE", 503)
	}
	if tenantID == uuid.Nil || projectID == uuid.Nil || workflowID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, domain.NewError("VALIDATION_FAILED", 422)
	}
	if _, _, _, _, err := parseImportReviewSavedCursor(after); err != nil {
		return zero, err
	}
	parsedSource := SQLImportMaskCandidateParsedSource{DB: s.DB, Tenant: tenantID, Workflow: workflowID, Who: who, Bucket: s.Bucket}
	parsed, err := ReadImportReviewParsed(ctx, parsedSource, s.Objects)
	if err != nil {
		return zero, err
	}
	metadata, err := s.listMetadata(ctx, who, tenantID, projectID, workflowID, parsed, after)
	if err != nil {
		return zero, err
	}
	items := metadata
	nextAfter := ""
	if len(metadata) == 51 {
		items = metadata[:50]
		nextAfter = importReviewProposalCursor(items[49])
	}
	for i := range items {
		item := &items[i]
		source := SQLImportReviewProposalSource{DB: s.DB, Tenant: tenantID, Workflow: workflowID, Who: who, Entry: item.EntryID, Target: item.TargetVersionID, Kind: item.Kind, Proposal: item.Proposal.ArtifactID, Parsed: parsed, Bucket: s.Bucket}
		raw, proposal, base, readErr := ReadImportReviewProposal(ctx, source, s.Objects, item.Kind)
		if readErr != nil {
			return zero, readErr
		}
		if proposal != item.Proposal || base != item.Base {
			return zero, artifactjson.ErrStale
		}
		item.Document = append(json.RawMessage(nil), raw...)
	}
	currentParsed, err := ReadImportReviewParsed(ctx, parsedSource, s.Objects)
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(parsed, currentParsed) {
		return zero, artifactjson.ErrStale
	}
	current, err := s.listMetadata(ctx, who, tenantID, projectID, workflowID, currentParsed, after)
	if err != nil {
		return zero, err
	}
	if len(current) != len(metadata) {
		return zero, artifactjson.ErrStale
	}
	for i := range metadata {
		if current[i].EntryID != metadata[i].EntryID || current[i].TargetVersionID != metadata[i].TargetVersionID || current[i].Kind != metadata[i].Kind ||
			current[i].Proposal != metadata[i].Proposal || current[i].Base != metadata[i].Base || current[i].DecisionStatus != metadata[i].DecisionStatus || current[i].DecisionEpoch != metadata[i].DecisionEpoch {
			return zero, artifactjson.ErrStale
		}
	}
	return ImportReviewProposalPage{Items: items, NextAfter: nextAfter}, ctx.Err()
}

func (s ImportReviewProposalListService) listMetadata(ctx context.Context, who auth.Identity, tenantID, projectID, workflowID uuid.UUID, parsed []byte, after string) ([]ImportReviewProposalItem, error) {
	entry, target, kind, proposal, err := parseImportReviewSavedCursor(after)
	if err != nil {
		return nil, err
	}
	items := make([]ImportReviewProposalItem, 0)
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
		var afterEntry, afterTarget, afterProposal *uuid.UUID
		var afterKind *string
		if after != "" {
			afterEntry, afterTarget, afterKind, afterProposal = &entry, &target, &kind, &proposal
		}
		rows, err := tx.Query(ctx, `SELECT entry_id,target_version_id,kind,proposal_id,proposal_sha,base_id,base_sha,decision_status,decision_epoch FROM photo.v30_import_review_proposal_page($1,$2,$3,$4,$5,$6,$7,$8,$9)`, tenantID, who.UserID, who.SessionID, workflowID, string(parsed), afterEntry, afterTarget, afterKind, afterProposal)
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
			if len(items) >= 51 || item.EntryID == uuid.Nil || item.TargetVersionID == uuid.Nil ||
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
