package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

// OperatorDustDecision is an explicit operator choice, never a qualification
// receipt or a default approval. Its base is exactly detection Params.frames[0].
type OperatorDustDecision struct {
	Actor     uuid.UUID          `json:"actor"`
	Tenant    uuid.UUID          `json:"tenant"`
	Run       uuid.UUID          `json:"run"`
	Task      uuid.UUID          `json:"task"`
	ParamsSHA string             `json:"params_sha256"`
	Proposal  review.ArtifactRef `json:"proposal"`
	Base      review.ArtifactRef `json:"base"`
	Decision  review.Decision    `json:"decision"`
}
type OperatorDustReview struct {
	Ledger    *OperatorApplyLedger
	Objects   *OperatorPhysicalObjects
	Decisions map[uuid.UUID]OperatorDustDecision
}

func (r OperatorDustReview) MaterializeOperatorReview(ctx context.Context, l ApplyLease, inputs []applyInput, _ []OperatorMeasuredOutput) (OperatorReviewResult, error) {
	fail := func() (OperatorReviewResult, error) {
		return OperatorReviewResult{}, errors.New("operator dust measured proposal/decision binding invalid")
	}
	if r.Ledger == nil || r.Objects == nil || !r.Ledger.scope(l) || l.Phase != "dust_review" || l.StageIndex == nil || *l.StageIndex < 1 || *l.StageIndex >= len(l.FrozenPlan) {
		return fail()
	}
	previous := l.FrozenPlan[*l.StageIndex-1]
	if previous.Operation != "sensor.dust" || previous.Cardinality != "group" {
		return fail()
	}
	groups, e := compileApplyDustGroups(inputs)
	if e != nil {
		return OperatorReviewResult{}, e
	}
	nodes := []applyNodePlan{}
	for _, n := range r.Ledger.plans {
		if n.Phase == previous.Phase {
			nodes = append(nodes, n)
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Ordinal < nodes[j].Ordinal })
	if len(nodes) != len(groups) || len(nodes) == 0 {
		return fail()
	}
	result := OperatorReviewResult{Phase: l.Phase}
	hashes := []string{}
	for ordinal, node := range nodes {
		if e = ctx.Err(); e != nil {
			return result, e
		}
		if node.Ordinal != ordinal || node.Operation != "sensor.dust" {
			return fail()
		}
		group := groups[ordinal]
		var params struct {
			Frames []review.ArtifactRef `json:"frames"`
		}
		if json.Unmarshal(node.Params, &params) != nil || len(params.Frames) != len(group.Items) || len(params.Frames) < 5 {
			return fail()
		}
		task, ok := r.Ledger.tasks[node.ID]
		if !ok || task.Operation != node.Operation || task.Params.SHA256 != node.ParamsSHA {
			return fail()
		}
		// Rebind every frame to the actual measured image.color producer for the
		// exact frozen group. This is independent of a syntactically valid proposal.
		for i, item := range group.Items {
			count := 0
			for id, plan := range r.Ledger.plans {
				if plan.Operation == "image.color" && plan.SourceItemID == item.ItemID && plan.SourceAssetID == item.AssetID && plan.SourceVersionID == item.VersionID {
					for _, proof := range r.Ledger.outputs[id] {
						if proof.Role == "working_image" && proof.Artifact.ref() == params.Frames[i] {
							sourceTask, found := r.Ledger.tasks[id]
							if !found || proof.Producer != id {
								return fail()
							}
							if e = r.Objects.VerifyMeasuredObject(ctx, sourceTask, proof); e != nil {
								return result, e
							}
							count++
						}
					}
				}
			}
			declared := false
			for _, ref := range task.Inputs {
				if ref == params.Frames[i] {
					declared = true
				}
			}
			if count != 1 || !declared {
				return fail()
			}
		}
		count := 0
		var proposal OperatorMeasuredOutput
		for _, proof := range r.Ledger.outputs[node.ID] {
			if proof.Role == "dust_candidates" {
				proposal = proof
				count++
			}
		}
		if count != 1 || proposal.Producer != node.ID {
			return fail()
		}
		if e = r.Objects.VerifyMeasuredObject(ctx, task, proposal); e != nil {
			return result, e
		}
		decision, ok := r.Decisions[node.ID]
		if !ok || decision.Actor == uuid.Nil || decision.Tenant != l.TenantID || decision.Run != l.RunID || decision.Task != node.ID || decision.ParamsSHA != node.ParamsSHA || decision.Proposal != proposal.Artifact.ref() || decision.Base != params.Frames[0] {
			return fail()
		}
		raw, e := readApplyJSON(ctx, r.Objects, "operator-measurement", proposal.Artifact)
		if e != nil {
			return result, e
		}
		verified, e := review.ReadDustProposal(ctx, bytes.NewReader(raw), decision.Proposal, decision.Base)
		if e != nil {
			return result, e
		}
		snapshot, e := verified.Bind(review.DustSnapshot{TenantID: l.TenantID, RunID: l.RunID, ReviewID: uuid.NewSHA1(l.RunID, []byte("operator-dust/"+node.ID.String())), Revision: 1, State: "pending", Proposal: decision.Proposal, Base: decision.Base})
		if e != nil {
			return result, e
		}
		plan, e := review.PlanDust(snapshot, decision.Actor, decision.Decision)
		if e != nil {
			return result, e
		}
		selection, e := canonical.Marshal(plan.Selection)
		if e != nil {
			return result, e
		}
		result.Dust = append(result.Dust, applyDustDecision{GroupOrdinal: ordinal, TaskID: node.ID, Proposal: proposal.Artifact, Selection: selection, Route: plan.Route})
		hashes = append(hashes, plan.DecisionHash)
	}
	result.DecisionSHA256, e = canonical.Hash(hashes)
	return result, e
}

var _ OperatorReviewMaterializer = OperatorDustReview{}
