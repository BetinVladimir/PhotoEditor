package workflow

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func linesAdmissionFixture(t *testing.T) (PerspectiveLinesPlan, review.ArtifactRef, PerspectiveLinesClosure) {
	t.Helper()
	scope, raw, icc := perspectiveLinesFixture(t, true)
	plan, err := CompilePerspectiveLinesPlan(scope, raw, icc)
	if err != nil {
		t.Fatal(err)
	}
	params := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(plan.Params())}
	closure, err := BuildPerspectiveLinesClosure(context.Background(), plan, params, &linesClosurePublisher{})
	if err != nil {
		t.Fatal(err)
	}
	return plan, params, closure
}
func TestPerspectiveLinesClosureAdmissionOwnsExactTransportEvidence(t *testing.T) {
	plan, params, closure := linesAdmissionFixture(t)
	raw, err := closure.AdmissionJSON(plan, params)
	if err != nil {
		t.Fatal(err)
	}
	var proof struct {
		Task        uuid.UUID       `json:"task_id"`
		Count       int             `json:"entry_count"`
		SHA         string          `json:"entries_sha"`
		Entries     []v2input.Entry `json:"entries"`
		Manifest    v2input.Ref     `json:"manifest"`
		ChunkJCS    string          `json:"chunk_jcs"`
		ManifestJCS string          `json:"manifest_jcs"`
	}
	if json.Unmarshal(raw, &proof) != nil || proof.Task != plan.Task() || proof.Count != 4 || len(proof.Entries) != 4 || proof.SHA != closure.Built().SHA256 || proof.Manifest != closure.Built().Manifest || proof.ChunkJCS != string(closure.documents[closure.built.Chunks[0].ArtifactID]) || proof.ManifestJCS != string(closure.documents[closure.built.Manifest.ArtifactID]) {
		t.Fatal("closure proof lost binding")
	}
}
func TestPerspectiveLinesClosureAdmissionRejectsEveryChangedBinding(t *testing.T) {
	for _, alter := range []func(*PerspectiveLinesClosure){
		func(c *PerspectiveLinesClosure) { c.task = uuid.New() },
		func(c *PerspectiveLinesClosure) { c.built.Count++ },
		func(c *PerspectiveLinesClosure) { c.built.SHA256 = "bad" },
		func(c *PerspectiveLinesClosure) { c.entries[0].Contract = "forged" },
		func(c *PerspectiveLinesClosure) { c.built.Manifest = c.built.Chunks[0] },
		func(c *PerspectiveLinesClosure) { c.built.Chunks = nil },
		func(c *PerspectiveLinesClosure) { c.documents[c.built.Manifest.ArtifactID] = []byte(`{}`) },
		func(c *PerspectiveLinesClosure) { c.documents[c.built.Chunks[0].ArtifactID] = []byte(`{}`) },
		func(c *PerspectiveLinesClosure) { c.documents[uuid.New()] = []byte(`{}`) },
	} {
		plan, params, closure := linesAdmissionFixture(t)
		alter(&closure)
		if _, err := closure.AdmissionJSON(plan, params); err == nil {
			t.Fatal("mutated closure admitted")
		}
	}
	plan, params, _ := linesAdmissionFixture(t)
	if _, err := (PerspectiveLinesClosure{}).AdmissionJSON(plan, params); err == nil {
		t.Fatal("unpublished closure admitted")
	}
}
