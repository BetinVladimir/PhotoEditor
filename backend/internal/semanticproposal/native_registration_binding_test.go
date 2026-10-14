package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func assertNativeRegistrationBinding(t *testing.T, frozen nativeRegistrationTask, built v2input.Built, operation string, marshal func(context.Context, v2input.Built) (json.RawMessage, error)) {
	t.Helper()
	body, err := marshal(context.Background(), built)
	if err != nil {
		t.Fatal("registration evidence", err)
	}
	var packet defectRegistrationBinding
	if err := json.Unmarshal(body, &packet); err != nil {
		t.Fatal(err)
	}
	task, err := frozen.DraftTask()
	if err != nil {
		t.Fatal(err)
	}
	transport, err := frozen.BindPublishedTransport(context.Background(), built)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := freezeDefectRegistrationProof(context.Background(), transport.Graph)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := canonical.Marshal(packet.Transport)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := canonical.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	if packet.SchemaVersion != 1 || packet.TaskID != task.ID || packet.Params != transport.Params || packet.InputDigest != task.InputDigest || string(packet.Profile) != string(task.ExecutionProfile) || string(actual) != string(expected) {
		t.Fatal("registration evidence substituted frozen inputs")
	}
	other := "face.detect"
	if operation == other {
		other = "mask.person"
	}
	if _, err := marshalNativeRegistrationBinding(context.Background(), frozen, built, other); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("wrong native operation", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := marshal(ctx, built); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled evidence", err)
	}
	bad := built
	bad.Count++
	if _, err := marshal(context.Background(), bad); err == nil {
		t.Fatal("altered transport count accepted")
	}
}
