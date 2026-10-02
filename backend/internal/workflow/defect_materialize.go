package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

type ContractPublisher interface {
	Publish(context.Context, artifactjson.Scope, []byte) (artifactjson.Ref, error)
}

// DefectExpansionInput is loaded from a claimed expansion and immutable saved
// review, not from HTTP. ResolvedImage/Alpha come from verified image contracts.
type DefectExpansionInput struct {
	TenantID, RunID, ExpansionID, ReviewID uuid.UUID
	Route                                  string
	Proposal                               review.VerifiedProposal
	Selection                              review.DefectSelection
	Resolved, ResolvedImage                review.ArtifactRef
	Protection                             review.ProtectionMask
	Alpha                                  *review.ArtifactRef
}
type MaterializedDefectBranch struct {
	Route             string
	BaseImage         *review.ArtifactRef
	Selection, Params *review.ArtifactRef
	Inputs            []review.ArtifactRef
}

// MaterializeDefectBranch publishes selection then task params under stable
// owner scopes. It does not hold DB locks over object IO. The expansion compiler
// must subsequently recheck its lease/run/decision and atomically insert the
// task or bind the base/skip route before sealing the barrier.
func MaterializeDefectBranch(ctx context.Context, publisher ContractPublisher, input DefectExpansionInput) (MaterializedDefectBranch, error) {
	fail := func() (MaterializedDefectBranch, error) {
		return MaterializedDefectBranch{}, errors.New("invalid defect expansion")
	}
	if publisher == nil || input.TenantID == uuid.Nil || input.RunID == uuid.Nil || input.ExpansionID == uuid.Nil || input.ReviewID == uuid.Nil || input.Selection.Revision < 2 || input.Selection.Revision > 9007199254740991 || input.Selection.Version != "defect_review_v1" || input.Selection.Accepted == nil || input.Selection.Rejected == nil || len(input.Selection.Accepted) > 4096 || len(input.Selection.Rejected) > 4096 {
		return fail()
	}
	if err := ctx.Err(); err != nil {
		return MaterializedDefectBranch{}, err
	}
	bound, err := input.Proposal.Bind(review.DefectSnapshot{Proposal: input.Selection.Proposal, Image: input.Selection.Image, LabelMap: input.Selection.LabelMap})
	if err != nil {
		return fail()
	}
	known := map[string]bool{}
	for _, id := range bound.CandidateIDs {
		known[id] = true
	}
	seen := map[string]bool{}
	for _, list := range [][]string{input.Selection.Accepted, input.Selection.Rejected} {
		for _, id := range list {
			if !known[id] || seen[id] {
				return fail()
			}
			seen[id] = true
		}
	}
	switch input.Route {
	case "base", "skip":
		if len(input.Selection.Accepted) != 0 {
			return fail()
		}
		out := MaterializedDefectBranch{Route: input.Route, Inputs: []review.ArtifactRef{}}
		if input.Route == "base" {
			ref := input.Selection.Image
			out.BaseImage = &ref
		}
		return out, nil
	case "repair":
	default:
		return fail()
	}
	selectionBody, err := canonical.Marshal(input.Selection)
	if err != nil {
		return fail()
	}
	hash := sha256.Sum256(selectionBody)
	// Validate every repair binding before publishing anything. This temporary ID
	// is used only for validation and is replaced by the repository-owned ID.
	checkRef := review.ArtifactRef{ID: input.ReviewID, SHA256: hex.EncodeToString(hash[:])}
	if _, err = review.BuildRepairDocument(input.Proposal, input.Selection, checkRef, input.Resolved, input.ResolvedImage, input.Protection, input.Alpha); err != nil {
		return fail()
	}
	selection, err := publisher.Publish(ctx, artifactjson.Scope{Tenant: input.TenantID, Run: input.RunID, Owner: input.ReviewID, Purpose: "review_selection"}, selectionBody)
	if err != nil {
		return MaterializedDefectBranch{}, err
	}
	selectionRef := review.ArtifactRef{ID: selection.ID, SHA256: selection.SHA256}
	document, err := review.BuildRepairDocument(input.Proposal, input.Selection, selectionRef, input.Resolved, input.ResolvedImage, input.Protection, input.Alpha)
	if err != nil {
		return MaterializedDefectBranch{}, err
	}
	params, err := publisher.Publish(ctx, artifactjson.Scope{Tenant: input.TenantID, Run: input.RunID, Owner: input.ExpansionID, Purpose: "task_params"}, document.Params)
	if err != nil {
		return MaterializedDefectBranch{}, err
	}
	expected := sha256.Sum256(document.Params)
	if params.ID == uuid.Nil || params.SHA256 != hex.EncodeToString(expected[:]) {
		return fail()
	}
	paramsRef := review.ArtifactRef{ID: params.ID, SHA256: params.SHA256}
	return MaterializedDefectBranch{Route: "repair", Selection: &selectionRef, Params: &paramsRef, Inputs: document.Inputs}, nil
}
