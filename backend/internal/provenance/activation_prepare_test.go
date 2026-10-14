package provenance

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
)

type signingResourcesStub struct {
	value  SigningTaskResources
	calls  int
	err    error
	mutate bool
	after  func()
}

func (s *signingResourcesStub) QualifySigningResources(_ context.Context, _ auth.Identity, i FrozenSigningIntent, e SigningExportEvidence) (SigningTaskResources, error) {
	s.calls++
	if s.mutate {
		i.params[0] = 'x'
		i.lineage[0] = 'x'
		i.request[0] = 'x'
		e.LineageBody[0] = 'x'
	}
	if s.after != nil {
		s.after()
	}
	return s.value, s.err
}
func TestSigningActivationPreparationRechecksQualifiedSourceAndTask(t *testing.T) {
	for _, mode := range []string{"valid", "mutate", "actor", "session", "stale", "resources", "expected_task", "denied", "operator", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			config, params, now := signingRegistryFixture(t)
			authority := &signingAuthorityStub{}
			registry, err := NewSigningPolicyRegistry(config, authority)
			if err != nil {
				t.Fatal(err)
			}
			body, lineage := contractBytes(t, SigningLineage{Schema: 1, SourceHashes: []string{params.Export.SHA256}, Actions: []SigningAction{}})
			export := uuid.New()
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			source := &signingSourceStub{evidence: SigningExportEvidence{Project: uuid.New(), ExportID: export, Export: params.Export, MIME: "image/jpeg", Lineage: lineage, LineageBody: body}}
			intent, err := PrepareSigningIntent(context.Background(), who, uuid.New(), export, params.PolicyID, "activation", source, registry, now)
			if err != nil {
				t.Fatal(err)
			}
			resources := SigningTaskResources{ContractsSHA: strings.Repeat("d", 64), RAMBytes: 32 << 20, ScratchBytes: 1 << 30, OutputBytes: (512 << 20) + (16 << 20), MaxInputJSONBytes: 16 << 20}
			expected, err := CompileSigningTask(context.Background(), intent, registry, "image/jpeg", resources, now)
			if err != nil {
				t.Fatal(err)
			}
			qualifier := &signingResourcesStub{value: resources}
			ctx := context.Background()
			cause := errors.New("qualification unavailable")
			switch mode {
			case "mutate":
				qualifier.mutate = true
			case "actor":
				who.UserID = uuid.New()
			case "session":
				who.SessionID = uuid.New()
			case "stale":
				qualifier.after = func() { source.change = true }
			case "resources":
				qualifier.value.RAMBytes++
			case "expected_task":
				expected.task.Operation = "wrong"
			case "denied":
				qualifier.err = cause
			case "operator":
				qualifier.after = func() { authority.err = cause }
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			receipt, err := (SigningActivationPreparation{Source: source, Registry: registry, Resources: qualifier, Clock: func() time.Time { return now }}).Prepare(ctx, who, intent, expected)
			if mode == "valid" || mode == "mutate" {
				if err != nil || receipt.Task().Task().ID != intent.TaskID() || qualifier.calls != 1 {
					t.Fatal("activation preparation failed", err)
				}
				returned := receipt.Task()
				returned.body[0] = 'x'
				returned.task.ExecutionProfile[0] = 'x'
				if receipt.Task().body[0] != '{' || receipt.Task().Task().ExecutionProfile[0] != '{' {
					t.Fatal("receipt aliases caller")
				}
			} else {
				if err == nil || receipt.Task().Task().ID != uuid.Nil {
					t.Fatal("stale/unqualified receipt exposed")
				}
				if (mode == "denied" || mode == "operator") && !errors.Is(err, cause) {
					t.Fatal("qualification error lost", err)
				}
			}
			if (mode == "actor" || mode == "session" || mode == "expected_task" || mode == "cancelled") && qualifier.calls != 0 {
				t.Fatal("invalid request reached qualification")
			}
		})
	}
}
