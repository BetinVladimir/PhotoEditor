package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestMultiSourceClosureRequiresJSONAndPhysicalArrays(t *testing.T) {
	for _, op := range []string{"optical.panorama", "optical.focus_stack"} {
		for _, corrupt := range []bool{false, true} {
			t.Run(op+map[bool]string{false: "/valid", true: "/bad-array"}[corrupt], func(t *testing.T) {
				v, task, s, o := adoptedArrayFixture(t, op, adoptedArrayNPY("(100,100,3)", 3, 3.5), adoptedArrayNPY("(100,100)", 1, .5))
				for _, item := range v.inventory.items {
					if item.Role == "WorkingImage" {
						o.bodies[item.Object.Key] = []byte(v.documents[item.Ordinal])
					}
				}
				if corrupt {
					o.bodies["original/pixels"] = []byte("bad array")
				}
				got, err := LoadAdoptedMultiSourceClosure(context.Background(), s, o, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), task, v.binding, "photos")
				if corrupt {
					if !errors.Is(err, v2input.ErrInvalid) {
						t.Fatalf("invalid bytes: %v", err)
					}
					if _, err := got.Binding(); !errors.Is(err, v2input.ErrInvalid) {
						t.Fatal("failure exposed proof")
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					binding, err := got.Binding()
					if err != nil || binding != v.binding {
						t.Fatal("wrong binding")
					}
					images, err := got.Images()
					if err != nil || len(images) != 2 {
						t.Fatal("wrong images")
					}
					docs, err := got.Documents()
					if err != nil || len(docs) != 2 {
						t.Fatal("wrong documents")
					}
					docs[0][0] = 'x'
					again, err := got.Documents()
					if err != nil || string(again[0]) != v.documents[0] {
						t.Fatal("documents alias caller")
					}
					if o.opened != 6 || s.calls != 11 {
						t.Fatalf("incomplete pipeline: opens=%d inventory=%d", o.opened, s.calls)
					}
				}
				if o.opened != o.closed {
					t.Fatal("stream leak")
				}
			})
		}
	}
}
func TestMultiSourceClosureZeroValueIsNotProof(t *testing.T) {
	var v VerifiedMultiSourceClosure
	if _, err := v.Binding(); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("zero binding accepted")
	}
	if _, err := v.Documents(); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("zero documents accepted")
	}
	if _, err := v.Images(); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("zero images accepted")
	}
}
