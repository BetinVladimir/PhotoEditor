package v2draft

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type openEyesPreparationFake struct {
	value OpenEyesClientPreparation
	calls int
}

func (f *openEyesPreparationFake) PrepareProjectKeyed(_ context.Context, _ auth.Identity,
	_, _ uuid.UUID, _ string, _ OpenEyesClientSelection) (OpenEyesClientPreparation, error) {
	f.calls++
	return f.value, nil
}

type openEyesBeginFake struct {
	identity OpenEyesDraftIdentity
	calls    int
}

func (f *openEyesBeginFake) Begin(_ context.Context, _ OpenEyesDraftBeginRequest) (OpenEyesDraftIdentity, error) {
	f.calls++
	return f.identity, nil
}

type openEyesIssuerFake struct {
	calls int
}

func (f *openEyesIssuerFake) Issue(_ context.Context, _ auth.Identity,
	_, _, _, _ uuid.UUID, params v2portrait.OpenEyesParams,
	_, _ uuid.UUID) (v2portrait.OpenEyesParams, error) {
	f.calls++
	return params, nil
}

func TestOpenEyesIssueRefusesChangedPreparationBeforeBegin(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	intent := OpenEyesClientIntent{SchemaVersion: 1, Shoot: uuid.New(), Snapshot: uuid.New(),
		SourceObservation: uuid.New(), DonorObservation: uuid.New()}
	preparation := &openEyesPreparationFake{value: intent}
	beginner := &openEyesBeginFake{identity: OpenEyesDraftIdentity{Run: uuid.New(), Workflow: uuid.New(), Owner: uuid.New()}}
	issuer := &openEyesIssuerFake{}
	service := OpenEyesDraftIssuer{Preparation: preparation, Beginner: beginner, Issuer: issuer}
	changed := intent
	changed.DonorObservation = uuid.New()
	if _, err := service.IssueAfterBegin(context.Background(), who, uuid.New(), uuid.New(), "eyes-issue", changed); err != v2input.ErrExpired || beginner.calls != 0 || issuer.calls != 0 {
		t.Fatal("changed preparation reached durable begin")
	}
	intent.Proposal.Confirmation.ArtifactID = beginner.identity.Owner
	preparation.value = intent
	if got, err := service.IssueAfterBegin(context.Background(), who, uuid.New(), uuid.New(), "eyes-issue", intent); err != nil || got.Identity != beginner.identity || got.Params != intent.Proposal || beginner.calls != 1 || issuer.calls != 1 {
		t.Fatal("matching preparation did not issue under new draft", err)
	}
}
