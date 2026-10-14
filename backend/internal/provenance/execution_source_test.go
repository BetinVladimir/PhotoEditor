package provenance

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type signingTaskReaderFunc func(context.Context, auth.Identity, ReadySigningExport, uuid.UUID) (CanonicalSigningTask, error)

func (f signingTaskReaderFunc) ReadCanonicalSigningTask(ctx context.Context, who auth.Identity, r ReadySigningExport, id uuid.UUID) (CanonicalSigningTask, error) {
	return f(ctx, who, r, id)
}

type signingTaskClaimsFunc func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask) (ResolvedSigningTask, error)

func (f signingTaskClaimsFunc) ResolveSigningTaskLineage(ctx context.Context, who auth.Identity, r ReadySigningExport, e CanonicalSigningTask) (ResolvedSigningTask, error) {
	return f(ctx, who, r, e)
}

func TestBoundSigningExecutionBindsCanonicalEvidenceAndClaims(t *testing.T) {
	for _, mode := range []string{"valid", "cross_run", "foreign_task", "missing_run", "final_run", "run_claim", "result", "fence", "operation", "params", "output", "source", "changed", "callback_mutation", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			fixture := signingGraphFixture()
			ready := ReadySigningExport{Tenant: uuid.New(), Project: uuid.New(), ExportID: uuid.New(), Run: uuid.New(), Task: fixture.FinalTask, Ref: fixture.Export}
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			nodes := map[uuid.UUID]SigningExecutionNode{}
			reads := map[uuid.UUID]int{}
			tasks := signingTaskReaderFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, id uuid.UUID) (CanonicalSigningTask, error) {
				n, ok := nodes[id]
				if !ok {
					return CanonicalSigningTask{}, ErrInvalid
				}
				reads[id]++
				inputs, _ := canonical.Marshal(n.Inputs)
				outputs, _ := canonical.Marshal(n.Outputs)
				value := CanonicalSigningTask{Task: id, Run: ready.Run, Result: n.CanonicalResult, Attempt: n.Attempt, Fence: n.Fence, Operation: n.Action.Operation, Version: n.Action.Version, Params: v2input.Ref{ArtifactID: uuid.New(), SHA256: n.Action.ParamsSHA}, InputDigest: strings.Repeat("a", 64), ResultDigest: strings.Repeat("b", 64), Inputs: inputs, Outputs: outputs, Metrics: json.RawMessage(`{}`)}
				// Immutable Params ID, not a freshly allocated identity per read.
				value.Params.ArtifactID = uuid.NewSHA1(id, []byte("params"))
				if mode == "cross_run" && id != ready.Task {
					value.Run = uuid.NewSHA1(id, []byte("previous-run"))
				}
				if mode == "foreign_task" {
					value.Task = uuid.New()
				}
				if mode == "missing_run" {
					value.Run = uuid.Nil
				}
				if mode == "final_run" && id == ready.Task {
					value.Run = uuid.New()
				}
				if mode == "changed" && reads[id] > 1 {
					value.Fence++
				}
				return value, nil
			})
			for _, n := range fixture.Nodes {
				nodes[n.Task] = n
			}
			claims := signingTaskClaimsFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, e CanonicalSigningTask) (ResolvedSigningTask, error) {
				n := nodes[e.Task]
				n.Run = e.Run
				resolved := ResolvedSigningTask{Node: n}
				if len(n.Parents) == 0 {
					resolved.Sources = append([]v2input.Ref(nil), fixture.Sources...)
				}
				switch mode {
				case "run_claim":
					resolved.Node.Run = uuid.New()
				case "result":
					resolved.Node.CanonicalResult = uuid.New()
				case "fence":
					resolved.Node.Fence++
				case "operation":
					resolved.Node.Action.Operation = "other"
				case "params":
					resolved.Node.Action.ParamsSHA = strings.Repeat("d", 64)
				case "output":
					resolved.Node.Outputs = []v2input.Ref{{ArtifactID: uuid.New(), SHA256: ready.Ref.SHA256}}
				case "source":
					if len(resolved.Sources) > 0 {
						resolved.Sources[0].SHA256 = strings.Repeat("e", 64)
					}
				case "callback_mutation":
					e.Inputs[0] = 'x'
					e.Outputs[0] = 'x'
					e.Metrics[0] = 'x'
				}
				return resolved, nil
			})
			ctx := context.Background()
			if mode == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			graph, err := (BoundSigningExecutionSource{Tasks: tasks, Claims: claims}).ReadSigningExecution(ctx, who, ready)
			if mode == "valid" || mode == "cross_run" || mode == "callback_mutation" {
				if err != nil || len(graph.Nodes) != 2 || len(graph.Sources) != 1 {
					t.Fatal("complete evidence lost", err)
				}
				expected, err := CompileSigningLineage(ctx, fixture)
				if err != nil {
					t.Fatal(err)
				}
				actual, err := CompileSigningLineage(ctx, graph)
				if err != nil || string(actual) != string(expected) {
					t.Fatal("claims changed", err)
				}
				for _, n := range fixture.Nodes {
					if reads[n.Task] != 2 {
						t.Fatal("missing canonical recheck")
					}
				}
			} else if err == nil || graph.FinalTask != uuid.Nil {
				t.Fatal("unbound claims exposed")
			}
		})
	}
}

func TestCanonicalTaskOutputRefsRejectAmbiguousWire(t *testing.T) {
	reference := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	good, _ := canonical.Marshal([]v2input.Ref{reference})
	duplicate, _ := canonical.Marshal([]v2input.Ref{reference, reference})
	for _, raw := range [][]byte{[]byte(`null`), []byte(`[]`), []byte(`{}`), duplicate, []byte(strings.Replace(string(good), "\"sha256\":", "\"extra\":true,\"sha256\":", 1))} {
		if _, err := canonicalTaskOutputRefs(raw); err == nil {
			t.Fatal("invalid canonical output refs accepted")
		}
	}
	if _, err := canonicalTaskOutputRefs(good); err != nil {
		t.Fatal(err)
	}
	if sameSigningRefs([]v2input.Ref{reference, reference}, []v2input.Ref{reference, reference}) {
		t.Fatal("duplicate outputs accepted")
	}
}
