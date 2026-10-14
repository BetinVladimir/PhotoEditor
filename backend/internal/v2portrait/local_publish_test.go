package v2portrait

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

type localPlanPublisher struct {
	calls  int
	scope  artifactjson.Scope
	body   []byte
	mode   string
	source uuid.UUID
	cancel context.CancelFunc
}

func (p *localPlanPublisher) Publish(ctx context.Context, scope artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
	p.calls++
	p.scope = scope
	p.body = append([]byte(nil), body...)
	sum := sha256.Sum256(body)
	ref := artifactjson.Ref{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	switch p.mode {
	case "wrong":
		ref.SHA256 = strings.Repeat("e", 64)
	case "alias":
		ref.ID = p.source
	case "error":
		return artifactjson.Ref{}, errors.New("publication_failed")
	case "mutate":
		body[0] = '!'
	}
	if p.cancel != nil {
		p.cancel()
	}
	return ref, nil
}

func TestCompileAndPublishLocalTaskPlanBoundaries(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	_ = json.Unmarshal(raw, &images)
	sourceRaw := images[0]
	source, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(sourceRaw)
	canvas, _ := canonical.Hash(source.Canvas)
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, CanvasHash: canvas}
	cost, _ := LocalPortraitResources(source.Canvas.Width, source.Canvas.Height)
	params, _, _ := (HairParams{SchemaVersion: 1, Base: binding, Mask: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}, Protected: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}, Strength: 1, SaturationFactor: 1}).Compile()
	scope := artifactjson.Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: "task_params", AssemblyLease: uuid.New()}
	for _, mode := range []string{"good", "wrong", "alias", "error", "mutate", "cancel"} {
		ctx, cancel := context.WithCancel(context.Background())
		publisher := &localPlanPublisher{mode: mode, source: binding.Source.ArtifactID}
		if mode == "cancel" {
			publisher.cancel = cancel
		}
		got, e := CompileAndPublishLocalTaskPlan(ctx, publisher, scope, "portrait.hair", binding, sourceRaw, params, strings.Repeat("a", 64), cost)
		cancel()
		if publisher.calls != 1 || publisher.scope != scope || !bytes.Equal(publisher.body, params) {
			t.Fatal("publication scope/bytes changed")
		}
		if mode == "good" || mode == "mutate" {
			if e != nil || got.ParamsRef.ArtifactID == uuid.Nil || !bytes.Equal(got.Plan.Params, params) {
				t.Fatal("published plan", mode, e)
			}
		} else {
			if e == nil || got.ParamsRef != (v2input.Ref{}) || got.Plan.Params != nil || got.Plan.Profile != nil {
				t.Fatal("partial publication escaped", mode, e)
			}
		}
	}
	invalid := scope
	invalid.Purpose = "other"
	publisher := &localPlanPublisher{}
	got, e := CompileAndPublishLocalTaskPlan(context.Background(), publisher, invalid, "portrait.hair", binding, sourceRaw, params, strings.Repeat("a", 64), cost)
	if e == nil || publisher.calls != 0 || got.Plan.Params != nil {
		t.Fatal("invalid scope performed IO")
	}
}
