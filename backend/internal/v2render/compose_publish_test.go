package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type compositionPublisherFunc func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, []byte) (v2input.Ref, error)

func (f compositionPublisherFunc) Publish(ctx context.Context, who auth.Identity, tenant, version, document uuid.UUID, raw []byte) (v2input.Ref, error) {
	return f(ctx, who, tenant, version, document, raw)
}

func TestCompositionParamsPublicationRetriesAndOwnsReferences(t *testing.T) {
	layer, base, proposal := compositionInputs()
	source := proposal
	request := CompositionPreparation{Tenant: uuid.New(), Task: uuid.New(), Actor: auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, Layer: layer, Base: base, SourceResult: &source, ProposalResult: proposal, Width: 5, Height: 5}
	compiled, err := CompileComposition(layer, base, &source, proposal, nil, 5, 5)
	if err != nil {
		t.Fatal(err)
	}
	artifact := uuid.New()
	calls := 0
	publisher := compositionPublisherFunc(func(ctx context.Context, who auth.Identity, tenant, version, document uuid.UUID, raw []byte) (v2input.Ref, error) {
		calls++
		if who != request.Actor || tenant != request.Tenant || version != base.AssetVersionID || document != uuid.NewSHA1(request.Task, []byte("layer.compose.params:1")) || string(raw) != string(compiled.JSON) {
			t.Fatal("publication identity or bytes changed")
		}
		sum := sha256.Sum256(raw)
		return v2input.Ref{ArtifactID: artifact, SHA256: hex.EncodeToString(sum[:])}, nil
	})
	first, err := PublishCompositionParams(context.Background(), request, publisher)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PublishCompositionParams(context.Background(), request, publisher)
	if err != nil || calls != 2 || first.ParamsRef != second.ParamsRef {
		t.Fatal("retry changed publication", err)
	}
	source.ArtifactID = uuid.New()
	request.Layer.Mask[0] = ' '
	if first.SourceResult.ArtifactID != proposal.ArtifactID || string(first.Layer.Mask) != string(compiled.Params.Mask) {
		t.Fatal("returned request aliases caller")
	}
}

func TestCompositionParamsPublicationRejectsFailureAndInvalidReceipt(t *testing.T) {
	layer, base, proposal := compositionInputs()
	request := CompositionPreparation{Tenant: uuid.New(), Task: uuid.New(), Actor: auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, Layer: layer, Base: base, ProposalResult: proposal, Width: 5, Height: 5}
	sentinel := errors.New("publication failed")
	for _, outcome := range []struct {
		ref v2input.Ref
		err error
	}{
		{err: sentinel}, {}, {ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: proposal.SHA256}},
	} {
		publisher := compositionPublisherFunc(func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, []byte) (v2input.Ref, error) {
			return outcome.ref, outcome.err
		})
		_, err := PublishCompositionParams(context.Background(), request, publisher)
		if err == nil || (outcome.err != nil && !errors.Is(err, sentinel)) {
			t.Fatal("invalid receipt/failure accepted", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	publisher := compositionPublisherFunc(func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, []byte) (v2input.Ref, error) {
		called = true
		return v2input.Ref{}, nil
	})
	if _, err := PublishCompositionParams(ctx, request, publisher); !errors.Is(err, context.Canceled) || called {
		t.Fatal("cancelled publication performed IO", err)
	}
	request.Task = uuid.Nil
	if _, err := PublishCompositionParams(context.Background(), request, publisher); err == nil || called {
		t.Fatal("invalid task performed IO", err)
	}
}
