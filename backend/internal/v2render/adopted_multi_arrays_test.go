package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestAdoptedMultiArraysRejectStaleInventoryBeforeOpen(t *testing.T) {
	for _, operation := range []string{"optical.panorama", "optical.focus_stack"} {
		t.Run(operation, func(t *testing.T) {
			binding, task, inv, raw := originalInventoryFixture(t, operation)
			docs := []string{string(raw), string(raw)}
			verified := VerifiedMultiOriginals{binding: binding, documents: docs, inventory: inv.owned()}
			stale := inv.owned()
			stale.items[0].Object.VersionID = "replacement"
			source := &multiOriginalInventoryTestSource{inventory: stale}
			objects := &multiReadObjects{raw: raw}
			err := VerifyAdoptedMultiOriginalArrays(context.Background(), source, objects, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), task, verified)
			if !errors.Is(err, v2input.ErrExpired) || objects.opened != 0 {
				t.Fatalf("stale inventory opened: %v, opens=%d", err, objects.opened)
			}
		})
	}
}

func TestAdoptedMultiArraysRejectInvalidNPYAndClose(t *testing.T) {
	binding, task, inv, raw := originalInventoryFixture(t, "optical.panorama")
	verified := VerifiedMultiOriginals{binding: binding, documents: []string{string(raw), string(raw)}, inventory: inv}
	source := &multiOriginalInventoryTestSource{inventory: inv}
	objects := &multiReadObjects{raw: []byte("not an NPY")}
	err := VerifyAdoptedMultiOriginalArrays(context.Background(), source, objects, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), task, verified)
	if !errors.Is(err, v2input.ErrInvalid) || objects.opened != 1 || objects.closed != 1 {
		t.Fatalf("invalid stream: %v opens=%d closes=%d", err, objects.opened, objects.closed)
	}
}

func TestAdoptedMultiArraysCancelledBeforeCallbacks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	source := &multiOriginalInventoryTestSource{}
	objects := &multiReadObjects{}
	err := VerifyAdoptedMultiOriginalArrays(ctx, source, objects, auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, VerifiedMultiOriginals{})
	if !errors.Is(err, context.Canceled) || source.calls != 0 || objects.opened != 0 {
		t.Fatalf("cancelled: %v", err)
	}
}
