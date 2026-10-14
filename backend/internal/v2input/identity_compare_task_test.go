package v2input

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestIdentityCompareProfileCapacityBounds(t *testing.T) {
	costs := SemanticQualifiedCosts{RAMBytes: 512 << 20, VRAMBytes: 6 << 30, ScratchBytes: 256 << 20}
	body, err := CompileIdentityCompareProfile("cuda", 6000, 4000, 900<<20, costs, 32<<30, 10<<30, 12<<30, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	var profile sessionDescribeExecutionProfile
	if json.Unmarshal(body, &profile) != nil {
		t.Fatal("profile JSON")
	}
	e := profile.ResourceEstimate
	if profile.Backend != "cuda" || profile.RendererVersion != "v2.2" || e.PixelCount != 24000000 ||
		e.RAMBytes < (2<<30)+160*24000000+(900<<20)+costs.RAMBytes || e.VRAMBytes != costs.VRAMBytes ||
		e.ScratchBytes < (900<<20)+costs.ScratchBytes || e.OutputBytes < 4*24000000 {
		t.Fatal("incomplete identity capacity")
	}
	limits := [4]int64{e.RAMBytes, e.VRAMBytes, e.ScratchBytes, e.OutputBytes}
	for i := range limits {
		bad := limits
		bad[i]--
		if _, err := CompileIdentityCompareProfile("cuda", 6000, 4000, 900<<20, costs, bad[0], bad[1], bad[2], bad[3]); err == nil {
			t.Fatal("under-reserved dimension", i)
		}
	}
	if _, err := CompileIdentityCompareProfile("cuda", 6000, 4000, 900<<20, SemanticQualifiedCosts{}, 32<<30, 10<<30, 12<<30, 1<<30); err == nil {
		t.Fatal("unqualified GPU accepted")
	}
	cpuCosts := costs
	cpuCosts.VRAMBytes = 0
	if raw, err := CompileIdentityCompareProfile("cpu", 6000, 4000, 900<<20, cpuCosts, 32<<30, 0, 12<<30, 1<<30); err != nil || len(raw) == 0 {
		t.Fatal("CPU profile refused", err)
	}
	for _, backend := range []string{"mps", "unknown"} {
		if _, err := CompileIdentityCompareProfile(backend, 6000, 4000, 900<<20, cpuCosts, 32<<30, 0, 12<<30, 1<<30); err == nil {
			t.Fatal("unsupported backend", backend)
		}
	}
	if _, err := CompileIdentityCompareProfile("cpu", 6000, 4000, 900<<20, costs, 32<<30, 10<<30, 12<<30, 1<<30); err == nil {
		t.Fatal("CPU claimed VRAM")
	}
}

func TestIdentityCompareFrozenTaskMatchesClosure(t *testing.T) {
	p, paramsRef, docs, dataset := identityClosureFixture(t)
	workflow := uuid.New()
	closure, err := CompileIdentityCompareClosure(context.Background(), workflow, 0, p, paramsRef, docs, dataset)
	if err != nil {
		t.Fatal(err)
	}
	sizes := make([]SemanticInputSize, len(closure.Direct))
	for i, ref := range closure.Direct {
		sizes[i] = SemanticInputSize{Ref: ref, Bytes: 4096}
	}
	costs := SemanticQualifiedCosts{RAMBytes: 256 << 20, VRAMBytes: 6 << 30, ScratchBytes: 64 << 20}
	frozen, err := CompileIdentityCompareTask(context.Background(), workflow, 0, "cuda", p, paramsRef, docs,
		dataset, sizes, costs, 32<<30, 10<<30, 12<<30, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := frozen.DraftTask()
	if err != nil || draft.Operation != "identity.compare" || draft.ResourceClass != "gpu" ||
		draft.ParamsArtifactID != paramsRef.ArtifactID || draft.ParamsSHA != paramsRef.SHA256 ||
		draft.InputDigest != closure.InputDigest || draft.NodeKey != "identity.compare:0" {
		t.Fatal("draft changed", err)
	}
	cpuCosts := costs
	cpuCosts.VRAMBytes = 0
	cpuTask, err := CompileIdentityCompareTask(context.Background(), workflow, 0, "cpu", p, paramsRef, docs,
		dataset, sizes, cpuCosts, 32<<30, 0, 12<<30, 1<<30)
	if err != nil {
		t.Fatal("CPU task", err)
	}
	cpuDraft, err := cpuTask.DraftTask()
	if err != nil {
		t.Fatal(err)
	}
	var cpuProfile sessionDescribeExecutionProfile
	if json.Unmarshal(cpuDraft.ExecutionProfile, &cpuProfile) != nil || cpuProfile.Backend != "cpu" || cpuProfile.ResourceEstimate.VRAMBytes != 0 {
		t.Fatal("CPU draft profile changed")
	}
	for _, change := range []func([]SemanticInputSize) []SemanticInputSize{
		func(values []SemanticInputSize) []SemanticInputSize { return values[:len(values)-1] },
		func(values []SemanticInputSize) []SemanticInputSize {
			values[0].Ref.SHA256 = p.ModelLockSHA
			return values
		},
	} {
		invalid := change(append([]SemanticInputSize(nil), sizes...))
		if _, err := CompileIdentityCompareTask(context.Background(), workflow, 0, "cuda", p, paramsRef, docs,
			dataset, invalid, costs, 32<<30, 10<<30, 12<<30, 1<<30); err == nil {
			t.Fatal("invalid inventory accepted")
		}
	}
}

func TestIdentityComparePublishedGraphCarriesExactConsentPin(t *testing.T) {
	p, paramsRef, docs, dataset := identityClosureFixture(t)
	workflow := uuid.New()
	closure, err := CompileIdentityCompareClosure(context.Background(), workflow, 0,
		p, paramsRef, docs, dataset)
	if err != nil {
		t.Fatal(err)
	}
	sizes := make([]SemanticInputSize, len(closure.Direct))
	for i, ref := range closure.Direct {
		sizes[i] = SemanticInputSize{Ref: ref, Bytes: 4096}
	}
	frozen, err := CompileIdentityCompareTask(context.Background(), workflow, 0, "cpu",
		p, paramsRef, docs, dataset, sizes,
		SemanticQualifiedCosts{RAMBytes: 256 << 20, ScratchBytes: 64 << 20},
		32<<30, 0, 12<<30, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PublishIdentityCompareTask(context.Background(), frozen,
		&memoryPublisher{bodies: make(map[uuid.UUID][]byte)})
	if err != nil {
		t.Fatal(err)
	}
	pin := prepared.Graph.IdentityConsent
	if pin == nil || pin.Workflow != workflow || pin.Ordinal != 0 ||
		pin.Ref != p.Consents || !bytes.Equal(pin.Body, docs.Consents) {
		t.Fatal("identity consent omitted from graph")
	}
	pin.Body[0] ^= 1
	if bytes.Equal(pin.Body, frozen.consentBody) {
		t.Fatal("graph mutated frozen consent")
	}
}

type identityCoordinatorStore struct {
	run, workflow uuid.UUID
	consent       Ref
	sealed        bool
}

func (s *identityCoordinatorStore) Begin(_ context.Context, _ DraftShell, _, _ string) (string, uuid.UUID, error) {
	return "created", s.run, nil
}
func (s *identityCoordinatorStore) Seal(_ context.Context, _, _ uuid.UUID, graph []GraphTask) error {
	if len(graph) != 1 || graph[0].IdentityConsent == nil ||
		graph[0].IdentityConsent.Ref != s.consent ||
		graph[0].IdentityConsent.Workflow != s.workflow {
		return ErrInvalid
	}
	s.sealed = true
	return nil
}
func (s *identityCoordinatorStore) Ticket(_ context.Context, _, _ uuid.UUID, _, _ string) (JobTicket, error) {
	return JobTicket{RunID: s.run, WorkflowID: s.workflow, State: "queued"}, nil
}

func TestAdmitIdentityCompareCarriesConsentToSeal(t *testing.T) {
	p, paramsRef, docs, dataset := identityClosureFixture(t)
	tenant, actor := uuid.New(), uuid.New()
	run, err := RunID(tenant, actor, "identity-key")
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := WorkflowID(run)
	if err != nil {
		t.Fatal(err)
	}
	closure, err := CompileIdentityCompareClosure(context.Background(), workflow, 0, p, paramsRef, docs, dataset)
	if err != nil {
		t.Fatal(err)
	}
	sizes := make([]SemanticInputSize, len(closure.Direct))
	for i, ref := range closure.Direct {
		sizes[i] = SemanticInputSize{Ref: ref, Bytes: 4096}
	}
	frozen, err := CompileIdentityCompareTask(context.Background(), workflow, 0, "cpu", p, paramsRef, docs,
		dataset, sizes, SemanticQualifiedCosts{RAMBytes: 256 << 20, ScratchBytes: 64 << 20}, 32<<30, 0, 12<<30, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		t.Fatal(err)
	}
	store := &identityCoordinatorStore{run: run, workflow: workflow, consent: p.Consents}
	shell := DraftShell{Tenant: tenant, Run: run, WorkflowID: workflow, Actor: actor, Tasks: []DraftTask{draft}}
	coordinator := Coordinator{Store: store, Publisher: &memoryPublisher{bodies: make(map[uuid.UUID][]byte)}}
	if _, err = AdmitIdentityCompare(context.Background(), coordinator, shell, "identity-key", "request-sha", frozen); err != nil || !store.sealed {
		t.Fatal("identity graph not sealed with consent", err)
	}
	store.sealed = false
	shell.Tasks[0].InputDigest = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if _, err = AdmitIdentityCompare(context.Background(), coordinator, shell, "identity-key", "request-sha", frozen); err == nil || store.sealed {
		t.Fatal("mutated identity task admitted")
	}
}
