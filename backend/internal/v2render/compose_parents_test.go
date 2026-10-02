package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type rejectedCompositionCatalog struct{ calls int }

func (c *rejectedCompositionCatalog) Lookup(context.Context, auth.Identity, uuid.UUID, v2draft.LayerRef) (objectstore.Object, error) {
	c.calls++
	return objectstore.Object{}, v2input.ErrInvalid
}

func TestVerifiedCompositionRejectsBeforePublication(t *testing.T) {
	layer, base, proposal := compositionInputs()
	compiled, err := CompileComposition(layer, base, nil, proposal, nil, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(compiled.JSON)
	request := CompositionPreparation{Tenant: uuid.New(), Task: uuid.New(), Actor: auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, Layer: layer, Base: base, ProposalResult: proposal, Width: 2, Height: 1, ParamsRef: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}}
	catalog := &rejectedCompositionCatalog{}
	reader := &adoptedRootFixture{}
	publisher := &compositionTransportPublisher{}
	if _, err := PrepareVerifiedComposition(context.Background(), request, catalog, reader, publisher); err == nil || catalog.calls != 1 || reader.opens != 0 || publisher.calls != 0 {
		t.Fatal("scope denial reached publication", err)
	}
	catalog.calls = 0
	bad := request
	bad.ParamsRef.SHA256 = base.Source.SHA256
	if _, err := PrepareVerifiedComposition(context.Background(), bad, catalog, reader, publisher); err == nil || catalog.calls != 0 {
		t.Fatal("invalid params reached catalog")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PrepareVerifiedComposition(ctx, request, catalog, reader, publisher); err != context.Canceled || catalog.calls != 0 {
		t.Fatal("canceled request reached catalog", err)
	}
}
