package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/review"
	"reflect"
)

type OperatorDefectDecision struct {
	Actor, Tenant, Run, ParentTask uuid.UUID
	ParamsSHA                      string
	Proposal, Image, Labels        review.ArtifactRef
	Decision                       review.Decision
}
type OperatorDefectCompilation struct {
	Plan   review.DefectPlan
	Branch MaterializedDefectBranch
	Task   *PreparedTask
	Parent applyNodePlan
}

// CompileOperatorDefectRepair uses actual proposal/label/protection validators,
// PlanDefects, MaterializeDefectBranch and PrepareDefectTask. The operator
// selection is immutable local measurement input, NOT a saved SQL review or a
// claim that normal client/expansion authorization has occurred.
func CompileOperatorDefectRepair(ctx context.Context, ledger *OperatorApplyLedger, objects *OperatorPhysicalObjects, l ApplyLease, parentID uuid.UUID, decision OperatorDefectDecision) (OperatorDefectCompilation, error) {
	var result OperatorDefectCompilation
	invalid := errors.New("operator defect proposal/decision binding invalid")
	if ledger == nil || objects == nil || !ledger.scope(l) || l.Phase != "defects_review" || decision.Actor == uuid.Nil || decision.Tenant != l.TenantID || decision.Run != l.RunID || decision.ParentTask != parentID {
		return result, invalid
	}
	parent, ok := ledger.plans[parentID]
	if !ok || parent.Operation != "defects.propose" || decision.ParamsSHA != parent.ParamsSHA {
		return result, invalid
	}
	result.Parent = parent
	task, ok := ledger.tasks[parentID]
	if !ok {
		return result, invalid
	}
	proofFor := func(role string) (OperatorMeasuredOutput, error) {
		var out OperatorMeasuredOutput
		count := 0
		for _, proof := range ledger.outputs[parentID] {
			if proof.Role == role {
				out = proof
				count++
			}
		}
		if count != 1 {
			return out, invalid
		}
		if e := objects.VerifyMeasuredObject(ctx, task, out); e != nil {
			return out, e
		}
		return out, nil
	}
	proposalProof, e := proofFor("defect_candidates")
	if e != nil {
		return result, e
	}
	labels, e := proofFor("defect_labels")
	if e != nil {
		return result, e
	}
	var params struct {
		Image review.ArtifactRef `json:"image"`
	}
	if json.Unmarshal(parent.Params, &params) != nil {
		return result, invalid
	}
	imageArtifact, ok := ledger.objects[params.Image.ID]
	if !ok || imageArtifact.ref() != params.Image || decision.Image != params.Image || decision.Proposal != proposalProof.Artifact.ref() || decision.Labels != labels.Artifact.ref() {
		return result, invalid
	}
	declared := false
	for _, ref := range task.Inputs {
		if ref == params.Image {
			declared = true
		}
	}
	if !declared {
		return result, invalid
	}
	image, e := readWorking(ctx, objects, "operator-measurement", imageArtifact)
	if e != nil {
		return result, e
	}
	raw, e := readApplyJSON(ctx, objects, "operator-measurement", proposalProof.Artifact)
	if e != nil {
		return result, e
	}
	proposal, e := review.ReadDefectProposal(ctx, bytes.NewReader(raw), proposalProof.Artifact.ref(), image.Source(imageArtifact.ref()))
	if e != nil {
		return result, e
	}
	labelReader, e := objects.Open(ctx, labels.Artifact.object())
	if e != nil {
		return result, e
	}
	_, labelErr := proposal.VerifyLabelMap(ctx, labelReader, labels.Artifact.Bytes)
	closeErr := labelReader.Close()
	if labelErr != nil {
		return result, labelErr
	}
	if closeErr != nil {
		return result, closeErr
	}
	reviewID := uuid.NewSHA1(l.RunID, []byte("operator-defect/"+parentID.String()))
	snapshot, e := proposal.Bind(review.DefectSnapshot{TenantID: l.TenantID, RunID: l.RunID, ReviewID: reviewID, Revision: 1, State: "pending", Proposal: proposalProof.Artifact.ref(), Image: imageArtifact.ref(), LabelMap: labels.Artifact.ref()})
	if e != nil {
		return result, e
	}
	plan, e := review.PlanDefects(snapshot, decision.Actor, decision.Decision)
	if e != nil {
		return result, e
	}
	result.Plan = plan
	expansionID := uuid.NewSHA1(reviewID, []byte("operator-measured-repair"))
	input := DefectExpansionInput{TenantID: l.TenantID, RunID: l.RunID, ReviewID: reviewID, ExpansionID: expansionID, Route: plan.Route, Proposal: proposal, Selection: plan.Selection, Alpha: image.Alpha}
	allowed := map[artifactjson.Scope][]byte{}
	if plan.Route == "repair" {
		report, e := proofFor("defect_propose_report")
		if e != nil {
			return result, e
		}
		reportRaw, e := readApplyJSON(ctx, objects, "operator-measurement", report.Artifact)
		if e != nil {
			return result, e
		}
		protection, e := review.ParseProposalProtection(reportRaw, proposalProof.Artifact.ref(), image.Source(imageArtifact.ref()))
		if e != nil {
			return result, e
		}
		protectionProof, e := proofFor("defect_protection")
		if e != nil || protectionProof.Artifact.ref() != protection.Artifact {
			return result, invalid
		}
		labelStream, e := objects.Open(ctx, labels.Artifact.object())
		if e != nil {
			return result, e
		}
		defer labelStream.Close()
		protectionStream, e := objects.Open(ctx, protectionProof.Artifact.object())
		if e != nil {
			return result, e
		}
		defer protectionStream.Close()
		if _, e = proposal.VerifyProtectedLabelMap(ctx, masknpy.PinnedMask{Reader: labelStream, Bytes: labels.Artifact.Bytes, SHA256: labels.Artifact.SHA256}, masknpy.PinnedMask{Reader: protectionStream, Bytes: protectionProof.Artifact.Bytes, SHA256: protectionProof.Artifact.SHA256}); e != nil {
			return result, e
		}
		var resolvedProof OperatorMeasuredOutput
		count := 0
		for id, node := range ledger.plans {
			if node.Ordinal == parent.Ordinal && node.SourceItemID == parent.SourceItemID && node.Operation == "style.resolve" {
				for _, proof := range ledger.outputs[id] {
					if proof.Role == "resolved_style" {
						resolvedProof = proof
						count++
					}
				}
			}
		}
		if count != 1 {
			return result, invalid
		}
		resolvedTask, ok := ledger.tasks[resolvedProof.Producer]
		if !ok {
			return result, invalid
		}
		if e = objects.VerifyMeasuredObject(ctx, resolvedTask, resolvedProof); e != nil {
			return result, e
		}
		resolvedRaw, e := readApplyJSON(ctx, objects, "operator-measurement", resolvedProof.Artifact)
		if e != nil {
			return result, e
		}
		resolved, e := review.ParseResolvedBinding(resolvedRaw)
		if e != nil || resolved.Mode == "skip" {
			return result, invalid
		}
		originArtifact, ok := ledger.objects[resolved.Image.ID]
		if !ok || originArtifact.ref() != resolved.Image {
			return result, invalid
		}
		origin, e := readWorking(ctx, objects, "operator-measurement", originArtifact)
		if e != nil {
			return result, e
		}
		if image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" || image.Color != origin.Color || image.Canvas.SourceSHA256 != origin.Canvas.SourceSHA256 {
			return result, invalid
		}
		input.Resolved = resolvedProof.Artifact.ref()
		input.ResolvedImage = resolved.Image
		input.Protection = protection
		selectionRaw, e := canonical.Marshal(plan.Selection)
		if e != nil {
			return result, e
		}
		selectionScope := artifactjson.Scope{Tenant: l.TenantID, Run: l.RunID, Owner: reviewID, Purpose: "review_selection"}
		selectionRef := operatorCompiledDocumentRef(selectionScope, selectionRaw)
		document, e := review.BuildRepairDocument(proposal, plan.Selection, review.ArtifactRef{ID: selectionRef.ID, SHA256: selectionRef.SHA256}, input.Resolved, input.ResolvedImage, input.Protection, input.Alpha)
		if e != nil {
			return result, e
		}
		allowed[selectionScope] = selectionRaw
		allowed[artifactjson.Scope{Tenant: l.TenantID, Run: l.RunID, Owner: expansionID, Purpose: "task_params"}] = document.Params
	}
	publisher := operatorReviewDocuments{ledger: ledger, objects: objects, allowed: allowed}
	branch, e := MaterializeDefectBranch(ctx, publisher, input)
	if e != nil {
		return result, e
	}
	result.Branch = branch
	if branch.Route != "repair" {
		return result, nil
	}
	sizes := []ArtifactSize{}
	for _, ref := range append(append([]review.ArtifactRef{}, branch.Inputs...), *branch.Params) {
		a, ok := ledger.objects[ref.ID]
		if !ok || a.ref() != ref {
			return result, invalid
		}
		sizes = append(sizes, ArtifactSize{Ref: ref, Bytes: a.Bytes})
	}
	prepared, e := PrepareDefectTask(l.RunID, reviewID, image.Canvas, branch, sizes)
	if e != nil {
		return result, e
	}
	result.Task = &prepared
	if len(prepared.Inputs) != len(branch.Inputs) || !reflect.DeepEqual(prepared.Params, *branch.Params) {
		return result, invalid
	}
	return result, nil
}
