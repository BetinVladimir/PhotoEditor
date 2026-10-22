package semanticreview

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestPrepareCurrentReviewComposesAuthenticatedReadsAndContentValidation(t *testing.T) {
	sources, documents, model, dataset := preparedDocumentsFixture(t)
	reads, loads := 0, 0
	load := func(context.Context) (PlanSources, error) { loads++; return sources, nil }
	read := func(_ context.Context, source PlanSource) ([]byte, error) {
		reads++
		for _, d := range documents {
			if d.Source == source {
				return d.Body, nil
			}
		}
		return nil, ErrInvalid
	}
	prepared, err := PrepareCurrentReview(context.Background(), load, read, 8<<20, model, dataset)
	if err != nil || len(prepared.Targets) != 3 || reads != len(documents) || loads != 2 {
		t.Fatal(err, reads, loads)
	}
	reads, loads = 0, 0
	load = func(context.Context) (PlanSources, error) {
		loads++
		if loads == 2 {
			sources.Revision++
		}
		return sources, nil
	}
	if _, err := PrepareCurrentReview(context.Background(), load, read, 8<<20, model, dataset); err != ErrConflict {
		t.Fatal("stale review prepared", err)
	}
}

func preparedDocumentsFixture(t *testing.T) (PlanSources, []PlanDocument, string, string) {
	return preparedDocumentsKindFixture(t, "remove_object")
}

func preparedDocumentsKindFixture(t *testing.T, kind string, proposeAll ...bool) (PlanSources, []PlanDocument, string, string) {
	t.Helper()
	raw, err := os.ReadFile("../v2input/testdata/semantic-plan-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Plan      json.RawMessage                `json:"plan"`
		Params    v2input.SemanticAssembleParams `json:"params"`
		Snapshot  json.RawMessage                `json:"snapshot"`
		Documents []struct {
			Ref  v2input.Ref     `json:"ref"`
			Body json.RawMessage `json:"body"`
		} `json:"documents"`
	}
	if json.Unmarshal(raw, &fixture) != nil {
		t.Fatal("fixture")
	}
	plan, err := v2input.DecodeSemanticPlan(fixture.Plan)
	if err != nil {
		t.Fatal(err)
	}
    // Optional multi-target fixture retains each original source binding while
    // giving every candidate the established proposed mask/transform metadata.
    if len(proposeAll)>0 && proposeAll[0] {
        proposed := plan.Candidates[1]
        for i := range plan.Candidates {
            binding := plan.Candidates[i].Target
            plan.Candidates[i] = proposed
            plan.Candidates[i].Target = binding
        }
        for i := range fixture.Documents {
            candidate, err := v2input.DecodeSemanticCandidate(fixture.Documents[i].Body)
            if err != nil {t.Fatal(err)}
            replacement := proposed
            replacement.Target = candidate.Target
            body, err := canonical.Marshal(replacement)
            if err != nil {t.Fatal(err)}
            fixture.Documents[i].Body = body
            fixture.Documents[i].Ref.SHA256, err = canonical.Hash(replacement)
            if err != nil {t.Fatal(err)}
            for n := range fixture.Params.Candidates {
                if fixture.Params.Candidates[n].ArtifactID == fixture.Documents[i].Ref.ArtifactID {
                    fixture.Params.Candidates[n] = fixture.Documents[i].Ref
                }
            }
        }
    }
	var snapshot v2input.SessionInputSnapshot
	if json.Unmarshal(fixture.Snapshot, &snapshot) != nil {
		t.Fatal("snapshot")
	}
	raw, err = os.ReadFile("../../../docs/implementation-v2/contracts/structural-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var structural struct {
		Fixtures map[string]json.RawMessage `json:"fixtures"`
	}
	if json.Unmarshal(raw, &structural) != nil {
		t.Fatal("structural")
	}
	var calibration map[string]any
	if json.Unmarshal(structural.Fixtures["Calibration"], &calibration) != nil {
		t.Fatal("calibration")
	}
	calibration["qualified"] = true
	calibration["loss_weights"] = []float64{1. / 6, 1. / 6, 1. / 6, 1. / 6, 1. / 6, 1. / 6}
	model, dataset := calibration["model_lock_sha"].(string), calibration["dataset_sha"].(string)
	sources := PlanSources{RunID: uuid.New(), TaskID: uuid.New(), ResultID: uuid.New(), Revision: 1}
	var documents []PlanDocument
	add := func(contract string, value any, existing *v2input.Ref) v2input.Ref {
		body, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		sha, err := canonical.Hash(value)
		if err != nil {
			t.Fatal(err)
		}
		ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
		if existing != nil {
			ref = *existing
			if ref.SHA256 != sha {
				t.Fatal("fixture candidate hash")
			}
		}
		source := PlanSource{Contract: contract, Ref: ref, Bytes: int64(len(body)), Bucket: "bucket", Key: ref.ArtifactID.String(), Version: "v1", MediaType: "application/json"}
		sources.Objects = append(sources.Objects, source)
		documents = append(documents, PlanDocument{source, body})
		return ref
	}
	intent := v2input.SemanticEditIntent{SchemaVersion: 1, Kind: "remove_object", Reference: snapshot.Items[0].Binding, ReferenceMask: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, TargetSnapshotID: snapshot.SnapshotID, Strength: .5, PreservePersons: true}
	intent.Kind = kind
	if kind == "local_exposure" {
		ev := .5
		intent.EV = &ev
	}
	intentRef := add("EditIntent", intent, nil)
	calRef := add("Calibration", calibration, nil)
	fixture.Params.Intent = intentRef
	fixture.Params.Calibration = calRef
	fixture.Params.ModelLockSHA = model
	add("Params", fixture.Params, nil)
	add("InputSnapshot", snapshot, nil)
	for _, d := range fixture.Documents {
		var value any
		if json.Unmarshal(d.Body, &value) != nil {
			t.Fatal("candidate")
		}
		add("TransferCandidate", value, &d.Ref)
	}
	plan.IntentSHA = intentRef.SHA256
	plan.Calibration = calRef
	plan.ModelLockSHA = model
	add("SemanticPlan", plan, nil)
	return sources, documents, model, dataset
}

func TestPrepareReviewBindsAllDocumentsAndTrustedQualificationPins(t *testing.T) {
	sources, documents, model, dataset := preparedDocumentsFixture(t)
	prepared, err := PrepareReviewDocuments(sources, documents, model, dataset)
	if err != nil || len(prepared.Targets) != 3 || prepared.Targets[0].State != "unsupported" || prepared.Targets[1].State != "proposed" || prepared.Targets[2].State != "rejected" {
		t.Fatal(prepared, err)
	}
	sources.Objects[0].Version = "changed"
	if prepared.Sources.Objects[0].Version != "v1" {
		t.Fatal("source alias")
	}
	sources.Objects[0].Version = "v1"
	for _, pins := range [][2]string{{strings.Repeat("f", 64), dataset}, {model, strings.Repeat("e", 64)}} {
		if _, err := PrepareReviewDocuments(sources, documents, pins[0], pins[1]); err == nil {
			t.Fatal("foreign qualification accepted")
		}
	}
	if _, err := PrepareReviewDocuments(sources, documents[:len(documents)-1], model, dataset); err == nil {
		t.Fatal("partial documents accepted")
	}
	duplicate := append([]PlanDocument(nil), documents...)
	duplicate[0] = duplicate[1]
	if _, err := PrepareReviewDocuments(sources, duplicate, model, dataset); err == nil {
		t.Fatal("duplicate document accepted")
	}
	changed := append([]PlanDocument(nil), documents...)
	changed[0].Body = append(append([]byte(nil), changed[0].Body...), byte(' '))
	if _, err := PrepareReviewDocuments(sources, changed, model, dataset); err == nil {
		t.Fatal("changed bytes accepted")
	}
}
