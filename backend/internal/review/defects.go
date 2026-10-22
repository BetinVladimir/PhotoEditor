// Package review validates immutable user decisions before transaction routing.
package review

import (
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

const maxRevision int64 = 9007199254740991

type ArtifactRef struct {
	ID     uuid.UUID `json:"artifact_id"`
	SHA256 string    `json:"sha256"`
}

func (r ArtifactRef) valid() bool {
	_, err := domain.ParseDigest(r.SHA256)
	return r.ID != uuid.Nil && err == nil
}

type Decision struct {
 ViewedToken string `json:"viewed_token,omitempty"`
 viewedGrant *RelightViewedGrant
	ExpectedRevision int64    `json:"expected_revision"`
	CandidateHash    string   `json:"candidate_hash"`
	Decision         string   `json:"decision"`
	Accepted         []string `json:"accepted_candidate_ids,omitempty"`
	Rejected         []string `json:"rejected_candidate_ids,omitempty"`
}

// DefectSnapshot must come from a locked review row and a verified immutable
// proposal, never from a client-provided list of candidate IDs. The caller must
// enforce tenant/project editor authorization in the same transaction.
type DefectSnapshot struct {
	TenantID, ReviewID, RunID uuid.UUID
	Revision                  int64
	State                     string
	Proposal, Image, LabelMap ArtifactRef
	CandidateIDs              []string
}

// DefectSelection is the Python defect_review_v1 wire contract. Omitted
// candidates remain pending and cannot be repaired by this selection.
type DefectSelection struct {
	Version  string      `json:"version"`
	Proposal ArtifactRef `json:"proposal"`
	Image    ArtifactRef `json:"image"`
	LabelMap ArtifactRef `json:"label_map"`
	Revision int64       `json:"revision"`
	Accepted []string    `json:"accepted_candidate_ids"`
	Rejected []string    `json:"rejected_candidate_ids"`
}

type DefectPlan struct {
	Revision     int64
	Route        string // repair, base, or skip; caller applies the gate atomically.
	Selection    DefectSelection
	DecisionHash string
}

type CandidateSnapshot struct {
	TenantID, ReviewID, RunID uuid.UUID
	Revision                  int64
	State, Kind               string
	Candidate, Base           ArtifactRef
}

type CandidateSelection struct {
	Version   string      `json:"version"`
	Candidate ArtifactRef `json:"candidate"`
	Base      ArtifactRef `json:"base"`
	Revision  int64       `json:"revision"`
}

type CandidatePlan struct {
	Revision     int64
	Route        string
	Selection    CandidateSelection
	DecisionHash string
}

func PlanCandidate(snapshot CandidateSnapshot, actor uuid.UUID, request Decision) (CandidatePlan, error) {
	fail := func(code string, status int) (CandidatePlan, error) {
		return CandidatePlan{}, domain.NewError(code, status)
	}
	if actor == uuid.Nil || snapshot.TenantID == uuid.Nil || snapshot.ReviewID == uuid.Nil || snapshot.RunID == uuid.Nil ||
		snapshot.Revision < 1 || snapshot.Revision >= maxRevision || snapshot.State != "pending" ||
		!snapshot.Candidate.valid() || !snapshot.Base.valid() || snapshot.Kind == "" || snapshot.Kind == "defects" {
		return fail("INTERNAL_ERROR", 500)
	}
	if request.ExpectedRevision < 1 || request.ExpectedRevision > maxRevision ||
		(request.Decision != "accept" && request.Decision != "reject" && request.Decision != "skip") ||
		len(request.Accepted) != 0 || len(request.Rejected) != 0 {
		return fail("VALIDATION_FAILED", 422)
	}
	if request.ExpectedRevision != snapshot.Revision {
		return fail("REVISION_CONFLICT", 409)
	}
	if request.CandidateHash != snapshot.Candidate.SHA256 {
		return fail("STALE_CANDIDATE_REVIEW", 409)
	}
	route := map[string]string{"accept": "candidate", "reject": "base", "skip": "skip"}[request.Decision]
	selection := CandidateSelection{"candidate_review_v1", snapshot.Candidate, snapshot.Base, snapshot.Revision + 1}
	hash, err := canonical.Hash(struct {
		Version   string             `json:"version"`
		TenantID  uuid.UUID          `json:"tenant_id"`
		ReviewID  uuid.UUID          `json:"review_id"`
		RunID     uuid.UUID          `json:"run_id"`
		Actor     uuid.UUID          `json:"actor_id"`
		Kind      string             `json:"kind"`
		Decision  string             `json:"decision"`
		Selection CandidateSelection `json:"selection"`
	}{"candidate_decision_v1", snapshot.TenantID, snapshot.ReviewID, snapshot.RunID, actor, snapshot.Kind, request.Decision, selection})
	if err != nil {
		return CandidatePlan{}, err
	}
	return CandidatePlan{selection.Revision, route, selection, hash}, nil
}

// PlanDefects has no persistence side effects. It must not be exposed as an
// approval endpoint until review persistence, audit and gate routing share a tx.
func PlanDefects(snapshot DefectSnapshot, actor uuid.UUID, request Decision) (DefectPlan, error) {
	fail := func(code string, status int) (DefectPlan, error) { return DefectPlan{}, domain.NewError(code, status) }
	if actor == uuid.Nil || snapshot.TenantID == uuid.Nil || snapshot.ReviewID == uuid.Nil || snapshot.RunID == uuid.Nil ||
		snapshot.Revision < 1 || snapshot.Revision >= maxRevision || !snapshot.Proposal.valid() || !snapshot.Image.valid() || !snapshot.LabelMap.valid() || len(snapshot.CandidateIDs) > 4096 {
		return fail("INTERNAL_ERROR", 500)
	}
	if snapshot.State != "pending" {
		return fail("REVISION_CONFLICT", 409)
	}
	if request.ExpectedRevision < 1 || request.ExpectedRevision > maxRevision {
		return fail("VALIDATION_FAILED", 422)
	}
	if request.ExpectedRevision != snapshot.Revision {
		return fail("REVISION_CONFLICT", 409)
	}
	if _, err := domain.ParseDigest(request.CandidateHash); err != nil {
		return fail("VALIDATION_FAILED", 422)
	}
	if request.CandidateHash != snapshot.Proposal.SHA256 {
		return fail("STALE_DEFECT_REVIEW", 409)
	}
	if request.Decision != "accept" && request.Decision != "reject" && request.Decision != "skip" {
		return fail("VALIDATION_FAILED", 422)
	}
	if len(request.Accepted) > 4096 || len(request.Rejected) > 4096 {
		return fail("VALIDATION_FAILED", 422)
	}
	if request.Decision != "accept" && (len(request.Accepted) > 0 || len(request.Rejected) > 0) {
		return fail("VALIDATION_FAILED", 422)
	}
	known := make(map[string]bool, len(snapshot.CandidateIDs))
	for _, id := range snapshot.CandidateIDs {
		if _, err := domain.ParseDigest(id); err != nil || known[id] {
			return fail("INTERNAL_ERROR", 500)
		}
		known[id] = true
	}
	seen := make(map[string]bool, len(request.Accepted)+len(request.Rejected))
	for _, set := range [][]string{request.Accepted, request.Rejected} {
		for _, id := range set {
			if _, err := domain.ParseDigest(id); err != nil || seen[id] {
				return fail("VALIDATION_FAILED", 422)
			}
			if !known[id] {
				return fail("UNKNOWN_DEFECT_CANDIDATE", 422)
			}
			seen[id] = true
		}
	}
	selection := DefectSelection{Version: "defect_review_v1", Proposal: snapshot.Proposal, Image: snapshot.Image, LabelMap: snapshot.LabelMap,
		Revision: snapshot.Revision + 1, Accepted: append([]string{}, request.Accepted...), Rejected: append([]string{}, request.Rejected...)}
	sort.Strings(selection.Accepted)
	sort.Strings(selection.Rejected)
	route := "base"
	if request.Decision == "skip" {
		route = "skip"
	} else if request.Decision == "accept" && len(selection.Accepted) > 0 {
		route = "repair"
	}
	hash, err := canonical.Hash(struct {
		Version   string          `json:"version"`
		TenantID  uuid.UUID       `json:"tenant_id"`
		ReviewID  uuid.UUID       `json:"review_id"`
		RunID     uuid.UUID       `json:"run_id"`
		Actor     uuid.UUID       `json:"actor_id"`
		Decision  string          `json:"decision"`
		Selection DefectSelection `json:"selection"`
	}{"defect_decision_v1", snapshot.TenantID, snapshot.ReviewID, snapshot.RunID, actor, request.Decision, selection})
	if err != nil {
		return DefectPlan{}, err
	}
	return DefectPlan{Revision: selection.Revision, Route: route, Selection: selection, DecisionHash: hash}, nil
}
