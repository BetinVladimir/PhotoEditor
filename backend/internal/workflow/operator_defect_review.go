package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"sort"
)

var errOperatorReviewTasksPending = errors.New("operator review repair tasks pending measured execution")

type OperatorDefectReview struct {
	Ledger    *OperatorApplyLedger
	Objects   *OperatorPhysicalObjects
	Decisions map[uuid.UUID]OperatorDefectDecision
}

func (r OperatorDefectReview) MaterializeOperatorReview(ctx context.Context, l ApplyLease, inputs []applyInput, ignored []OperatorMeasuredOutput) (OperatorReviewResult, error) {
	result := OperatorReviewResult{Phase: l.Phase}
	if r.Ledger == nil || r.Objects == nil || !r.Ledger.scope(l) || l.Phase != "defects_review" || l.StageIndex == nil || *l.StageIndex < 1 || *l.StageIndex >= len(l.FrozenPlan) {
		return result, errors.New("operator defect review scope invalid")
	}
	previous := l.FrozenPlan[*l.StageIndex-1]
	if previous.Operation != "defects.propose" {
		return result, errors.New("operator defect stage order changed")
	}
	nodes := []applyNodePlan{}
	for _, node := range r.Ledger.plans {
		if node.Phase == previous.Phase {
			nodes = append(nodes, node)
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Ordinal < nodes[j].Ordinal })
	if len(nodes) != l.ItemCount || len(inputs) != l.ItemCount {
		return result, errors.New("operator defect item coverage invalid")
	}
	hashes := []string{}
	pending := false
	seen := map[uuid.UUID]bool{}
	for _, node := range nodes {
		bound := false
		for _, input := range inputs {
			if input.ItemID == node.SourceItemID && input.Ordinal == node.Ordinal && input.AssetID == node.SourceAssetID && input.VersionID == node.SourceVersionID {
				bound = true
			}
		}
		if !bound || seen[node.SourceItemID] {
			return result, errors.New("operator defect source item changed")
		}
		seen[node.SourceItemID] = true
		decision, exists := r.Decisions[node.ID]
		if !exists {
			return result, errors.New("explicit measured-proposal operator decision required")
		}
		compilation, e := CompileOperatorDefectRepair(ctx, r.Ledger, r.Objects, l, node.ID, decision)
		if e != nil {
			return result, e
		}
		hashes = append(hashes, compilation.Plan.DecisionHash)
		output := applyOutput{Ordinal: node.Ordinal, ItemID: node.SourceItemID, TaskID: node.ID, Route: compilation.Plan.Route}
		if compilation.Task == nil {
			// SQL's base/skip continuation retains the proposal's exact input image.
			output.Artifact = r.Ledger.objects[decision.Image.ID]
		} else {
			prepared := *compilation.Task
			raw, e := readApplyJSON(ctx, r.Objects, "operator-measurement", r.Ledger.objects[prepared.Params.ID])
			if e != nil {
				return result, e
			}
			plan := applyNodePlan{ID: prepared.ID, Phase: l.Phase, Ordinal: node.Ordinal, SourceItemID: node.SourceItemID, SourceAssetID: node.SourceAssetID, SourceVersionID: node.SourceVersionID, NodeKey: prepared.NodeKey, Operation: prepared.Operation, Inputs: prepared.Inputs, Prerequisites: []uuid.UUID{node.ID}, ParamsSHA: prepared.Params.SHA256, Params: raw}
			registered, e := r.Ledger.Register(ctx, l, plan)
			if e != nil || !registered {
				return result, errors.New("operator compiled repair registration rejected")
			}
			materialized, e := r.Ledger.Materialize(ctx, l, prepared)
			if e != nil || !materialized {
				return result, errors.New("operator compiled repair task changed")
			}
			proofs, done := r.Ledger.outputs[prepared.ID]
			if !done {
				pending = true
				continue
			}
			count := 0
			for _, proof := range proofs {
				if proof.Role == "working_image" {
					if e = r.Objects.VerifyMeasuredObject(ctx, prepared, proof); e != nil {
						return result, e
					}
					output.Artifact = proof.Artifact
					output.TaskID = prepared.ID
					count++
				}
			}
			if count != 1 {
				return result, errors.New("operator repair lacks unique measured WorkingImage")
			}
		}
		result.Outputs = append(result.Outputs, output)
	}
	if pending {
		return result, errOperatorReviewTasksPending
	}
	digest, e := canonical.Hash(hashes)
	if e != nil {
		return result, e
	}
	result.DecisionSHA256 = digest
	return result, nil
}

var _ OperatorReviewMaterializer = OperatorDefectReview{}
