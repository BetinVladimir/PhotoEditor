package v2portrait

import (
	"context"
	"encoding/json"
	"errors"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"reflect"
	"testing"
)

var errAnalysisPhase = errors.New("phase failed")

type analysisSubmitFake struct {
	prepared    PreparedRelightAnalysis
	graph       v2input.GraphTask
	receipt     RelightAnalysisReceipt
	calls       []string
	ready       bool
	fail        string
	recover     bool
	readFailure error
}

func (f *analysisSubmitFake) ReadQueuedReceipt(context.Context, auth.Identity, RelightAnalysisPlanRequest) (RelightAnalysisReceipt, error) {
	f.calls = append(f.calls, "read")
	if f.readFailure != nil {
		return RelightAnalysisReceipt{}, f.readFailure
	}
	if !f.ready {
		return RelightAnalysisReceipt{}, v2input.ErrExpired
	}
	return f.receipt, nil
}
func (f *analysisSubmitFake) phase(name string) error {
	f.calls = append(f.calls, name)
	if name == f.fail {
		f.ready = f.recover
		return errAnalysisPhase
	}
	return nil
}
func (f *analysisSubmitFake) PrepareParams(context.Context, auth.Identity, RelightAnalysisPlanRequest) (PreparedRelightAnalysis, error) {
	err := f.phase("prepare")
	return f.prepared, err
}
func (f *analysisSubmitFake) PublishTransport(context.Context, auth.Identity, RelightAnalysisPlanRequest, PreparedRelightAnalysis) (v2input.GraphTask, error) {
	err := f.phase("publish")
	return f.graph, err
}
func (f *analysisSubmitFake) Admit(context.Context, auth.Identity, RelightAnalysisPlanRequest, PreparedRelightAnalysis) error {
	err := f.phase("admit")
	if err == nil {
		f.ready = true
	}
	return err
}
func analysisSubmitFixture(t *testing.T) (RelightAnalysisPlanRequest, auth.Identity, *analysisSubmitFake) {
	t.Helper()
	r := analysisPlanFixture(t)
	p, err := CompileRelightAnalysisPlan(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := p.PublishInputClosure(context.Background(), &analysisTransportPublisher{})
	if err != nil {
		t.Fatal(err)
	}
	transport, err := canonical.Marshal(map[string]any{"manifest": graph.Built.Manifest, "chunks": graph.Built.Chunks, "count": graph.Built.Count, "sha256": graph.Built.SHA256})
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := p.Descriptor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	digest, err := canonical.Hash(json.RawMessage(descriptor))
	if err != nil {
		t.Fatal(err)
	}
	receipt := RelightAnalysisReceipt{SchemaVersion: 1, Run: p.snapshot.Run, Workflow: p.snapshot.Workflow, Task: p.snapshot.Task, State: "queued", Actor: r.Actor, Session: r.Session, Operation: r.Operation, DescriptorSHA: digest, Qualification: r.Runtime.Qualification, Transport: transport}
	return r, auth.Identity{UserID: r.Actor, SessionID: r.Session}, &analysisSubmitFake{prepared: PreparedRelightAnalysis{plan: p, params: artifactjson.Ref{ID: p.snapshot.ParamsDocument, SHA256: p.snapshot.ParamsSHA}}, graph: graph, receipt: receipt}
}
func TestAnalysisSubmissionReceiptFirstAndSuccessfulSequence(t *testing.T) {
	for _, existing := range []bool{true, false} {
		r, who, f := analysisSubmitFixture(t)
		f.ready = existing
		receipt, err := submitRelightAnalysis(context.Background(), f, who, r)
		if err != nil || receipt.Run != f.receipt.Run {
			t.Fatal(err)
		}
		expected := []string{"read"}
		if !existing {
			expected = []string{"read", "prepare", "publish", "admit", "read"}
		}
		if !reflect.DeepEqual(f.calls, expected) {
			t.Fatal(f.calls)
		}
	}
}
func TestAnalysisSubmissionRecoversUnknownCommitWithoutSecondActivation(t *testing.T) {
	for _, phase := range []string{"prepare", "publish", "admit"} {
		for _, recover := range []bool{true, false} {
			r, who, f := analysisSubmitFixture(t)
			f.fail = phase
			f.recover = recover
			_, err := submitRelightAnalysis(context.Background(), f, who, r)
			if recover && err != nil {
				t.Fatal(err)
			}
			if !recover && !errors.Is(err, errAnalysisPhase) {
				t.Fatal("original error lost", err)
			}
			count := 0
			for _, call := range f.calls {
				if call == phase {
					count++
				}
			}
			if count != 1 || f.calls[len(f.calls)-1] != "read" {
				t.Fatal("phase repeated or no recovery", f.calls)
			}
		}
	}
}
func TestAnalysisSubmissionStopsOnReceiptAuthorityFailure(t *testing.T) {
	r, who, f := analysisSubmitFixture(t)
	f.readFailure = errAnalysisPhase
	if _, err := submitRelightAnalysis(context.Background(), f, who, r); !errors.Is(err, errAnalysisPhase) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.calls, []string{"read"}) {
		t.Fatal("continued after receipt failure", f.calls)
	}
}
