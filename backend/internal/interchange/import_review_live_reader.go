package interchange

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/workflow"
)

var ErrImportReviewAwaitingDecision = workflow.ErrInterchangeImportAwaitingDecision

type ImportLiveReviewDecision struct {
	Decision string
	Accepted []string
	Rejected []string
	Epoch    int64
}

type SQLImportLiveReviewReader struct {
	DB        *pgxpool.Pool
	Tenant    uuid.UUID
	Workflow  uuid.UUID
	Parsed    []byte
	Witnesses *ImportReviewWitnesses
}

func (r SQLImportLiveReviewReader) Read(ctx context.Context, entry uuid.UUID, target v2input.SessionVersionBinding, kind string, proposal, base v2input.Ref, label *v2input.Ref) (ImportLiveReviewDecision, error) {
	var zero ImportLiveReviewDecision
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if r.DB == nil || r.Tenant == uuid.Nil || r.Workflow == uuid.Nil || entry == uuid.Nil || target.AssetVersionID == uuid.Nil ||
		!validImportRef(target.Source) || target.Source != base || !validImportRef(proposal) || !validImportRef(base) ||
		(kind != "defects" && kind != "dust") || kind == "defects" && (label == nil || !validImportRef(*label)) || kind == "dust" && label != nil ||
		len(r.Parsed) < 2 || len(r.Parsed) > 8<<20 {
		return zero, v2input.ErrInvalid
	}
	var labelID *uuid.UUID
	var labelSHA *string
	if label != nil {
		labelID, labelSHA = &label.ArtifactID, &label.SHA256
	}
	var acceptedRaw, rejectedRaw []byte
	err := database.TenantTransaction(ctx, r.DB, r.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT decision,accepted_ids,rejected_ids,epoch FROM photo.v30_import_review_live_decision($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			r.Tenant, r.Workflow, entry, target.AssetVersionID, kind, proposal.ArtifactID, proposal.SHA256, base.ArtifactID, base.SHA256,
			labelID, labelSHA, string(r.Parsed)).Scan(&zero.Decision, &acceptedRaw, &rejectedRaw, &zero.Epoch)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ImportLiveReviewDecision{}, ErrImportReviewAwaitingDecision
	}
	if err != nil {
		return ImportLiveReviewDecision{}, err
	}
	if json.Unmarshal(acceptedRaw, &zero.Accepted) != nil || json.Unmarshal(rejectedRaw, &zero.Rejected) != nil ||
		zero.Epoch < 1 || (zero.Decision != "accept" && zero.Decision != "reject" && zero.Decision != "skip") ||
		zero.Decision != "accept" && (len(zero.Accepted) != 0 || len(zero.Rejected) != 0) ||
		len(zero.Accepted)+len(zero.Rejected) > 4096 || !validLiveReviewIDs(zero.Accepted, zero.Rejected) {
		return ImportLiveReviewDecision{}, v2input.ErrInvalid
	}
	if r.Witnesses != nil {
		if err := r.Witnesses.Record(ctx, ImportReviewWitness{Entry: entry, Target: target.AssetVersionID, Kind: kind, Proposal: proposal, Base: base,
			LabelMap: label, Epoch: zero.Epoch, Decision: zero.Decision, Accepted: zero.Accepted, Rejected: zero.Rejected}); err != nil {
			return ImportLiveReviewDecision{}, err
		}
	}
	return zero, ctx.Err()
}

func validLiveReviewIDs(accepted, rejected []string) bool {
	seen := make(map[string]bool, len(accepted)+len(rejected))
	for _, group := range [][]string{accepted, rejected} {
		last := ""
		for _, id := range group {
			if _, err := domain.ParseDigest(id); err != nil || id <= last || seen[id] {
				return false
			}
			seen[id] = true
			last = id
		}
	}
	return true
}
