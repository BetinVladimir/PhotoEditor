package v2optical

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestMultiDraftOwnsRecoveryAndRebuildsDescriptor(t *testing.T) {
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
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			tenant, project, snapshot := uuid.New(), uuid.New(), uuid.New()
			key := uuid.NewString()
			draft, err := CompileMultiDraft(context.Background(), who, tenant, project, snapshot, key, operation, params, originals, qualified, capacity)
			if err != nil {
				t.Fatal(err)
			}
			again, err := CompileMultiDraft(context.Background(), who, tenant, project, snapshot, key, operation, params, originals, qualified, capacity)
			if err != nil || draft.raw != again.raw {
				t.Fatal("nondeterministic draft", err)
			}
			s, err := draft.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			published := v2input.Ref{ArtifactID: s.ParamsDocument, SHA256: s.ParamsSHA}
			descriptor, err := draft.Descriptor(context.Background(), qualified, published)
			if err != nil {
				t.Fatal(err)
			}
			var decoded struct {
				Task    v2input.DraftTask `json:"task"`
				Entries []v2input.Entry   `json:"entries"`
			}
			if json.Unmarshal(descriptor, &decoded) != nil || decoded.Task.ID != s.Task || decoded.Task.Operation != operation || len(decoded.Entries) != 7 {
				t.Fatal("descriptor scope lost")
			}
			s.SourcesJCS[0] = "mutated"
			s.Profile[0] = ' '
			params[0] = ' '
			originals[0][0] = ' '
			repeated, err := draft.Descriptor(context.Background(), qualified, published)
			if err != nil || !bytes.Equal(descriptor, repeated) {
				t.Fatal("mutable caller changed draft", err)
			}
		})
	}
}
func TestMultiDraftDescriptorRejectsTamperedRecovery(t *testing.T) {
	operation := "optical.panorama"
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
	for _, fault := range []string{"task", "params", "source", "profile", "reserve", "published"} {
		t.Run(fault, func(t *testing.T) {
			s, _ := draft.Snapshot()
			published := v2input.Ref{ArtifactID: s.ParamsDocument, SHA256: s.ParamsSHA}
			switch fault {
			case "task":
				s.Task = uuid.New()
			case "params":
				s.ParamsSHA = pin.DatasetSHA
			case "source":
				s.SourcesJCS[0], s.SourcesJCS[1] = s.SourcesJCS[1], s.SourcesJCS[0]
			case "profile":
				s.Profile = json.RawMessage(`{}`)
			case "reserve":
				s.ReservedBytes++
			case "published":
				published.ArtifactID = uuid.New()
			}
			raw, err := canonical.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = (MultiDraft{raw: string(raw)}).Descriptor(context.Background(), qualified, published); err == nil {
				t.Fatal("tampered recovery accepted")
			}
		})
	}
}
