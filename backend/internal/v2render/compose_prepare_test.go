package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type compositionResolverFake struct {
	request CompositionPreparation
	calls   int
	reject  bool
}

func (r *compositionResolverFake) ResolveTaskInputs(ctx context.Context, actor auth.Identity, tenant uuid.UUID, roots []v2input.Entry, base v2input.SessionVersionBinding) ([]v2input.Entry, error) {
	r.calls++
	if tenant != r.request.Tenant || actor != r.request.Actor || base != r.request.Base {
		return nil, v2input.ErrInvalid
	}
	if r.reject {
		return nil, errors.New("scope rejected")
	}
	return append([]v2input.Entry(nil), roots...), nil
}

func TestPrepareCompositionConnectsFrozenRootsTransportAndGraphTask(t *testing.T) {
	layer, base, proposal := compositionInputs()
	compiled, _ := CompileComposition(layer, base, nil, proposal, nil, 2, 1)
	digest := sha256.Sum256(compiled.JSON)
	request := CompositionPreparation{Tenant: uuid.New(), Task: uuid.New(), Actor: auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, Layer: layer, Base: base, ProposalResult: proposal, Width: 2, Height: 1, ParamsRef: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(digest[:])}}
	resolver := &compositionResolverFake{request: request}
	publisher := &compositionTransportPublisher{}
	prepared, err := PrepareComposition(context.Background(), request, resolver, publisher)
	if err != nil || resolver.calls != 1 || publisher.calls != 2 || prepared.GraphTask.ID != request.Task || prepared.GraphTask.Open == nil || prepared.GraphTask.Built.Count != 4 || len(prepared.GraphTask.Direct) != 1 || len(prepared.InputDigest) != 64 {
		t.Fatal(prepared, err)
	}
	collisionPublisher := &compositionTransportPublisher{firstID: proposal.ArtifactID}
	if _, err := PrepareComposition(context.Background(), request, resolver, collisionPublisher); err == nil {
		t.Fatal("transport reused nested dependency artifact ID")
	}
	resolver.reject = true
	publisher.calls = 0
	if _, err := PrepareComposition(context.Background(), request, resolver, publisher); err == nil || publisher.calls != 0 {
		t.Fatal("published before scoped resolution")
	}
	resolver.reject = false
	resolver.calls = 0
	request.ParamsRef.SHA256 = base.Source.SHA256
	if _, err := PrepareComposition(context.Background(), request, resolver, publisher); err == nil || resolver.calls != 0 || publisher.calls != 0 {
		t.Fatal("wrong frozen params reached IO")
	}
}
