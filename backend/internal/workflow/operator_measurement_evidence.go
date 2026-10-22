package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"reflect"
	"sort"
	"strings"
)

type operatorEvidenceTask struct {
	Plan           applyNodePlan   `json:"plan"`
	Task           PreparedTask    `json:"task"`
	ObservationSHA string          `json:"observation_sha256"`
	Observation    json.RawMessage `json:"observation"`
	ElapsedMS      float64         `json:"elapsed_ms"`
}
type operatorEvidenceReview struct {
	Phase       string                      `json:"phase"`
	DecisionSHA string                      `json:"decision_sha256"`
	Outputs     []applyOutput               `json:"outputs"`
	Dust        []applyDustDecision         `json:"dust"`
	Culling     *review.OperatorCullingPlan `json:"culling"`
}

// FreezeOperatorMeasurement closes the real local execution ledger after
// ProcessOne completion. It is evidence for subsequent independent golden and
// applicability measurements, NOT a qualification Report or SQL receipt.
func FreezeOperatorMeasurement(ctx context.Context, s *OperatorApplyLedger, objects *OperatorPhysicalObjects, observations []OperatorMeasuredTask) (json.RawMessage, error) {
	fail := func() (json.RawMessage, error) {
		return nil, errors.New("operator complete measurement evidence mismatch")
	}
	if s == nil || objects == nil || !s.complete || s.failed || len(s.tasks) == 0 || len(observations) != len(s.tasks) {
		return fail()
	}
	tasks := []operatorEvidenceTask{}
	seen := map[uuid.UUID]bool{}
	for _, o := range observations {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		id := o.Task.ID
		t, ok := s.tasks[id]
		n, planned := s.plans[id]
		elapsed := float64(o.Elapsed) / 1e6
		if !ok || !planned || seen[id] || !reflect.DeepEqual(t, o.Task) || elapsed <= 0 || math.IsNaN(elapsed) || math.IsInf(elapsed, 0) {
			return fail()
		}
		seen[id] = true
		if e := validateOperatorObservation(t, o); e != nil {
			return nil, e
		}
		stored, ok := s.outputs[id]
		if !ok || len(stored) != len(o.Outputs) {
			return fail()
		}
		for i, p := range o.Outputs {
			old := stored[i]
			if p.Artifact != old.Artifact || p.Producer != old.Producer || p.Role != old.Role || p.Contract != old.Contract {
				return fail()
			}
			if e := objects.VerifyMeasuredObject(ctx, t, p); e != nil {
				return nil, e
			}
		}
		tasks = append(tasks, operatorEvidenceTask{Plan: n, Task: t, ObservationSHA: o.ObservationSHA256, Observation: append(json.RawMessage(nil), o.ObservationRaw...), ElapsedMS: elapsed})
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Task.NodeKey < tasks[j].Task.NodeKey })
	reviews := []operatorEvidenceReview{}
	for _, stage := range s.lease.FrozenPlan {
		if stage.Cardinality != "review" && !strings.HasSuffix(stage.Phase, "_review") {
			continue
		}
		r, ok := s.reviews[stage.Phase]
		if !ok || r.Phase != stage.Phase || len(r.DecisionSHA256) != 64 {
			return fail()
		}
		reviews = append(reviews, operatorEvidenceReview{Phase: r.Phase, DecisionSHA: r.DecisionSHA256, Outputs: r.Outputs, Dust: r.Dust, Culling: r.Culling})
	}
	sort.Slice(reviews, func(i, j int) bool { return reviews[i].Phase < reviews[j].Phase })
	// The entire original collection is retained, including culling exclusions.
	// No "selected only" output can erase a failed or unmeasured source case.
	evidence := struct {
		Schema        int                      `json:"schema_version"`
		Boundary      string                   `json:"boundary"`
		Tenant        uuid.UUID                `json:"tenant_id"`
		Run           uuid.UUID                `json:"run_id"`
		Sources       []applyInput             `json:"sources"`
		Stages        []ApplyV4Stage           `json:"stages"`
		Tasks         []operatorEvidenceTask   `json:"tasks"`
		Reviews       []operatorEvidenceReview `json:"reviews"`
		MaskApprovals []OperatorMaskApproval   `json:"mask_approvals"`
	}{1, "complete_local_measurement_not_qualification_or_sql_receipt", s.lease.TenantID, s.lease.RunID, s.inputs, s.lease.FrozenPlan, tasks, reviews, s.maskApprovals}
	raw, e := canonical.Marshal(evidence)
	return json.RawMessage(raw), e
}
