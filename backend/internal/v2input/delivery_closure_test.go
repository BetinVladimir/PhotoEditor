package v2input

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestDeliveryClosureOwnsInputsAndRejectsMissingRoots(t *testing.T) {
	ref := func(n int) Ref { return Ref{uuid.New(), fmt.Sprintf("%064x", n)} }
	frozen, snapshot, config := Entry{ref(1), "FrozenDeliveryInput"}, Entry{ref(2), "InputSnapshot"}, Entry{ref(3), "ExportPresetConfig"}
	params := DeliveryPrepareParams{1, frozen.Ref}
	sha, err := canonical.Hash(params)
	if err != nil {
		t.Fatal(err)
	}
	paramsRef := Ref{uuid.New(), sha}
	workflow := uuid.New()
	roots := []Entry{snapshot, config}
	resolved := []Entry{frozen, snapshot, config}
	closure, err := CompileDeliveryPrepareClosure(workflow, params, paramsRef, roots, resolved)
	if err != nil {
		t.Fatal(err)
	}
	again, err := CompileDeliveryPrepareClosure(workflow, params, paramsRef, roots, []Entry{config, snapshot, frozen})
	if err != nil || again.InputDigest != closure.InputDigest {
		t.Fatal("unstable inventory")
	}
	if _, err := CompileDeliveryPrepareClosure(workflow, params, paramsRef, roots, resolved[:2]); err == nil {
		t.Fatal("missing config accepted")
	}
	collision := append(append([]Entry(nil), resolved...), Entry{snapshot.Ref, "WorkingImage"})
	if _, err := CompileDeliveryPrepareClosure(workflow, params, paramsRef, roots, collision); err == nil {
		t.Fatal("contract collision accepted")
	}
	bad := paramsRef
	bad.SHA256 = fmt.Sprintf("%064x", 99)
	if _, err := CompileDeliveryPrepareClosure(workflow, params, bad, roots, resolved); err == nil {
		t.Fatal("wrong params SHA accepted")
	}
	resolved[0] = Entry{}
	cursor, err := closure.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for {
		_, ok, err := cursor.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		count++
	}
	if count != 4 {
		t.Fatalf("owned inventory count = %d", count)
	}
}

func TestDeliveryPrepareLargeInventoryKeepsNestedInputsIndexed(t *testing.T) {
	ref := func(n int) Ref { return Ref{uuid.New(), fmt.Sprintf("%064x", n)} }
	frozen, snapshot, config := Entry{ref(1), "FrozenDeliveryInput"}, Entry{ref(2), "InputSnapshot"}, Entry{ref(3), "ExportPresetConfig"}
	required := []Entry{frozen, snapshot, config}
	resolved := append([]Entry(nil), required...)
	for n := 0; n < 1000; n++ {
		resolved = append(resolved, Entry{ref(n + 4), "WorkingImage"})
	}
	params := DeliveryPrepareParams{1, frozen.Ref}
	sha, err := canonical.Hash(params)
	if err != nil {
		t.Fatal(err)
	}
	paramsRef := Ref{uuid.New(), sha}
	closure, err := CompileDeliveryPrepareClosure(uuid.New(), params, paramsRef, required, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if len(closure.Direct) != 2 || closure.Direct[0] != paramsRef || closure.Direct[1] != frozen.Ref {
		t.Fatal("unbounded prepare direct roots")
	}
	cursor, err := closure.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for {
		_, ok, err := cursor.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		count++
	}
	if count != len(resolved)+1 {
		t.Fatal("nested prepare inputs lost", count)
	}
}
