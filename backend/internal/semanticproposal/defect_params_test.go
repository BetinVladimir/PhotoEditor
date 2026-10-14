package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/review"
)

type defectParamsPublisher struct {
	calls  int
	scope  artifactjson.Scope
	body   []byte
	ref    artifactjson.Ref
	err    error
	cancel context.CancelFunc
}

func (p *defectParamsPublisher) Publish(ctx context.Context, scope artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
	p.calls++
	p.scope = scope
	p.body = append([]byte{}, body...)
	if p.cancel != nil {
		p.cancel()
	}
	if p.err != nil {
		return artifactjson.Ref{}, p.err
	}
	sum := sha256.Sum256(body)
	ref := p.ref
	if ref.SHA256 == "" {
		ref.SHA256 = hex.EncodeToString(sum[:])
	}
	return ref, nil
}
func TestCompileAndPublishDefectParams(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []json.RawMessage
	if err = json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	body := fixtures[0]
	sum := sha256.Sum256(body)
	imageRef := review.ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	image, err := review.ParseWorkingImage(body)
	if err != nil {
		t.Fatal(err)
	}
	scope := artifactjson.Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: "task_params"}
	publisher := &defectParamsPublisher{ref: artifactjson.Ref{ID: uuid.New()}}
	result, err := CompileAndPublishDefectParams(context.Background(), publisher, scope, imageRef, body, []review.DefectProposalFaceInput{}, [2]float64{.002, .012}, nil, nil, nil)
	if err != nil || publisher.calls != 1 || publisher.scope != scope || result.Ref.ArtifactID != publisher.ref.ID || len(result.Inputs) != 2 || result.ManualRoots == nil {
		t.Fatal(result, err)
	}
	compiled, err := review.BuildDefectProposalTaskInputsFromImage(context.Background(), imageRef, body, []review.DefectProposalFaceInput{}, [2]float64{.002, .012}, nil, nil, nil)
	if err != nil || string(compiled.Params) != string(publisher.body) || result.Ref.SHA256 != compiled.ParamsSHA256 {
		t.Fatal("published Params changed", err)
	}
	prepared, err := PrepareDefectParams(context.Background(), imageRef, body, []review.DefectProposalFaceInput{}, [2]float64{.002, .012}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	firstPublisher := &defectParamsPublisher{ref: artifactjson.Ref{ID: uuid.New()}}
	first, err := PublishPreparedDefectParams(context.Background(), firstPublisher, scope, prepared)
	if err != nil {
		t.Fatal(err)
	}
	first.Inputs[0].SHA256 = "changed"
	firstPublisher.body[0] = ' '
	secondPublisher := &defectParamsPublisher{ref: artifactjson.Ref{ID: uuid.New()}}
	second, err := PublishPreparedDefectParams(context.Background(), secondPublisher, scope, prepared)
	if err != nil || string(secondPublisher.body) != string(compiled.Params) || second.Inputs[0] == first.Inputs[0] {
		t.Fatal("prepared Params alias escaped", err)
	}
	for _, invalid := range []PreparedDefectParams{{}, {params: prepared.params, digest: imageRef.SHA256, inputs: prepared.inputs, roots: prepared.roots}} {
		p := &defectParamsPublisher{}
		got, err := PublishPreparedDefectParams(context.Background(), p, scope, invalid)
		if err == nil || p.calls != 0 || got.Inputs != nil {
			t.Fatal("invalid prepared Params published")
		}
	}
	for _, badScope := range []artifactjson.Scope{{}, {Tenant: scope.Tenant, Run: scope.Run, Owner: scope.Owner, Purpose: "review_selection"}} {
		p := &defectParamsPublisher{}
		denied, err := CompileAndPublishDefectParams(context.Background(), p, badScope, imageRef, body, []review.DefectProposalFaceInput{}, [2]float64{.002, .012}, nil, nil, nil)
		if err == nil || p.calls != 0 || denied.Inputs != nil {
			t.Fatal("invalid scope published")
		}
	}
	p := &defectParamsPublisher{}
	denied, err := CompileAndPublishDefectParams(context.Background(), p, scope, imageRef, append(append([]byte{}, body...), ' '), []review.DefectProposalFaceInput{}, [2]float64{.002, .012}, nil, nil, nil)
	if err == nil || p.calls != 0 || denied.Inputs != nil {
		t.Fatal("changed source published")
	}
	failure := errors.New("publication failed")
	for _, p := range []*defectParamsPublisher{
		{ref: artifactjson.Ref{ID: uuid.New(), SHA256: imageRef.SHA256}},
		{ref: artifactjson.Ref{ID: image.Pixels.ID}},
		{},
		{err: failure},
	} {
		denied, err := CompileAndPublishDefectParams(context.Background(), p, scope, imageRef, body, []review.DefectProposalFaceInput{}, [2]float64{.002, .012}, nil, nil, nil)
		if err == nil || p.calls != 1 || denied.Ref.ArtifactID != uuid.Nil || denied.Inputs != nil || denied.ManualRoots != nil {
			t.Fatal("invalid publisher result accepted", denied, err)
		}
		if p.err != nil && !errors.Is(err, failure) {
			t.Fatal("publication error lost")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	p = &defectParamsPublisher{ref: artifactjson.Ref{ID: uuid.New()}, cancel: cancel}
	denied, err = CompileAndPublishDefectParams(ctx, p, scope, imageRef, body, []review.DefectProposalFaceInput{}, [2]float64{.002, .012}, nil, nil, nil)
	if !errors.Is(err, context.Canceled) || denied.Inputs != nil || p.calls != 1 {
		t.Fatal("cancellation after publication accepted")
	}
}
