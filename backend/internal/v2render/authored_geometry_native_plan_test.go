package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestAuthoredNativeRequestActuallySerializesCurrentContext(t *testing.T) {
	m := AuthoredNativeMaterialization{source: AuthoredInstructionSource{Tenant: uuid.New(), Parent: uuid.New(), Actor: uuid.New()}, authored: []byte(`{"original":true}`), selected: AuthoredGeometryMaterialization{Params: MaterializedAuthoredGeometry{Body: []byte(`{"current":true}`)}}}
	raw, e := authoredNativeQualificationRequest(m)
	if e != nil {
		t.Fatal(e)
	}
	var wire map[string]json.RawMessage
	if json.Unmarshal(raw, &wire) != nil || len(wire) != 3 {
		t.Fatal("closed request shape")
	}
	expected, e := canonical.Marshal(m.source)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(wire["context"], expected) || wire["Context"] != nil {
		t.Fatal("current source must be encoded under exact SQL context key")
	}
}
func TestAuthoredNativeUsesActualProtectedNamespaceAndResourceClass(t *testing.T) {
	m := AuthoredNativeMaterialization{source: AuthoredInstructionSource{Tenant: uuid.New(), Parent: uuid.New(), Actor: uuid.New(), Instruction: AuthoredGeometryInstruction{Identity: uuid.New()}}}
	for _, operation := range []string{"image.upscale", "optical.panorama", "optical.focus_stack"} {
		m.selected.Operation = operation
		run, workflow, task, e := authoredNativeIdentities(m)
		if e != nil {
			t.Fatal(e)
		}
		expected := uuid.NewSHA1(m.source.Tenant, []byte("V2.Admit:"+m.source.Actor.String()+":authored-native/"+m.source.Instruction.Identity.String()))
		expectedWorkflow, e := v2input.WorkflowID(expected)
		if e != nil {
			t.Fatal(e)
		}
		expectedTask, e := v2input.TaskID(expectedWorkflow, operation, 0)
		if e != nil {
			t.Fatal(e)
		}
		if run != expected || workflow != expectedWorkflow || task != expectedTask {
			t.Fatal("namespace differs from installed source scope")
		}
		class, e := authoredNativeResourceClass(operation)
		if e != nil {
			t.Fatal(e)
		}
		want := "cpu"
		if operation == "image.upscale" {
			want = "gpu"
		}
		if class != want {
			t.Fatal("operation resource class")
		}
	}
	if _, e := authoredNativeResourceClass("render.authored_geometry"); !errors.Is(e, v2input.ErrInvalid) {
		t.Fatal("foreign operation")
	}
	m.source.Actor = uuid.Nil
	if _, _, _, e := authoredNativeIdentities(m); !errors.Is(e, v2input.ErrInvalid) {
		t.Fatal("missing source actor")
	}
}
func TestAuthoredNativeMaterializationCancellationBeforeDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e := (SQLAuthoredNativeMaterializer{}).Read(ctx, auth.Identity{}, uuid.New(), uuid.New(), 1)
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("cancelled before any pool/object read: %v", e)
	}
}

func TestAuthoredNativeTransportCancellationBeforePublisher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := PublishAuthoredNativeTransport(ctx, AuthoredNativeTaskPlan{}, nil); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
func TestAuthoredNativeAdmissionCancellationBeforePool(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := (SQLAuthoredNativeAdmission{}).Stage(ctx, AuthoredNativeMaterialization{}, AuthoredNativeTaskPlan{}); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if e := (SQLAuthoredNativeAdmission{}).Admit(ctx, AuthoredNativeMaterialization{}, AuthoredNativeTaskPlan{}, "", nil); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
func TestAuthoredNativePreparationCancellationBeforeDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := (SQLAuthoredNativePreparation{}).Prepare(ctx, auth.Identity{}, uuid.New(), uuid.New(), 1); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
func TestAuthoredNativeDeploymentCancellationBeforeFile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := ReadAuthoredNativeDeployment(ctx, "must-not-open", ""); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
