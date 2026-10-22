package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"sort"
)

var errOperatorApprovalPending = errors.New("explicit operator review approval pending")

// OperatorFileReviews emits the real measured challenge and waits for an
// external operator reply. No labels/goldens create default accepted decisions.
// The whole measurement deadline includes this human barrier.
type OperatorFileReviews struct {
	Ledger          *OperatorApplyLedger
	Objects         *OperatorPhysicalObjects
	Directory       string
	Actor, Snapshot uuid.UUID
	SnapshotDigest  string
}
type operatorApproval struct {
	Schema       int                         `json:"schema_version"`
	ChallengeSHA string                      `json:"challenge_sha256"`
	Actor        uuid.UUID                   `json:"actor_id"`
	Phase        string                      `json:"phase"`
	Candidates   []OperatorCandidateDecision `json:"candidates"`
	Defects      []OperatorDefectDecision    `json:"defects"`
	Dust         []OperatorDustDecision      `json:"dust"`
	Culling      *review.CullingDecision     `json:"culling"`
}

func (r *OperatorFileReviews) MaterializeOperatorReview(ctx context.Context, l ApplyLease, inputs []applyInput, outputs []OperatorMeasuredOutput) (OperatorReviewResult, error) {
	fail := func() (OperatorReviewResult, error) {
		return OperatorReviewResult{}, errors.New("operator approval actor/task/inventory binding invalid")
	}
	if r == nil || r.Ledger == nil || r.Objects == nil || r.Actor == uuid.Nil || r.Snapshot == uuid.Nil || !r.Ledger.scope(l) || l.StageIndex == nil || *l.StageIndex < 1 || *l.StageIndex >= len(l.FrozenPlan) {
		return fail()
	}
	info, e := os.Lstat(r.Directory)
	if e != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return fail()
	}
	// Current defect-repair nodes must not reissue the preceding proposal challenge.
	previous := l.FrozenPlan[*l.StageIndex-1]
	nodes := []applyNodePlan{}
	proofs := []operatorObservationObject{}
	for id, n := range r.Ledger.plans {
		if n.Phase != previous.Phase {
			continue
		}
		nodes = append(nodes, n)
		for _, p := range r.Ledger.outputs[id] {
			t, ok := r.Ledger.tasks[id]
			if !ok {
				return fail()
			}
			if e = r.Objects.VerifyMeasuredObject(ctx, t, p); e != nil {
				return OperatorReviewResult{}, e
			}
			path, e := r.Objects.path(p.Artifact.Key)
			if e != nil {
				return OperatorReviewResult{}, e
			}
			contract := p.Contract
			proofs = append(proofs, operatorObservationObject{ArtifactID: p.Artifact.ArtifactID.String(), SHA256: p.Artifact.SHA256, Bytes: p.Artifact.Bytes, MediaType: p.Artifact.MediaType, Role: p.Role, Contract: &contract, Path: path})
		}
	}
	if len(nodes) == 0 || len(proofs) == 0 {
		return fail()
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID.String() < nodes[j].ID.String() })
	sort.Slice(proofs, func(i, j int) bool { return proofs[i].ArtifactID < proofs[j].ArtifactID })
	challenge := struct {
		Schema             int    `json:"schema_version"`
		Boundary           string `json:"boundary"`
		Tenant, Run, Actor uuid.UUID
		Phase              string
		Nodes              []applyNodePlan
		Sources            []applyInput
		Objects            []operatorObservationObject
	}{1, "explicit_operator_decision_no_sql_authority", l.TenantID, l.RunID, r.Actor, l.Phase, nodes, inputs, proofs}
	raw, e := canonical.Marshal(challenge)
	if e != nil {
		return OperatorReviewResult{}, e
	}
	hash, e := canonical.Hash(json.RawMessage(raw))
	if e != nil {
		return OperatorReviewResult{}, e
	}
	path := filepath.Join(r.Directory, hash+".challenge.json")
	e = writeOperatorExclusive(path, raw)
	if errors.Is(e, os.ErrExist) {
		retained, readErr := readOperatorPrivateJSON(path, 16<<20)
		if readErr != nil {
			return OperatorReviewResult{}, readErr
		}
		if string(retained) != string(raw) {
			return fail()
		}
	} else if e != nil {
		return OperatorReviewResult{}, e
	}
	raw, e = readOperatorPrivateJSON(filepath.Join(r.Directory, hash+".approval.json"), 8<<20)
	if errors.Is(e, os.ErrNotExist) {
		return OperatorReviewResult{}, errOperatorApprovalPending
	}
	if e != nil {
		return OperatorReviewResult{}, e
	}
	var a operatorApproval
	if e = decodeOperatorClosed(raw, &a); e != nil {
		return OperatorReviewResult{}, e
	}
	if a.Schema != 1 || a.ChallengeSHA != hash || a.Actor != r.Actor || a.Phase != l.Phase {
		return fail()
	}
	expected := map[uuid.UUID]bool{}
	for _, n := range nodes {
		expected[n.ID] = true
	}
	switch l.Phase {
	case "dust_review":
		if len(a.Candidates) != 0 || len(a.Defects) != 0 || a.Culling != nil || len(a.Dust) != len(nodes) {
			return fail()
		}
		ds := map[uuid.UUID]OperatorDustDecision{}
		for _, d := range a.Dust {
			if !expected[d.Task] || d.Actor != r.Actor {
				return fail()
			}
			if _, ok := ds[d.Task]; ok {
				return fail()
			}
			ds[d.Task] = d
		}
		return (OperatorDustReview{Ledger: r.Ledger, Objects: r.Objects, Decisions: ds}).MaterializeOperatorReview(ctx, l, inputs, outputs)
	case "defects_review":
		if len(a.Candidates) != 0 || len(a.Dust) != 0 || a.Culling != nil || len(a.Defects) != len(nodes) {
			return fail()
		}
		ds := map[uuid.UUID]OperatorDefectDecision{}
		for _, d := range a.Defects {
			if !expected[d.ParentTask] || d.Actor != r.Actor {
				return fail()
			}
			if _, ok := ds[d.ParentTask]; ok {
				return fail()
			}
			ds[d.ParentTask] = d
		}
		return (OperatorDefectReview{Ledger: r.Ledger, Objects: r.Objects, Decisions: ds}).MaterializeOperatorReview(ctx, l, inputs, outputs)
	case "cull_review":
		if len(a.Candidates) != 0 || len(a.Dust) != 0 || len(a.Defects) != 0 || a.Culling == nil {
			return fail()
		}
		return (OperatorCullingReview{Ledger: r.Ledger, Objects: r.Objects, Actor: r.Actor, Snapshot: r.Snapshot, SnapshotDigest: r.SnapshotDigest, Decision: *a.Culling}).MaterializeOperatorReview(ctx, l, inputs, outputs)
	case "background_review", "inpaint_review", "generation_review", "composition_review":
		if len(a.Defects) != 0 || len(a.Dust) != 0 || a.Culling != nil || len(a.Candidates) != len(nodes) {
			return fail()
		}
		ds := map[uuid.UUID]OperatorCandidateDecision{}
		for _, d := range a.Candidates {
			if !expected[d.Task] || d.Actor != r.Actor {
				return fail()
			}
			if _, ok := ds[d.Task]; ok {
				return fail()
			}
			ds[d.Task] = d
		}
		return (OperatorCandidateReview{Ledger: r.Ledger, Objects: r.Objects, Decisions: ds}).MaterializeOperatorReview(ctx, l, inputs, outputs)
	}
	return fail()
}
func readOperatorPrivateJSON(path string, limit int64) ([]byte, error) {
	info, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() < 2 || info.Size() > limit {
		return nil, errors.New("operator private JSON file invalid")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	if int64(len(raw)) != info.Size() {
		return nil, errors.New("operator private JSON changed")
	}
	return raw, nil
}

var _ OperatorReviewMaterializer = (*OperatorFileReviews)(nil)
