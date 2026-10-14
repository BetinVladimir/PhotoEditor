package review

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

// DefectGate writes graph routing using this same transaction. Implementations
// must not commit it, publish messages or perform other external side effects.
type DefectGate interface {
	ApplyDefectDecision(context.Context, pgx.Tx, DefectSnapshot, DefectPlan) error
}

// PersistDefectsAuthorized requires a live tenant transaction with editor ACL
// already checked for the run's project. Idempotency must be acquired before
// calling. A savepoint guarantees decision/audit/gate all roll back on error,
// even if the caller catches the error and continues its outer transaction.
// The caller commits the outer transaction; success here is not durable yet.
func PersistDefectsAuthorized(ctx context.Context, outer pgx.Tx, tenantID, reviewID, actor, requestID uuid.UUID,
	proposal VerifiedProposal, request Decision, gate DefectGate) (DefectPlan, error) {
	if outer == nil || gate == nil || tenantID == uuid.Nil || reviewID == uuid.Nil || actor == uuid.Nil || requestID == uuid.Nil || !proposal.ref.valid() {
		return DefectPlan{}, domain.NewError("INTERNAL_ERROR", 500)
	}
	tx, err := outer.Begin(ctx)
	if err != nil {
		return DefectPlan{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var s DefectSnapshot
	s.TenantID = tenantID
	s.ReviewID = reviewID
	s.Image = proposal.document.Image
	s.LabelMap = proposal.document.LabelMap
	var status string
	var cancelled bool
	// Run-before-review lock order also serializes cancellation with the decision.
	err = tx.QueryRow(ctx, `SELECT r.id,r.status,r.cancel_requested FROM photo.runs r
 JOIN photo.reviews v ON v.tenant_id=r.tenant_id AND v.run_id=r.id
 WHERE v.tenant_id=$1 AND v.id=$2 FOR UPDATE OF r`, tenantID, reviewID).Scan(&s.RunID, &status, &cancelled)
	if errors.Is(err, pgx.ErrNoRows) {
		return DefectPlan{}, domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		return DefectPlan{}, err
	}
	if cancelled || (status != "running" && status != "waiting_review") {
		return DefectPlan{}, domain.NewError("REVISION_CONFLICT", 409)
	}
	var kind string
	var taskID, baseID, resolvedID, reportID *uuid.UUID
	err = tx.QueryRow(ctx, `SELECT revision,state,candidate_artifact_id,candidate_hash,kind,task_id,base_artifact_id,resolved_artifact_id,proposal_report_artifact_id FROM photo.reviews
 WHERE tenant_id=$1 AND id=$2 AND run_id=$3 FOR UPDATE`, tenantID, reviewID, s.RunID).Scan(&s.Revision, &s.State, &s.Proposal.ID, &s.Proposal.SHA256, &kind, &taskID, &baseID, &resolvedID, &reportID)
	if err != nil {
		return DefectPlan{}, err
	}
	if kind != "defects" || taskID == nil {
		return DefectPlan{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if baseID == nil || *baseID != s.Image.ID || resolvedID == nil || reportID == nil {
		return DefectPlan{}, domain.NewError("STALE_DEFECT_REVIEW", 409)
	}
	s, err = proposal.Bind(s)
	if err != nil {
		return DefectPlan{}, domain.NewError("STALE_DEFECT_REVIEW", 409)
	}
	plan, err := PlanDefects(s, actor, request)
	if err != nil {
		return DefectPlan{}, err
	}
	// Both outputs must belong to this successful canonical proposal attempt.
	// An orphan output or a result from another task is not an approvable proposal.
	var attested bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM photo.tasks t
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=$4 AND a.created_by_task_id=t.id
 JOIN photo.artifacts m ON m.tenant_id=t.tenant_id AND m.id=$6 AND m.created_by_task_id=t.id
 JOIN photo.artifacts image ON image.tenant_id=t.tenant_id AND image.id=$8 AND image.state='ready' AND image.sha256=$9
 JOIN photo.artifacts report ON report.tenant_id=t.tenant_id AND report.id=$10 AND report.created_by_task_id=t.id
 JOIN photo.artifacts style ON style.tenant_id=t.tenant_id AND style.id=$11
 JOIN photo.tasks st ON st.tenant_id=style.tenant_id AND st.id=style.created_by_task_id AND st.run_id=t.run_id
 JOIN photo.task_results sr ON sr.tenant_id=st.tenant_id AND sr.id=st.canonical_result_id AND sr.task_id=st.id
 WHERE t.tenant_id=$1 AND t.id=$2 AND t.run_id=$3 AND t.operation='defects.propose' AND t.operation_version='1' AND t.state='succeeded'
 AND a.state='ready' AND a.sha256=$5 AND a.output_role='defect_candidates'
 AND m.state='ready' AND m.sha256=$7 AND m.output_role='defect_labels'
 AND report.state='ready' AND report.output_role='defect_propose_report'
 AND style.state='ready' AND style.output_role='resolved_style'
 AND st.operation='style.resolve' AND st.operation_version='1' AND st.state='succeeded'
 AND t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',image.id::text,'sha256',image.sha256))
 AND sr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',style.id::text,'sha256',style.sha256))
 AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',report.id::text,'sha256',report.sha256))
 AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256),jsonb_build_object('artifact_id',m.id::text,'sha256',m.sha256)))`,
		tenantID, *taskID, s.RunID, s.Proposal.ID, s.Proposal.SHA256, s.LabelMap.ID, s.LabelMap.SHA256, s.Image.ID, s.Image.SHA256, *reportID, *resolvedID).Scan(&attested)
	if err != nil {
		return DefectPlan{}, err
	}
	if !attested {
		return DefectPlan{}, domain.NewError("STALE_DEFECT_REVIEW", 409)
	}
	selection, err := canonical.Marshal(plan.Selection)
	if err != nil {
		return DefectPlan{}, err
	}
	tag, err := tx.Exec(ctx, `UPDATE photo.reviews SET state='decided',decision=$3,decided_by=$4,decided_at=clock_timestamp(),
 revision=$5,selection=$6,decision_hash=$7,decision_route=$8 WHERE tenant_id=$1 AND id=$2 AND state='pending' AND revision=$9`,
		tenantID, reviewID, request.Decision, actor, plan.Revision, selection, plan.DecisionHash, plan.Route, s.Revision)
	if err != nil {
		return DefectPlan{}, err
	}
	if tag.RowsAffected() != 1 {
		return DefectPlan{}, domain.NewError("REVISION_CONFLICT", 409)
	}
	details, err := canonical.Marshal(map[string]any{"decision_hash": plan.DecisionHash, "decision": request.Decision, "route": plan.Route, "revision": plan.Revision, "candidate_hash": s.Proposal.SHA256})
	if err != nil {
		return DefectPlan{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details)
 VALUES($1,'user',$2,'review.decide','review',$3,$4,$5)`, tenantID, actor, reviewID, requestID, details)
	if err != nil {
		return DefectPlan{}, err
	}
	if err = gate.ApplyDefectDecision(ctx, tx, s, plan); err != nil {
		return DefectPlan{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return DefectPlan{}, err
	}
	return plan, nil
}

func PersistCandidateAuthorized(ctx context.Context, outer pgx.Tx, tenantID, reviewID, actor, requestID uuid.UUID, request Decision) (CandidatePlan, error) {
	return persistCandidateAuthorized(ctx, outer, tenantID, reviewID, actor, uuid.Nil, requestID, request, nil)
}

type negativeCandidateBinding struct{ params, base, candidate ArtifactRef }

func validNegativeCandidateBinding(binding *negativeCandidateBinding, s CandidateSnapshot) bool {
	return binding != nil && binding.params.valid() && binding.base.valid() && binding.base.ID == s.Base.ID && binding.candidate == s.Candidate && binding.candidate.valid()
}

func persistCandidateAuthorized(ctx context.Context, outer pgx.Tx, tenantID, reviewID, actor, sessionID, requestID uuid.UUID, request Decision, binding *negativeCandidateBinding) (CandidatePlan, error) {

	if outer == nil || tenantID == uuid.Nil || reviewID == uuid.Nil || actor == uuid.Nil || requestID == uuid.Nil {
		return CandidatePlan{}, domain.NewError("INTERNAL_ERROR", 500)
	}
	tx, err := outer.Begin(ctx)
	if err != nil {
		return CandidatePlan{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var s CandidateSnapshot
	s.TenantID, s.ReviewID = tenantID, reviewID
	var status string
	var cancelled, attested bool
	err = tx.QueryRow(ctx, `SELECT r.id,r.status,r.cancel_requested FROM photo.runs r JOIN photo.reviews v ON v.tenant_id=r.tenant_id AND v.run_id=r.id WHERE v.tenant_id=$1 AND v.id=$2 FOR UPDATE OF r`, tenantID, reviewID).Scan(&s.RunID, &status, &cancelled)
	if errors.Is(err, pgx.ErrNoRows) {
		return CandidatePlan{}, domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		return CandidatePlan{}, err
	}
	if cancelled || status != "waiting_review" {
		return CandidatePlan{}, domain.NewError("REVISION_CONFLICT", 409)
	}
	var taskID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT revision,state,kind,task_id,candidate_artifact_id,candidate_hash,base_artifact_id FROM photo.reviews WHERE tenant_id=$1 AND id=$2 AND run_id=$3 FOR UPDATE`, tenantID, reviewID, s.RunID).Scan(&s.Revision, &s.State, &s.Kind, &taskID, &s.Candidate.ID, &s.Candidate.SHA256, &s.Base.ID)
	if err != nil {
		return CandidatePlan{}, err
	}
	if s.Kind == "defects" {
		return CandidatePlan{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM photo.tasks t JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id JOIN photo.artifacts c ON c.tenant_id=t.tenant_id AND c.id=$4 AND c.created_by_task_id=t.id JOIN photo.artifacts b ON b.tenant_id=t.tenant_id AND b.id=$6 WHERE t.tenant_id=$1 AND t.id=$2 AND t.run_id=$3 AND t.state='succeeded' AND c.state='ready' AND c.sha256=$5 AND c.output_role='working_image' AND b.state='ready' AND b.media_type='application/json' AND t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',b.id::text,'sha256',b.sha256)) AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',c.id::text,'sha256',c.sha256)))`, tenantID, taskID, s.RunID, s.Candidate.ID, s.Candidate.SHA256, s.Base.ID).Scan(&attested)
	if err != nil {
		return CandidatePlan{}, err
	}
	if s.Kind == "optical.negative" {
		// The public persistence entry cannot skip the physical Params/source check.
		if !validNegativeCandidateBinding(binding, s) {
			return CandidatePlan{}, domain.NewError("STALE_CANDIDATE_REVIEW", 409)
		}
		err = tx.QueryRow(ctx, negativeCandidateAttestationSQL, tenantID, taskID, s.RunID, s.Candidate.ID, s.Candidate.SHA256, s.Base.ID, binding.params.ID, binding.params.SHA256).Scan(&attested)
		if err != nil {
			return CandidatePlan{}, err
		}
		var baseSHA string
		if err = tx.QueryRow(ctx, `SELECT sha256 FROM photo.artifacts WHERE tenant_id=$1 AND id=$2`, tenantID, s.Base.ID).Scan(&baseSHA); err != nil {
			return CandidatePlan{}, err
		}
		if baseSHA != binding.base.SHA256 {
			return CandidatePlan{}, domain.NewError("STALE_CANDIDATE_REVIEW", 409)
		}
	}
	if !attested && s.Kind == "render.compose" {
		err = tx.QueryRow(ctx, renderCandidateAttestationSQL, tenantID, taskID, s.RunID, s.Candidate.ID, s.Candidate.SHA256, s.Base.ID).Scan(&attested)
		if err != nil {
			return CandidatePlan{}, err
		}
	}
	if restorationPreviewKind(s.Kind) {
		var ready bool
		err = tx.QueryRow(ctx, `SELECT photo.v28_restoration_review_decision_ready($1,$2)`, tenantID, reviewID).Scan(&ready)
		if err != nil {
			return CandidatePlan{}, err
		}
		attested = ready
	}
	if s.Kind == "portrait.open_eyes" {
		if sessionID == uuid.Nil {
			return CandidatePlan{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
		}
		var ready bool
		err = tx.QueryRow(ctx, `SELECT photo.v28_open_eyes_review_decision_ready($1,$2,$3,$4)`, tenantID, reviewID, actor, sessionID).Scan(&ready)
		if err != nil {
			return CandidatePlan{}, err
		}
		attested = ready
	}
	if !attested {
		return CandidatePlan{}, domain.NewError("STALE_CANDIDATE_REVIEW", 409)
	}
	if err = tx.QueryRow(ctx, `SELECT sha256 FROM photo.artifacts WHERE tenant_id=$1 AND id=$2`, tenantID, s.Base.ID).Scan(&s.Base.SHA256); err != nil {
		return CandidatePlan{}, err
	}
	plan, err := PlanCandidate(s, actor, request)
	if err != nil {
		return CandidatePlan{}, err
	}
	selection, err := canonical.Marshal(plan.Selection)
	if err != nil {
		return CandidatePlan{}, err
	}
	if restorationPreviewKind(s.Kind) || s.Kind == "portrait.open_eyes" {
		if sessionID == uuid.Nil {
			return CandidatePlan{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
		}
		var decided bool
		decisionFunction := candidateDecisionFunction(s.Kind)
		err = tx.QueryRow(ctx, `SELECT `+decisionFunction+`($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9)`, tenantID, reviewID, actor, sessionID, s.Revision, request.Decision, string(selection), plan.DecisionHash, plan.Route).Scan(&decided)
		if err != nil {
			return CandidatePlan{}, err
		}
		if !decided {
			return CandidatePlan{}, domain.NewError("STALE_CANDIDATE_REVIEW", 409)
		}
	} else {
		tag, e := tx.Exec(ctx, `UPDATE photo.reviews SET state='decided',decision=$3,decided_by=$4,decided_at=clock_timestamp(),revision=$5,selection=$6,decision_hash=$7,decision_route=$8 WHERE tenant_id=$1 AND id=$2 AND state='pending' AND revision=$9`, tenantID, reviewID, request.Decision, actor, plan.Revision, selection, plan.DecisionHash, plan.Route, s.Revision)
		if e != nil {
			return CandidatePlan{}, e
		}
		if tag.RowsAffected() != 1 {
			return CandidatePlan{}, domain.NewError("REVISION_CONFLICT", 409)
		}
	}
	details, err := canonical.Marshal(map[string]any{"decision_hash": plan.DecisionHash, "decision": request.Decision, "route": plan.Route, "revision": plan.Revision, "candidate_hash": s.Candidate.SHA256, "kind": s.Kind})
	if err != nil {
		return CandidatePlan{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details) VALUES($1,'user',$2,'review.decide','review',$3,$4,$5)`, tenantID, actor, reviewID, requestID, details); err != nil {
		return CandidatePlan{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CandidatePlan{}, err
	}
	return plan, nil
}

func candidateDecisionFunction(kind string) string {
	if kind == "portrait.open_eyes" {
		return "photo.decide_v28_open_eyes_review"
	}
	return "photo.decide_v28_restoration_review"
}

func PersistDustAuthorized(ctx context.Context, outer pgx.Tx, tenantID, reviewID, actor, requestID uuid.UUID, proposal VerifiedDustProposal, request Decision) (DustPlan, error) {
	if outer == nil || tenantID == uuid.Nil || reviewID == uuid.Nil || actor == uuid.Nil || requestID == uuid.Nil {
		return DustPlan{}, domain.NewError("INTERNAL_ERROR", 500)
	}
	tx, err := outer.Begin(ctx)
	if err != nil {
		return DustPlan{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var s DustSnapshot
	s.TenantID, s.ReviewID = tenantID, reviewID
	var status string
	var cancelled, attested bool
	err = tx.QueryRow(ctx, `SELECT r.id,r.status,r.cancel_requested FROM photo.runs r JOIN photo.reviews v ON v.tenant_id=r.tenant_id AND v.run_id=r.id WHERE v.tenant_id=$1 AND v.id=$2 FOR UPDATE OF r`, tenantID, reviewID).Scan(&s.RunID, &status, &cancelled)
	if errors.Is(err, pgx.ErrNoRows) {
		return DustPlan{}, domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		return DustPlan{}, err
	}
	if cancelled || status != "waiting_review" {
		return DustPlan{}, domain.NewError("REVISION_CONFLICT", 409)
	}
	var taskID uuid.UUID
	var kind string
	err = tx.QueryRow(ctx, `SELECT revision,state,kind,task_id,candidate_artifact_id,candidate_hash,base_artifact_id FROM photo.reviews WHERE tenant_id=$1 AND id=$2 AND run_id=$3 FOR UPDATE`, tenantID, reviewID, s.RunID).Scan(&s.Revision, &s.State, &kind, &taskID, &s.Proposal.ID, &s.Proposal.SHA256, &s.Base.ID)
	if err != nil {
		return DustPlan{}, err
	}
	if kind != "sensor_dust" {
		return DustPlan{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM photo.tasks t JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id JOIN photo.artifacts c ON c.tenant_id=t.tenant_id AND c.id=$4 AND c.created_by_task_id=t.id JOIN photo.artifacts b ON b.tenant_id=t.tenant_id AND b.id=$6 WHERE t.tenant_id=$1 AND t.id=$2 AND t.run_id=$3 AND t.operation='sensor.dust' AND t.state='succeeded' AND c.state='ready' AND c.sha256=$5 AND c.output_role='dust_candidates' AND b.state='ready' AND b.media_type='application/json' AND t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',b.id::text,'sha256',b.sha256)) AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',c.id::text,'sha256',c.sha256)))`, tenantID, taskID, s.RunID, s.Proposal.ID, s.Proposal.SHA256, s.Base.ID).Scan(&attested)
	if err != nil {
		return DustPlan{}, err
	}
	if !attested {
		return DustPlan{}, domain.NewError("STALE_DEFECT_REVIEW", 409)
	}
	if err = tx.QueryRow(ctx, `SELECT sha256 FROM photo.artifacts WHERE tenant_id=$1 AND id=$2`, tenantID, s.Base.ID).Scan(&s.Base.SHA256); err != nil {
		return DustPlan{}, err
	}
	s, err = proposal.Bind(s)
	if err != nil {
		return DustPlan{}, domain.NewError("INPUT_INVALID", 422)
	}
	plan, err := PlanDust(s, actor, request)
	if err != nil {
		return DustPlan{}, err
	}
	selection, err := canonical.Marshal(plan.Selection)
	if err != nil {
		return DustPlan{}, err
	}
	tag, err := tx.Exec(ctx, `UPDATE photo.reviews SET state='decided',decision=$3,decided_by=$4,decided_at=clock_timestamp(),revision=$5,selection=$6,decision_hash=$7,decision_route=$8 WHERE tenant_id=$1 AND id=$2 AND state='pending' AND revision=$9`, tenantID, reviewID, request.Decision, actor, plan.Revision, selection, plan.DecisionHash, plan.Route, s.Revision)
	if err != nil {
		return DustPlan{}, err
	}
	if tag.RowsAffected() != 1 {
		return DustPlan{}, domain.NewError("REVISION_CONFLICT", 409)
	}
	details, err := canonical.Marshal(map[string]any{"decision_hash": plan.DecisionHash, "decision": request.Decision, "route": plan.Route, "revision": plan.Revision, "candidate_hash": s.Proposal.SHA256, "kind": kind})
	if err != nil {
		return DustPlan{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details) VALUES($1,'user',$2,'review.decide','review',$3,$4,$5)`, tenantID, actor, reviewID, requestID, details); err != nil {
		return DustPlan{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return DustPlan{}, err
	}
	return plan, nil
}
