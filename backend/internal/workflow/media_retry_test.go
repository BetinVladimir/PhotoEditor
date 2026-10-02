package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/review"
)

type mediaRetryPorts struct {
	lease                    *MediaRetryLease
	begun, sealed, released  int
	registered, materialized []uuid.UUID
	failPublish              bool
}

func (p *mediaRetryPorts) Claim(context.Context) (*MediaRetryLease, error) { return p.lease, nil }
func (p *mediaRetryPorts) Release(context.Context, MediaRetryLease) (bool, error) {
	p.released++
	return true, nil
}
func (p *mediaRetryPorts) Begin(context.Context, MediaRetryLease, uuid.UUID, string, json.RawMessage) (bool, error) {
	p.begun++
	return true, nil
}
func (p *mediaRetryPorts) Register(_ context.Context, _ MediaRetryLease, _ uuid.UUID, n MediaRetryDocument, _ string) (bool, error) {
	p.registered = append(p.registered, n.ID)
	return true, nil
}
func (p *mediaRetryPorts) Materialize(_ context.Context, _ MediaRetryLease, _ uuid.UUID, n PreparedTask) (bool, error) {
	p.materialized = append(p.materialized, n.ID)
	return true, nil
}
func (p *mediaRetryPorts) Seal(context.Context, MediaRetryLease, uuid.UUID) (bool, error) {
	p.sealed++
	return true, nil
}
func (p *mediaRetryPorts) Publish(_ context.Context, s artifactjson.Scope, raw []byte) (artifactjson.Ref, error) {
	if p.failPublish {
		return artifactjson.Ref{}, errors.New("store down")
	}
	sum := sha256.Sum256(raw)
	return artifactjson.Ref{ID: uuid.NewSHA1(s.Owner, []byte("params")), SHA256: hex.EncodeToString(sum[:])}, nil
}

func TestBuildMediaRetryDocumentsAreDeterministicAndComplete(t *testing.T) {
	event, version := uuid.New(), uuid.New()
	master := MediaMaster{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Bytes: 1000, Width: 6000, Height: 4000, MediaType: "image/jpeg"}
	first, err := BuildMediaRetryDocuments(event, version, master, []string{"preview", "thumbnail"}, strings.Repeat("b", 64))
	if err != nil || len(first) != 4 {
		t.Fatal(first, err)
	}
	second, err := BuildMediaRetryDocuments(event, version, master, []string{"thumbnail", "preview"}, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	for i, item := range first {
		if item.ID != second[i].ID || string(item.Params) != string(second[i].Params) || len(item.Inputs) != 1 || item.Inputs[0] != master.Artifact {
			t.Fatal("nondeterministic plan", i)
		}
		var params map[string]any
		if json.Unmarshal(item.Params, &params) != nil || len(params) != 5 || params["source_kind"] != "master" || params["suffix"] != ".jpg" || params["profile_hash"] == nil {
			t.Fatal(string(item.Params))
		}
	}
}

func TestPrepareMediaRetryTaskBindsParamsAndBudget(t *testing.T) {
	master := MediaMaster{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Bytes: 1000, Width: 6000, Height: 4000, MediaType: "image/jpeg"}
	docs, err := BuildMediaRetryDocuments(uuid.New(), uuid.New(), master, []string{"thumbnail"}, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	params := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	task, err := PrepareMediaRetryTask(PublishedMediaRetry{Document: docs[0], Params: params}, master)
	if err != nil || task.ResourceClass != "media" || task.Params != params || task.InputDigest == "" {
		t.Fatal(task, err)
	}
	if _, err = ParseResourceEstimate(task.ExecutionProfile); err != nil {
		t.Fatal(err)
	}
}

func TestBuildMediaRetryRejectsUnsafeSources(t *testing.T) {
	valid := MediaMaster{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Bytes: 1, Width: 1, Height: 1, MediaType: "image/jpeg"}
	for _, change := range []func(*MediaMaster){func(m *MediaMaster) { m.MediaType = "application/octet-stream" }, func(m *MediaMaster) { m.Width = 200000000 }, func(m *MediaMaster) { m.Artifact.SHA256 = "bad" }} {
		m := valid
		change(&m)
		if _, err := BuildMediaRetryDocuments(uuid.New(), uuid.New(), m, []string{"thumbnail"}, strings.Repeat("b", 64)); err == nil {
			t.Fatal("accepted", m)
		}
	}
	if _, err := BuildMediaRetryDocuments(uuid.New(), uuid.New(), valid, []string{"thumbnail", "thumbnail"}, strings.Repeat("b", 64)); err == nil {
		t.Fatal("accepted duplicates")
	}
}

func TestBuildMediaRetryFromWorkingMasterDeclaresNestedInputs(t *testing.T) {
	manifest, pixels, alpha := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("b", 64)}, review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	working := review.WorkingImage{Pixels: pixels, Alpha: &alpha, DType: "float32", Canvas: review.Canvas{Width: 120, Height: 80}, Color: review.ColorSpace{ProfileID: "work_rec2020_gamma24_v1", Encoding: "gamma24", ICCSHA256: strings.Repeat("d", 64)}}
	docs, err := BuildMediaRetryDocuments(uuid.New(), uuid.New(), MediaMaster{Artifact: manifest, Bytes: 500, Working: &working}, []string{"preview"}, strings.Repeat("e", 64))
	if err != nil || len(docs) != 2 {
		t.Fatal(docs, err)
	}
	for _, doc := range docs {
		if len(doc.Inputs) != 3 || doc.Inputs[0] != manifest || doc.Inputs[1] != pixels || doc.Inputs[2] != alpha {
			t.Fatal(doc.Inputs)
		}
		var p map[string]any
		_ = json.Unmarshal(doc.Params, &p)
		if p["source_kind"] != "working" || p["suffix"] != nil || len(p) != 4 {
			t.Fatal(string(doc.Params))
		}
	}
}

func TestMediaRetryCoordinatorSealsDeterministicPlan(t *testing.T) {
	manifest, pixels := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	working := review.WorkingImage{Pixels: pixels, DType: "float32", Canvas: review.Canvas{Width: 120, Height: 80}, Color: review.ColorSpace{ProfileID: "work_rec2020_gamma24_v1", Encoding: "gamma24", ICCSHA256: strings.Repeat("c", 64)}}
	p := &mediaRetryPorts{lease: &MediaRetryLease{EventID: uuid.New(), Token: uuid.New(), TenantID: uuid.New(), AssetVersionID: uuid.New(), Variants: []string{"thumbnail"}, Master: MediaMaster{Artifact: manifest, Bytes: 100, Working: &working}}}
	done, err := (MediaRetryCoordinator{Store: p, Publisher: p, SRGBSHA: strings.Repeat("d", 64)}).Process(context.Background())
	if err != nil || !done || p.begun != 1 || p.sealed != 1 || p.released != 0 || len(p.registered) != 2 || len(p.materialized) != 2 {
		t.Fatal(done, err, p)
	}
	for i := range p.registered {
		if p.registered[i] != p.materialized[i] {
			t.Fatal("node/task identity mismatch")
		}
	}
}

func TestMediaRetryCoordinatorReleasesLeaseOnObjectFailure(t *testing.T) {
	master := MediaMaster{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Bytes: 1, Width: 1, Height: 1, MediaType: "image/jpeg"}
	p := &mediaRetryPorts{failPublish: true, lease: &MediaRetryLease{EventID: uuid.New(), Token: uuid.New(), TenantID: uuid.New(), AssetVersionID: uuid.New(), Variants: []string{"preview"}, Master: master}}
	done, err := (MediaRetryCoordinator{Store: p, Publisher: p, SRGBSHA: strings.Repeat("b", 64)}).Process(context.Background())
	if err == nil || done || p.released != 1 || p.sealed != 0 {
		t.Fatal(done, err, p.released, p.sealed)
	}
}
