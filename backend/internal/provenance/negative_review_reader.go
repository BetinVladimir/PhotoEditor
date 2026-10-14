package provenance

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2optical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type SQLSigningNegativeReviewReader struct {
	DB        *pgxpool.Pool
	Adoption  SigningNegativeAdoptionReader
	Documents SigningTaskDocumentReader
}

// Only an existing append-once candidate decision for the exact adopted
// working image can qualify. Semantic-transfer approval is never consulted.
func (s SQLSigningNegativeReviewReader) ReadSigningNegativeReview(ctx context.Context, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask, root, report v2input.Ref) (*SigningNegativeReview, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil || s.Adoption == nil || s.Documents == nil || !task.ReviewRequired {
		return nil, ErrInvalid
	}
	adopted, result, err := ReadSigningNegativeResult(ctx, s.Adoption, s.Documents, who, ready, task)
	if err != nil {
		return nil, err
	}
	if adopted != root || result.Report != report {
		return nil, ErrInvalid
	}
	paramsRaw, err := s.Documents.ReadSigningTaskDocument(ctx,who,ready,task,task.Params,65536)
    if err != nil {return nil,err}
    paramsRaw=append([]byte(nil),paramsRaw...)
    if !pinned(paramsRaw,task.Params,65536){return nil,ErrInvalid}
    params,err:=v2optical.ParseNegativeParams(paramsRaw)
    if err!=nil{return nil,ErrInvalid}
	var approval *SigningNegativeReview
	err = tenant.WithTenant(ctx, s.DB, who, ready.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, `SELECT v.id,v.revision,v.state,v.kind,coalesce(v.decision,''),
   coalesce(v.decision_route,''),coalesce(v.decision_hash,''),v.decided_by,
   v.decided_at IS NOT NULL,v.selection,v.candidate_artifact_id,v.candidate_hash,
   v.base_artifact_id,b.sha256
   FROM photo.reviews v LEFT JOIN photo.artifacts b ON b.tenant_id=v.tenant_id AND b.id=v.base_artifact_id
   WHERE v.tenant_id=$1 AND v.run_id=$2 AND v.task_id=$3 ORDER BY v.id LIMIT 2`, ready.Tenant, task.Run, task.Task)
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			count++
			if count > 1 {
				return ErrInvalid
			}
			var id uuid.UUID
			var revision int64
			var state, kind, decision, route, hash string
			var actor, base *uuid.UUID
			var dated bool
			var selection []byte
			var candidate uuid.UUID
			var candidateSHA string
			var baseSHA *string
			if err := rows.Scan(&id, &revision, &state, &kind, &decision, &route, &hash, &actor, &dated, &selection, &candidate, &candidateSHA, &base, &baseSHA); err != nil {
				return err
			}
			if kind != "optical.negative" || actor == nil || base == nil || baseSHA == nil || !dated || state != "decided" || revision != 2 || decision != "accept" || route != "candidate" || candidate != result.WorkingImage.ArtifactID || candidateSHA != result.WorkingImage.SHA256 || *base != params.Base.Source.ArtifactID || *baseSHA != params.Base.Source.SHA256 {
				return ErrInvalid
			}
			snapshot := review.CandidateSnapshot{TenantID: ready.Tenant, ReviewID: id, RunID: task.Run, Revision: 1, State: "pending", Kind: kind, Candidate: review.ArtifactRef{ID: candidate, SHA256: candidateSHA}, Base: review.ArtifactRef{ID: *base, SHA256: *baseSHA}}
			actual, err := verifySigningCandidateDecision(snapshot, *actor, selection, hash)
			if err != nil {
				return err
			}
			// Ensure selection is an object even if a future canonical codec changes.
			var object map[string]json.RawMessage
			if json.Unmarshal(actual, &object) != nil || object == nil {
				return ErrInvalid
			}
			approval = &SigningNegativeReview{ID: id, Task: task.Task, Result: task.Result, Root: root, Report: report, Accepted: true}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if approval == nil {
			return ErrInvalid
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	currentParams,err:=s.Documents.ReadSigningTaskDocument(ctx,who,ready,task,task.Params,65536)
    if err!=nil{return nil,err};if !bytes.Equal(currentParams,paramsRaw){return nil,ErrInvalid}
	current, err := s.Adoption.ReadSigningAdoptedNegativeRoot(ctx, who, ready, task)
	if err != nil {
		return nil, err
	}
	if current != root {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return approval, nil
}

var _ SigningNegativeReviewReader = SQLSigningNegativeReviewReader{}
