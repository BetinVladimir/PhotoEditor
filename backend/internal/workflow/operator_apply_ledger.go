package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2qualification"
	"reflect"
	"sort"
)

// OperatorApplyLedger is an isolated compiler state, never a replacement for
// ApplyStore in service wiring. Artifacts are retained measured bytes; methods
// with names shared by SQL writers do not INSERT accepted/task/qualification rows.
type OperatorApplyLedger struct {
	mixedChildren     *v2qualification.OperatorMixedChildren
	mixedCorpus       string
	maskApprovals     []OperatorMaskApproval
	lease             ApplyLease
	inputs            []applyInput
	plans             map[uuid.UUID]applyNodePlan
	tasks             map[uuid.UUID]PreparedTask
	outputs           map[uuid.UUID][]OperatorMeasuredOutput
	objects           map[uuid.UUID]styleArtifact
	complete          bool
	failed            bool
	groupCount        int
	decisionPort      OperatorReviewMaterializer
	cullingProjection json.RawMessage
	reviews           map[string]OperatorReviewResult
}

// Review materialization must independently validate the explicit operator
// decision against the exact measured proposals/current phase. There is no
// default approve/identity fallback. The production review compiler remains the
// required implementation; raw fixture outputs cannot be supplied as decisions.
type OperatorReviewMaterializer interface {
	MaterializeOperatorReview(context.Context, ApplyLease, []applyInput, []OperatorMeasuredOutput) (OperatorReviewResult, error)
}
type OperatorReviewResult struct {
	DecisionSHA256 string
	Phase          string
	Outputs        []applyOutput
	Dust           []applyDustDecision
	Culling        *review.OperatorCullingPlan
}

func cloneOperator[T any](value T) (T, error) {
	var out T
	raw, e := json.Marshal(value)
	if e != nil {
		return out, e
	}
	e = json.Unmarshal(raw, &out)
	return out, e
}
func NewOperatorApplyLedger(l ApplyLease, inputs []applyInput, compiled []ApplyV4Stage, decisionPort OperatorReviewMaterializer) (*OperatorApplyLedger, error) {
	if l.TenantID == uuid.Nil || l.RunID == uuid.Nil || l.Token == uuid.Nil || l.GraphVersion != 4 || len(inputs) != l.ItemCount || len(compiled) == 0 {
		return nil, errors.New("operator ledger scope invalid")
	}
	if l.Phase != compiled[0].Phase {
		return nil, errors.New("operator ledger initial phase mismatch")
	}
	owned, e := cloneOperator(inputs)
	if e != nil {
		return nil, e
	}
	plan, e := cloneOperator(compiled)
	if e != nil {
		return nil, e
	}
	index := 0
	l.StageIndex = &index
	l.FrozenPlan = plan
	s := &OperatorApplyLedger{lease: l, inputs: owned, plans: map[uuid.UUID]applyNodePlan{}, tasks: map[uuid.UUID]PreparedTask{}, outputs: map[uuid.UUID][]OperatorMeasuredOutput{}, objects: map[uuid.UUID]styleArtifact{}, decisionPort: decisionPort, reviews: map[string]OperatorReviewResult{}}
	for _, item := range inputs {
		if item.ItemID == uuid.Nil || item.AssetID == uuid.Nil || item.VersionID == uuid.Nil {
			return nil, errors.New("operator source identity missing")
		}
		s.objects[item.Image.ArtifactID] = item.Image
		if item.Style != nil {
			s.objects[item.Style.ArtifactID] = *item.Style
		}
	}
	return s, nil
}
func (s *OperatorApplyLedger) scope(l ApplyLease) bool {
	return l.TenantID == s.lease.TenantID && l.RunID == s.lease.RunID && l.Token == s.lease.Token && !s.failed && !s.complete
}
func (s *OperatorApplyLedger) Claim(ctx context.Context) (*ApplyLease, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if s.failed {
		return nil, errors.New("operator stage failed")
	}
	if s.complete {
		return nil, nil
	}
	for id := range s.tasks {
		if _, ok := s.outputs[id]; !ok {
			return nil, nil
		}
	}
	l, e := cloneOperator(s.lease)
	return &l, e
}
func (s *OperatorApplyLedger) Renew(ctx context.Context, l ApplyLease) (bool, error) {
	return s.scope(l), ctx.Err()
}
func (s *OperatorApplyLedger) Inputs(ctx context.Context, l ApplyLease) ([]applyInput, error) {
	if !s.scope(l) {
		return nil, errors.New("operator scope changed")
	}
	return cloneOperator(s.inputs)
}
func (s *OperatorApplyLedger) RegisterV4Plan(ctx context.Context, l ApplyLease, plan []ApplyV4Stage) (bool, error) {
	return s.scope(l) && reflect.DeepEqual(plan, s.lease.FrozenPlan), ctx.Err()
}
func (s *OperatorApplyLedger) Register(ctx context.Context, l ApplyLease, n applyNodePlan) (bool, error) {
	if !s.scope(l) || n.Phase != l.Phase || n.ID == uuid.Nil {
		return false, errors.New("operator node scope invalid")
	}
	if old, ok := s.plans[n.ID]; ok {
		return reflect.DeepEqual(old, n), nil
	}
	owned, e := cloneOperator(n)
	if e == nil {
		s.plans[n.ID] = owned
	}
	return e == nil, e
}
func (s *OperatorApplyLedger) Materialize(ctx context.Context, l ApplyLease, t PreparedTask) (bool, error) {
	n, ok := s.plans[t.ID]
	if !ok || !s.scope(l) || n.Operation != t.Operation || n.ParamsSHA != t.Params.SHA256 || !reflect.DeepEqual(n.Inputs, t.Inputs) {
		return false, errors.New("operator materialization changed")
	}
	if old, found := s.tasks[t.ID]; found {
		return reflect.DeepEqual(old, t), nil
	}
	owned, e := cloneOperator(t)
	if e == nil {
		s.tasks[t.ID] = owned
	}
	return e == nil, e
}
func (s *OperatorApplyLedger) Advance(ctx context.Context, l ApplyLease, to string) (bool, error) {
	if !s.scope(l) || l.Phase != s.lease.Phase {
		return false, errors.New("operator phase changed")
	}
	index := *s.lease.StageIndex + 1
	expected := "adopt"
	if index < len(s.lease.FrozenPlan) {
		expected = s.lease.FrozenPlan[index].Phase
	}
	if to != "" && to != expected {
		return false, errors.New("operator phase order mismatch")
	}
	s.lease.Phase = expected
	s.lease.StageIndex = &index
	return true, ctx.Err()
}
func (s *OperatorApplyLedger) SetGroupCount(ctx context.Context, l ApplyLease, n int) (bool, error) {
	if !s.scope(l) || n < 1 || n > s.lease.ItemCount {
		return false, errors.New("operator group count invalid")
	}
	s.groupCount = n
	return true, ctx.Err()
}
func (s *OperatorApplyLedger) Fail(ctx context.Context, l ApplyLease, code string) (bool, error) {
	if !s.scope(l) || code == "" {
		return false, errors.New("operator failure scope invalid")
	}
	s.failed = true
	return true, ctx.Err()
}
func (s *OperatorApplyLedger) Adopt(ctx context.Context, l ApplyLease) (bool, error) {
	if !s.scope(l) || l.Phase != "adopt" {
		return false, errors.New("operator completion phase invalid")
	}
	for id := range s.tasks {
		if len(s.outputs[id]) == 0 {
			return false, errors.New("unmeasured stage at completion")
		}
	}
	s.complete = true
	return true, ctx.Err()
}
func (s *OperatorApplyLedger) Artifact(ctx context.Context, l ApplyLease, ref review.ArtifactRef) (styleArtifact, error) {
	a, ok := s.objects[ref.ID]
	if !s.scope(l) || !ok || a.SHA256 != ref.SHA256 {
		return styleArtifact{}, errors.New("operator artifact unbound")
	}
	return a, ctx.Err()
}
func (s *OperatorApplyLedger) Outputs(ctx context.Context, l ApplyLease, op, role string) ([]applyOutput, error) {
	if !s.scope(l) {
		return nil, errors.New("operator output scope invalid")
	}
	var out []applyOutput
	for id, proofs := range s.outputs {
		n := s.plans[id]
		if n.Operation != op {
			continue
		}
		for _, proof := range proofs {
			if proof.Role == role {
				out = append(out, applyOutput{Ordinal: n.Ordinal, ItemID: n.SourceItemID, TaskID: id, Artifact: proof.Artifact})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ordinal < out[j].Ordinal })
	return out, ctx.Err()
}
func (s *OperatorApplyLedger) PendingMeasuredTasks(ctx context.Context) ([]PreparedTask, error) {
	var out []PreparedTask
	for id, t := range s.tasks {
		if _, ok := s.outputs[id]; !ok {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeKey < out[j].NodeKey })
	return cloneOperator(out)
}
func (s *OperatorApplyLedger) MeasurementComplete(ctx context.Context) (bool, error) {
	if s.failed {
		return false, errors.New("operator measurement failed")
	}
	return s.complete, ctx.Err()
}

// AcceptMeasuredTask requires a physical verifier port. Mere metadata supplied by
// an adapter is not proof of retained measured bytes/typed output contracts.
func (s *OperatorApplyLedger) AcceptMeasuredTask(ctx context.Context, t PreparedTask, m OperatorMeasuredTask) error {
	if e := validateOperatorObservation(t, m); e != nil {
		return e
	}
	actual, ok := s.tasks[t.ID]
	if !ok || !reflect.DeepEqual(actual, t) || !reflect.DeepEqual(m.Task, t) || len(m.Outputs) == 0 {
		return errors.New("operator measured task mismatch")
	}
	if _, exists := s.outputs[t.ID]; exists {
		return errors.New("operator measurement replay rejected")
	}
	total := int64(0)
	seen := map[uuid.UUID]bool{}
	for _, proof := range m.Outputs {
		if proof.Producer != t.ID || proof.Role == "" || seen[proof.Artifact.ArtifactID] || proof.Artifact.Bytes < 1 || proof.Artifact.SHA256 == "" || proof.Verifier == nil {
			return errors.New("operator output proof missing")
		}
		if err := proof.Verifier.VerifyMeasuredObject(ctx, t, proof); err != nil {
			return err
		}
		seen[proof.Artifact.ArtifactID] = true
		total += proof.Artifact.Bytes
	}
	if total != m.OutputBytes {
		return errors.New("operator measured byte count mismatch")
	}
	owned := make([]OperatorMeasuredOutput, len(m.Outputs))
	copy(owned, m.Outputs)
	s.outputs[t.ID] = owned
	for _, proof := range owned {
		s.objects[proof.Artifact.ArtifactID] = proof.Artifact
	}
	return nil
}

type OperatorMeasuredOutput struct {
	Artifact styleArtifact
	Role     string
	Contract string
	Producer uuid.UUID
	Verifier *OperatorPhysicalObjects
}

// Review state is stored separately from worker outputs and can only originate
// in the explicit review materializer; absence blocks the whole measurement.
func (s *OperatorApplyLedger) operatorReview(ctx context.Context, l ApplyLease) (OperatorReviewResult, error) {
	if !s.scope(l) || l.Phase != s.lease.Phase || s.decisionPort == nil {
		return OperatorReviewResult{}, errors.New("explicit operator review materializer required")
	}
	var outputs []OperatorMeasuredOutput
	for _, v := range s.outputs {
		outputs = append(outputs, v...)
	}
	r, e := s.decisionPort.MaterializeOperatorReview(ctx, l, s.inputs, outputs)
	if e != nil {
		return r, e
	}
	if r.Phase != l.Phase || len(r.DecisionSHA256) != 64 {
		return r, errors.New("operator review binding invalid")
	}
	return r, nil
}
func (s *OperatorApplyLedger) ProcessCandidateReviews(ctx context.Context, l ApplyLease) (string, error) {
	r, e := s.operatorReview(ctx, l)
	if e != nil {
		return "", e
	}
	s.reviews[l.Phase] = r
	_, e = s.Advance(ctx, l, "")
	return "advanced", e
}
func (s *OperatorApplyLedger) ProcessDefectReviews(ctx context.Context, l ApplyLease) (string, error) {
	r, e := s.operatorReview(ctx, l)
	if errors.Is(e, errOperatorReviewTasksPending) {
		return "activated", nil
	}
	if e != nil {
		return "", e
	}
	s.reviews[l.Phase] = r
	_, e = s.Advance(ctx, l, "")
	return "advanced", e
}
func (s *OperatorApplyLedger) ProcessDustReviews(ctx context.Context, l ApplyLease) (string, error) {
	return s.ProcessCandidateReviews(ctx, l)
}
func (s *OperatorApplyLedger) CandidateOutputs(ctx context.Context, l ApplyLease, phase string) ([]applyOutput, error) {
	if !s.scope(l) {
		return nil, errors.New("operator review output scope invalid")
	}
	r, ok := s.reviews[phase]
	if !ok {
		return nil, errors.New("explicit operator decision missing")
	}
	return r.Outputs, ctx.Err()
}
func (s *OperatorApplyLedger) DefectOutputs(ctx context.Context, l ApplyLease) ([]applyOutput, error) {
	return s.CandidateOutputs(ctx, l, "defects_review")
}
func (s *OperatorApplyLedger) DustDecisions(ctx context.Context, l ApplyLease) ([]applyDustDecision, error) {
	r, ok := s.reviews["dust_review"]
	if !ok || !s.scope(l) {
		return nil, errors.New("explicit operator dust decision missing")
	}
	return r.Dust, ctx.Err()
}
func (s *OperatorApplyLedger) conservativeProposalReport(ctx context.Context, l ApplyLease, p applyOutput) (styleArtifact, error) {
 if !s.scope(l){return styleArtifact{},errors.New("operator proposal report scope invalid")}
	for _, proof := range s.outputs[p.TaskID] {
		if proof.Role == "defect_propose_report" {
			return proof.Artifact, nil
		}
	}
	return styleArtifact{}, errors.New("measured proposal report missing")
}
func (s *OperatorApplyLedger) ProcessCullingReview(ctx context.Context, l ApplyLease) (bool, error) {
	if !s.scope(l) || l.Phase != "cull_review" {
		return false, errors.New("operator culling phase invalid")
	}
	if len(s.cullingProjection) == 0 {
		return false, nil
	}
	r, e := s.operatorReview(ctx, l)
	if e != nil {
		return false, e
	}
	if r.Culling == nil || !r.Culling.NeverDelete || len(r.Culling.Selected) == 0 {
		return false, errors.New("operator culling explicit selection missing")
	}
	s.reviews[l.Phase] = r
	return s.Advance(ctx, l, "")
}
func (s *OperatorApplyLedger) RegisterCullingReview(ctx context.Context, l ApplyLease, p json.RawMessage) (bool, error) {
	if !s.scope(l) || l.Phase != "cull_review" || len(p) < 2 || len(p) > 6<<20 {
		return false, errors.New("operator culling projection scope invalid")
	}
	normalized, e := canonical.Bytes(p)
	if e != nil || !bytes.Equal(normalized, p) {
		return false, errors.New("operator culling projection noncanonical")
	}
	if len(s.cullingProjection) != 0 {
		return bytes.Equal(s.cullingProjection, p), ctx.Err()
	}
	s.cullingProjection = append(json.RawMessage(nil), p...)
	return true, ctx.Err()
}
func (s *OperatorApplyLedger) FailEditorPrecompose(ctx context.Context, l ApplyLease) (bool, error) {
	return s.Fail(ctx, l, "EDITOR_PRECOMPOSE_PROFILE_MISSING")
}
func (s *OperatorApplyLedger) RecordEditorPrecompose(ctx context.Context, l ApplyLease, r review.ArtifactRef, canvas string) (bool, error) {
	outputs, e := s.Outputs(ctx, l, "editor.layers_precompose", "working_image")
	if e != nil || len(outputs) != 1 || outputs[0].Artifact.ref() != r || len(canvas) != 64 {
		return false, errors.New("measured editor precompose root missing")
	}
	return true, nil
}

var _ OperatorApplyStore = (*OperatorApplyLedger)(nil)
