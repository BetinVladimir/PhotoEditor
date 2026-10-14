package interchange

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// ImportReviewDecisionRequest is a new decision about a prepared proposal in
// the target version. The archived review is deliberately absent from this
// request: it is evidence for remapping, never authority for a grant.
type ImportReviewDecisionRequest struct {
	Action          string       `json:"action"`
	Kind            string       `json:"kind"`
	EntryID         uuid.UUID    `json:"entry_id"`
	TargetVersionID uuid.UUID    `json:"target_version_id"`
	Proposal        v2input.Ref  `json:"proposal"`
	Base            v2input.Ref  `json:"base"`
	LabelMap        *v2input.Ref `json:"label_map,omitempty"`
	ExpectedEpoch   int64        `json:"expected_epoch"`
	Decision        string       `json:"decision,omitempty"`
	Accepted        []string     `json:"accepted_candidate_ids,omitempty"`
	Rejected        []string     `json:"rejected_candidate_ids,omitempty"`
}

func validateImportReviewDecisionRequest(ctx context.Context, r ImportReviewDecisionRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	invalid := func() error { return domain.NewError("VALIDATION_FAILED", 422) }
	if r.EntryID == uuid.Nil || r.TargetVersionID == uuid.Nil || !validImportRef(r.Proposal) || !validImportRef(r.Base) ||
		r.Proposal.ArtifactID == r.Base.ArtifactID || r.ExpectedEpoch < 0 || r.ExpectedEpoch >= 9007199254740991 {
		return invalid()
	}
	switch r.Kind {
	case "defects":
		if r.LabelMap == nil || !validImportRef(*r.LabelMap) || r.LabelMap.ArtifactID == r.Proposal.ArtifactID || r.LabelMap.ArtifactID == r.Base.ArtifactID {
			return invalid()
		}
	case "dust":
		if r.LabelMap != nil {
			return invalid()
		}
	default:
		return invalid()
	}
	if r.Action == "revoke" {
		if r.ExpectedEpoch < 1 || r.Decision != "" || len(r.Accepted) != 0 || len(r.Rejected) != 0 {
			return invalid()
		}
		return ctx.Err()
	}
	if r.Action != "grant" || (r.Decision != "accept" && r.Decision != "reject" && r.Decision != "skip") ||
		len(r.Accepted)+len(r.Rejected) > 4096 || r.Decision != "accept" && (len(r.Accepted) != 0 || len(r.Rejected) != 0) {
		return invalid()
	}
	seen := make(map[string]bool, len(r.Accepted)+len(r.Rejected))
	for _, set := range [][]string{r.Accepted, r.Rejected} {
		last := ""
		for _, id := range set {
			if _, err := domain.ParseDigest(id); err != nil || id <= last || seen[id] {
				return invalid()
			}
			seen[id] = true
			last = id
		}
	}
	return ctx.Err()
}
