package v2optical

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestMultiReceiptBindsHistoricalAcknowledgement(t *testing.T) {
	for _, operation := range []string{"optical.panorama", "optical.focus_stack"} {
		t.Run(operation, func(t *testing.T) {
			pin, locks, report, calibration, params, originals := multiQualifiedFixture(t, operation)
			qualified, err := VerifyMultiQualificationEvidence(operation, pin, locks, report, calibration, params, originals)
			if err != nil {
				t.Fatal(err)
			}
			capacity, err := MultiResources(operation, params, originals)
			if err != nil {
				t.Fatal(err)
			}
			draft, err := CompileMultiDraft(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), uuid.New(), uuid.NewString(), operation, params, originals, qualified, capacity)
			if err != nil {
				t.Fatal(err)
			}
			s, err := draft.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			published := v2input.Ref{ArtifactID: s.ParamsDocument, SHA256: s.ParamsSHA}
			plan, err := CompileMultiTaskPlan(context.Background(), operation, params, originals, qualified, capacity)
			if err != nil {
				t.Fatal(err)
			}
			graph, err := BuildMultiTaskTransport(context.Background(), s.Task, plan, originals, qualified, published, &negativePublisher{})
			if err != nil {
				t.Fatal(err)
			}
			transport, err := canonical.Marshal(map[string]any{"manifest": graph.Built.Manifest, "chunks": graph.Built.Chunks, "count": graph.Built.Count, "sha256": graph.Built.SHA256})
			if err != nil {
				t.Fatal(err)
			}
			descriptor, err := draft.Descriptor(context.Background(), qualified, published)
			if err != nil {
				t.Fatal(err)
			}
			digest, err := canonical.Hash(json.RawMessage(descriptor))
			if err != nil {
				t.Fatal(err)
			}
			receipt := MultiReceipt{SchemaVersion: 1, Run: s.Run, Workflow: s.Workflow, Task: s.Task, State: "queued", Actor: s.Actor, Session: s.Session, Operation: operation, DescriptorSHA: digest, Qualification: pin, Transport: transport}
			raw, err := canonical.Marshal(receipt)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := parseMultiReceipt(context.Background(), draft, qualified, published, raw)
			if err != nil || parsed.Task != s.Task {
				t.Fatal("valid receipt refused", err)
			}
			for _, mutate := range []func(*MultiReceipt){
				func(r *MultiReceipt) { r.Run = uuid.New() },
				func(r *MultiReceipt) { r.Actor = uuid.New() },
				func(r *MultiReceipt) { r.Session = uuid.New() },
				func(r *MultiReceipt) { r.Task = uuid.New() },
				func(r *MultiReceipt) { r.State = "succeeded" },
				func(r *MultiReceipt) { r.Operation = "optical.perspective" },
				func(r *MultiReceipt) { r.DescriptorSHA = s.ParamsSHA },
				func(r *MultiReceipt) { r.Qualification.Report.ArtifactID = uuid.New() },
				func(r *MultiReceipt) { r.Transport = json.RawMessage(`{}`) },
			} {
				changed := receipt
				mutate(&changed)
				body, err := canonical.Marshal(changed)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := parseMultiReceipt(context.Background(), draft, qualified, published, body); err == nil {
					t.Fatal("substituted historical acknowledgement accepted")
				}
			}
			var unknown map[string]any
			if json.Unmarshal(raw, &unknown) != nil {
				t.Fatal("fixture")
			}
			unknown["foreign"] = true
			body, err := canonical.Marshal(unknown)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parseMultiReceipt(context.Background(), draft, qualified, published, body); err == nil {
				t.Fatal("unknown receipt field accepted")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := parseMultiReceipt(ctx, draft, qualified, published, raw); err != context.Canceled {
				t.Fatal(err)
			}
		})
	}
}
