package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestAuthoredFaceCandidatePublicationCancellationPrecedesDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	candidate, sha, err := (AuthoredFaceCandidatePublication{}).Publish(ctx, auth.Identity{}, AuthoredInstructionSource{})
	if !errors.Is(err, context.Canceled) || sha != "" || candidate.Parent != uuid.Nil {
		t.Fatalf("canceled publication: %v", err)
	}
}
func TestAuthoredFaceCandidatePublicationRefusesForeignActorBeforePhysicalRead(t *testing.T) {
	source := AuthoredInstructionSource{Actor: uuid.New(), Session: uuid.New()}
	for _, who := range []auth.Identity{{UserID: uuid.New(), SessionID: source.Session}, {UserID: source.Actor, SessionID: uuid.New()}} {
		candidate, sha, err := (AuthoredFaceCandidatePublication{}).Publish(context.Background(), who, source)
		if !errors.Is(err, v2input.ErrInvalid) || sha != "" || candidate.Parent != uuid.Nil {
			t.Fatalf("foreign identity: %v", err)
		}
	}
}
