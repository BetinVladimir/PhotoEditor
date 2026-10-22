package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"sort"
)

// OperatorCandidateDecision is explicit human/operator input over retained
// measured proposal bytes. A module label or default accept is not a decision.
type OperatorCandidateDecision struct {
	Actor     uuid.UUID          `json:"actor"`
	Tenant    uuid.UUID          `json:"tenant"`
	Run       uuid.UUID          `json:"run"`
	Task      uuid.UUID          `json:"task"`
	Phase     string             `json:"phase"`
	ParamsSHA string             `json:"params_sha256"`
	Candidate review.ArtifactRef `json:"candidate"`
	Base      review.ArtifactRef `json:"base"`
	Decision  review.Decision    `json:"decision"`
}
type OperatorCandidateReview struct {
	Ledger    *OperatorApplyLedger
	Objects   *OperatorPhysicalObjects
	Decisions map[uuid.UUID]OperatorCandidateDecision
}

func (r OperatorCandidateReview) MaterializeOperatorReview(ctx context.Context, l ApplyLease, inputs []applyInput, ignored []OperatorMeasuredOutput) (OperatorReviewResult, error) {
	fail := func() (OperatorReviewResult, error) {
		return OperatorReviewResult{}, errors.New("operator candidate decision binding invalid")
	}
	if r.Ledger == nil || r.Objects == nil || !r.Ledger.scope(l) || l.StageIndex == nil || *l.StageIndex < 1 || *l.StageIndex >= len(l.FrozenPlan) {
		return fail()
	}
	kinds := map[string]string{"background_review": "background_generation", "inpaint_review": "inpaint", "generation_review": "generation", "composition_review": "composition"}
	kind := kinds[l.Phase]
	if kind == "" {
		return OperatorReviewResult{}, errors.New("operator exact repair/review compiler required")
	}
	previous := l.FrozenPlan[*l.StageIndex-1]
	allowed := map[string]bool{"background.generate": true, "image.inpaint": true, "image.generate": true, "image.compose": true, "geometry.crop": true}
	if !allowed[previous.Operation] {
		return fail()
	}
	nodes := []applyNodePlan{}
	for _, node := range r.Ledger.plans {
		if node.Phase == previous.Phase {
			nodes = append(nodes, node)
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Ordinal < nodes[j].Ordinal })
	if len(nodes) != l.ItemCount || len(inputs) != l.ItemCount {
		return fail()
	}
	result := OperatorReviewResult{Phase: l.Phase}
	hashes := []string{}
	seen := map[uuid.UUID]bool{}
	for _, node := range nodes {
		if e := ctx.Err(); e != nil {
			return result, e
		}
		if seen[node.SourceItemID] || node.Operation != previous.Operation {
			return fail()
		}
		seen[node.SourceItemID] = true
		bound := false
		for _, input := range inputs {
			if input.ItemID == node.SourceItemID && input.Ordinal == node.Ordinal && input.AssetID == node.SourceAssetID && input.VersionID == node.SourceVersionID {
				bound = true
			}
		}
		if !bound {
			return fail()
		}
		task, exists := r.Ledger.tasks[node.ID]
		if !exists || task.Operation != node.Operation {
			return fail()
		}
		proofs, exists := r.Ledger.outputs[node.ID]
		if !exists {
			return fail()
		}
		var candidate OperatorMeasuredOutput
		count := 0
		for _, proof := range proofs {
			if proof.Role == "working_image" {
				candidate = proof
				count++
			}
		}
		if count != 1 || candidate.Producer != node.ID {
			return fail()
		}
		if e := r.Objects.VerifyMeasuredObject(ctx, task, candidate); e != nil {
			return result, e
		}
		var params struct {
			Image review.ArtifactRef `json:"image"`
		}
		if json.Unmarshal(node.Params, &params) != nil {
			return fail()
		}
		base, exists := r.Ledger.objects[params.Image.ID]
		if !exists || base.SHA256 != params.Image.SHA256 {
			return fail()
		}
		declared := false
		for _, ref := range task.Inputs {
			if ref == params.Image {
				declared = true
			}
		}
		if !declared {
			return fail()
		}
		decision, exists := r.Decisions[node.ID]
		if !exists || decision.Actor == uuid.Nil || decision.Tenant != l.TenantID || decision.Run != l.RunID || decision.Task != node.ID || decision.Phase != l.Phase || decision.ParamsSHA != node.ParamsSHA || decision.Candidate != candidate.Artifact.ref() || decision.Base != base.ref() {
			return fail()
		}
		candidateImage, e := readWorking(ctx, r.Objects, "operator-measurement", candidate.Artifact)
		if e != nil {
			return result, e
		}
		baseImage, e := readWorking(ctx, r.Objects, "operator-measurement", base)
		if e != nil {
			return result, e
		}
		// readWorking validates physical canonical documents. Service's exact
		// candidate-pair rules remain authoritative, including composition geometry.
		candidateRaw, e := canonical.Marshal(candidateImage)
		if e != nil {
			return result, e
		}
		baseRaw, e := canonical.Marshal(baseImage)
		if e != nil {
			return result, e
		}
		if e = review.ValidateOperatorCandidatePair(kind, candidateRaw, baseRaw); e != nil {
			return result, e
		}
		snapshot := review.CandidateSnapshot{TenantID: l.TenantID, RunID: l.RunID, ReviewID: uuid.NewSHA1(l.RunID, []byte("operator-candidate/"+node.ID.String())), Revision: 1, State: "pending", Kind: kind, Candidate: candidate.Artifact.ref(), Base: base.ref()}
		plan, e := review.PlanCandidate(snapshot, decision.Actor, decision.Decision)
		if e != nil {
			return result, e
		}
		selected := base
		if plan.Route == "candidate" {
			selected = candidate.Artifact
		} else if plan.Route != "base" && plan.Route != "skip" {
			return fail()
		}
		result.Outputs = append(result.Outputs, applyOutput{Ordinal: node.Ordinal, ItemID: node.SourceItemID, TaskID: node.ID, Artifact: selected, Route: plan.Route})
		hashes = append(hashes, plan.DecisionHash)
	}
	digest, e := canonical.Hash(hashes)
	if e != nil {
		return result, e
	}
	result.DecisionSHA256 = digest
	return result, nil
}

var _ OperatorReviewMaterializer = OperatorCandidateReview{}
