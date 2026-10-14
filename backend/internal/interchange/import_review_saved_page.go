package interchange

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
)

type ImportReviewSavedPage struct {
	Items     []ImportReviewDecisionStatus `json:"items"`
	NextAfter string                       `json:"next_after,omitempty"`
}

func importReviewSavedCursor(item ImportReviewDecisionStatus) string {
	return item.EntryID.String() + "." + item.TargetVersionID.String() + "." + item.Kind + "." + item.Proposal.ArtifactID.String()
}

func parseImportReviewSavedCursor(raw string) (uuid.UUID, uuid.UUID, string, uuid.UUID, error) {
	if raw == "" {
		return uuid.Nil, uuid.Nil, "", uuid.Nil, nil
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 4 || len(raw) > 128 || (parts[2] != "defects" && parts[2] != "dust") {
		return uuid.Nil, uuid.Nil, "", uuid.Nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	entry, e1 := uuid.Parse(parts[0])
	target, e2 := uuid.Parse(parts[1])
	proposal, e3 := uuid.Parse(parts[3])
	if e1 != nil || e2 != nil || e3 != nil || entry == uuid.Nil || target == uuid.Nil || proposal == uuid.Nil ||
		parts[0] != entry.String() || parts[1] != target.String() || parts[3] != proposal.String() {
		return uuid.Nil, uuid.Nil, "", uuid.Nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	return entry, target, parts[2], proposal, nil
}

func (s ImportReviewDecisionService) ListSavedPage(ctx context.Context, who auth.Identity, tenantID, projectID, workflowID uuid.UUID, after string) (ImportReviewSavedPage, error) {
	var zero ImportReviewSavedPage
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	entry, target, kind, proposal, err := parseImportReviewSavedCursor(after)
	if err != nil {
		return zero, err
	}
	if s.DB == nil {
		return zero, domain.NewError("INTERCHANGE_UNAVAILABLE", 503)
	}
	if tenantID == uuid.Nil || projectID == uuid.Nil || workflowID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, domain.NewError("VALIDATION_FAILED", 422)
	}
	page := ImportReviewSavedPage{Items: make([]ImportReviewDecisionStatus, 0, 501)}
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
		rows, err := tx.Query(ctx, `SELECT photo.v30_import_review_saved_page($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			tenantID, who.UserID, who.SessionID, projectID, workflowID, afterEntry, afterTarget, afterKind, afterProposal)
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
			if len(page.Items) >= 501 || json.Unmarshal(raw, &item) != nil || item.EntryID == uuid.Nil || item.TargetVersionID == uuid.Nil ||
				(item.Kind != "defects" && item.Kind != "dust") || !validImportRef(item.Proposal) || !validImportRef(item.Base) ||
				(item.Kind == "defects" && (item.LabelMap == nil || !validImportRef(*item.LabelMap))) ||
				(item.Kind == "dust" && item.LabelMap != nil) ||
				(item.Decision != "accept" && item.Decision != "reject" && item.Decision != "skip") ||
				(item.Status != "granted" && item.Status != "revoked") || item.Epoch < 1 ||
				len(item.Accepted)+len(item.Rejected) > 4096 {
				return domain.NewError("INTERCHANGE_DECISION_INVALID_RECEIPT", 503)
			}
			page.Items = append(page.Items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return zero, err
	}
	if len(page.Items) == 501 {
		page.NextAfter = importReviewSavedCursor(page.Items[499])
		page.Items = page.Items[:500]
	}
	return page, ctx.Err()
}
