package review

import (
	"bytes"
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

// ReadRestorationResultSelection replays the decided human plan and compares it to
// the current principal-scoped SQL selection. Neither source alone is treated
// as authority for a rejected candidate or skipped output.
func (s *Service) ReadRestorationResultSelection(ctx context.Context, who auth.Identity, tid, reviewID uuid.UUID) (RestorationResultSelection, error) {
	var out RestorationResultSelection
	if s == nil || s.DB == nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if tid == uuid.Nil || reviewID == uuid.Nil {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		var snapshot CandidateSnapshot
		var projectID, actor uuid.UUID
		var kind, decision, route, hash string
		var selection []byte
		err := tx.QueryRow(ctx, `SELECT w.project_id,v.kind,v.run_id,v.decided_by,v.decision,v.decision_route,v.decision_hash,v.selection,
   v.candidate_artifact_id,v.candidate_hash,v.base_artifact_id,b.sha256
   FROM photo.reviews v
   JOIN photo.tasks task ON task.tenant_id=v.tenant_id AND task.id=v.task_id AND task.run_id=v.run_id
   JOIN photo.runs r ON r.tenant_id=v.tenant_id AND r.id=v.run_id
   JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
   JOIN photo.artifacts b ON b.tenant_id=v.tenant_id AND b.id=v.base_artifact_id
   WHERE v.tenant_id=$1 AND v.id=$2 AND v.kind IN('portrait.glare','portrait.open_eyes','image.reflection') AND v.state='decided'
    AND v.revision=2 AND v.decided_by IS NOT NULL AND v.decided_at IS NOT NULL
   FOR SHARE OF v,task,r`, tid, reviewID).
			Scan(&projectID, &kind, &snapshot.RunID, &actor, &decision, &route, &hash, &selection,
				&snapshot.Candidate.ID, &snapshot.Candidate.SHA256, &snapshot.Base.ID, &snapshot.Base.SHA256)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return err
		}
		if _, err = project.Authorize(ctx, tx, tid, projectID, who.UserID, role, domain.Editor, false); err != nil {
			return err
		}
		snapshot.TenantID = tid
		snapshot.ReviewID = reviewID
		snapshot.Kind = kind
		snapshot.State = "pending"
		snapshot.Revision = 1
		request := Decision{ExpectedRevision: 1, CandidateHash: snapshot.Candidate.SHA256, Decision: decision}
		plan, err := PlanCandidate(snapshot, actor, request)
		if err != nil {
			return err
		}
		expected, err := canonical.Marshal(plan.Selection)
		normalized, normalizeErr := canonical.Bytes(selection)
		if err != nil || normalizeErr != nil || !bytes.Equal(expected, normalized) || plan.DecisionHash != hash || plan.Route != route {
			return domain.NewError("STALE_CANDIDATE_REVIEW", 409)
		}
		compiled, err := CompileRestorationResultSelection(snapshot, actor, request, plan)
		if err != nil {
			return err
		}
		var selectedReview uuid.UUID
		var selectedHash, selectedRoute string
		var imageID *uuid.UUID
		var imageSHA *string
		selectionFunction := "photo.get_v28_restoration_review_selection"
		if kind == "portrait.open_eyes" {
			selectionFunction = "photo.get_v28_open_eyes_review_selection"
		}
		err = tx.QueryRow(ctx, `SELECT review_id,decision_hash,route,image_id,image_sha
 FROM `+selectionFunction+`($1,$2,$3,$4)`, tid, who.UserID, who.SessionID, reviewID).
			Scan(&selectedReview, &selectedHash, &selectedRoute, &imageID, &imageSHA)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("STALE_CANDIDATE_REVIEW", 409)
		}
		if err != nil {
			return err
		}
		if selectedReview != reviewID || selectedHash != hash || selectedRoute != route ||
			(imageID == nil) != (compiled.Image == nil) || (imageSHA == nil) != (compiled.Image == nil) {
			return domain.NewError("STALE_CANDIDATE_REVIEW", 409)
		}
		if compiled.Image != nil && (*imageID != compiled.Image.ID || *imageSHA != compiled.Image.SHA256) {
			return domain.NewError("STALE_CANDIDATE_REVIEW", 409)
		}
		out = compiled
		return ctx.Err()
	})
	if err != nil {
		return RestorationResultSelection{}, err
	}
	return out, nil
}
