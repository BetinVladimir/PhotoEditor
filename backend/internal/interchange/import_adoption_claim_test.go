package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestImportAdoptionClaimBindsTaskAndFence(t *testing.T) {
	workflow := uuid.New()
	claim := ImportAdoptionClaim{
		Tenant: uuid.New(), Workflow: workflow, Run: uuid.New(),
		Task:   uuid.NewSHA1(workflow, []byte("v2.interchange.import:0")),
		Result: uuid.New(), Attempt: 1, Fence: 1, Token: uuid.New(),
	}
	if err := claim.validate(); err != nil {
		t.Fatal(err)
	}
	foreign := claim
	foreign.Task = uuid.New()
	if foreign.validate() == nil {
		t.Fatal("foreign task accepted")
	}
	stale := claim
	stale.Fence = 0
	if stale.validate() == nil {
		t.Fatal("unfenced claim accepted")
	}
	stale = claim
	stale.Token = uuid.Nil
	if stale.validate() == nil {
		t.Fatal("tokenless claim accepted")
	}
}

func TestImportAdoptionClaimStoreRejectsAbsentPoolAndCancellation(t *testing.T) {
	store := SQLImportAdoptionClaimStore{}
	if _, err := store.NextImportAdoption(context.Background()); err == nil {
		t.Fatal("missing scheduler pool accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.NextImportAdoption(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled claim discovery continued", err)
	}
}
