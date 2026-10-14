package v2input

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"testing"
)

func TestSemanticCommitRejectsChangedReopenedInventory(t *testing.T) {
	entries := make([]Entry, 513)
	for i := range entries {
		entries[i] = Entry{Ref: Ref{ArtifactID: uuid.MustParse(fmt.Sprintf("00000000-0000-4000-8000-%012x", i+1)), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, Contract: "TransferCandidate"}
	}
	built, err := BuildInputClosure(context.Background(), uuid.New(), &entryCursor{entries: entries}, &memoryPublisher{bodies: make(map[uuid.UUID][]byte)})
	if err != nil {
		t.Fatal(err)
	}
	task := GraphTask{Built: built, Open: func(context.Context) (Cursor, error) { return &entryCursor{entries: entries}, nil }}
	if _, err := semanticCommitInventory(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	entries[512].Contract = "MaskRef"
	if _, err := semanticCommitInventory(context.Background(), task); !errors.Is(err, ErrInvalid) {
		t.Fatal("changed reopened content accepted", err)
	}
	entries[512].Contract = "TransferCandidate"
	entries[0], entries[1] = entries[1], entries[0]
	if _, err := semanticCommitInventory(context.Background(), task); !errors.Is(err, ErrInvalid) {
		t.Fatal("changed order accepted", err)
	}
	entries[0], entries[1] = entries[1], entries[0]
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := semanticCommitInventory(ctx, task); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
