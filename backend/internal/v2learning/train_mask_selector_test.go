package v2learning

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type maskRootListFixture []v2input.Ref

func (r maskRootListFixture) List(context.Context, TrainingStart,
	TrainingCandidate) ([]v2input.Ref, error) {
	return r, nil
}

type maskRootReaderFixture struct{ bodies map[v2input.Ref][]byte }

func (r maskRootReaderFixture) Read(_ context.Context, _ auth.Identity,
	_ uuid.UUID, ref v2input.Ref) ([]byte, error) {
	return r.bodies[ref], nil
}

type maskLoaderFixture struct{ calls int }

func (m *maskLoaderFixture) Load(_ context.Context, _ auth.Identity,
	_ TrainingStart, _ TrainingCandidate, _ v2input.Ref,
	_ TrainingImage, _ bool) (TrainingMask, error) {
	m.calls++
	return TrainingMask{}, nil
}

func TestTrainingMaskSelectorRequiresUniqueProtectedRoot(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil || len(images) == 0 {
		t.Fatal("missing image fixture")
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	maskBody := func(protected bool) []byte {
		body, err := canonical.Marshal(map[string]any{
			"artifact": v2input.Ref{ArtifactID: uuid.New(),
				SHA256: strings.Repeat("a", 64)},
			"canvas": image.Canvas, "transform_chain": json.RawMessage(image.TransformChain),
			"object_id": nil, "selector": "manual", "confidence": 1.0,
			"model_lock_id": nil, "protected": protected,
		})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	protectedRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	otherRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	secondRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}
	reader := maskRootReaderFixture{bodies: map[v2input.Ref][]byte{
		protectedRef: maskBody(true), otherRef: maskBody(false),
		secondRef: maskBody(true)}}
	loader := &maskLoaderFixture{}
	selector := TrainingMaskSelector{Roots: maskRootListFixture{otherRef, protectedRef},
		Reader: reader, Masks: loader}
	bound := TrainingImage{Document: image,
		PixelsCount: image.Canvas.Width * image.Canvas.Height}
	got, _, err := selector.SelectProtected(context.Background(), auth.Identity{},
		TrainingStart{TenantID: uuid.New()}, TrainingCandidate{}, bound)
	if err != nil || got != protectedRef || loader.calls != 1 {
		t.Fatalf("unique protected root not selected: %v", err)
	}
	selector.Roots = maskRootListFixture{otherRef, protectedRef, secondRef}
	got, _, err = selector.SelectPinned(context.Background(), auth.Identity{},
		TrainingStart{TenantID: uuid.New()}, TrainingCandidate{}, bound,
		protectedRef)
	if err != nil || got != protectedRef || loader.calls != 2 {
		t.Fatalf("explicit protection not selected: %v", err)
	}
	if _, _, err := selector.SelectPinned(context.Background(), auth.Identity{},
		TrainingStart{TenantID: uuid.New()}, TrainingCandidate{}, bound,
		v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("e", 64)}); err == nil {
		t.Fatal("unlinked protected mask accepted")
	}
	selector.Roots = maskRootListFixture{otherRef}
	if _, _, err := selector.SelectProtected(context.Background(), auth.Identity{},
		TrainingStart{TenantID: uuid.New()}, TrainingCandidate{}, bound); err == nil {
		t.Fatal("missing protection accepted")
	}
	selector.Roots = maskRootListFixture{protectedRef, secondRef}
	if _, _, err := selector.SelectProtected(context.Background(), auth.Identity{},
		TrainingStart{TenantID: uuid.New()}, TrainingCandidate{}, bound); err == nil {
		t.Fatal("ambiguous protection accepted")
	}
}
