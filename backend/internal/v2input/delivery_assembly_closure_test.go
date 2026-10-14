package v2input

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestDeliveryAssemblyClosurePinsRequiredRootsAndParams(t *testing.T) {
	entry := func(contract string, n int) Entry {
		return Entry{Ref: Ref{ArtifactID: uuid.New(), SHA256: fmt.Sprintf("%064x", n)}, Contract: contract}
	}
	root, shard, config := entry("DeliveryAssemblyInput", 1), entry("DeliverySlotShard", 2), entry("ExportPresetConfig", 3)
	roots := []Entry{root, shard, config}
	params := DeliveryAssembleParams{SchemaVersion: 1, Assembly: root.Ref}
	sha, err := canonical.Hash(params)
	if err != nil {
		t.Fatal(err)
	}
	paramsRef := Ref{ArtifactID: uuid.New(), SHA256: sha}
	workflow := uuid.New()
	closure, err := CompileDeliveryAssemblyClosure(workflow, params, paramsRef, roots, roots)
	if err != nil {
		t.Fatal(err)
	}
	again, err := CompileDeliveryAssemblyClosure(workflow, params, paramsRef, roots, []Entry{config, root, shard})
	if err != nil || again.InputDigest != closure.InputDigest {
		t.Fatal("unstable closure", err)
	}
	for _, scenario := range []string{"missing", "contract", "alias", "params", "root", "duplicate_root"} {
		t.Run(scenario, func(t *testing.T) {
			required := append([]Entry(nil), roots...)
			resolved := append([]Entry(nil), roots...)
			current := params
			ref := paramsRef
			switch scenario {
			case "missing":
				resolved = resolved[:2]
			case "contract":
				resolved[1].Contract = "WorkingImage"
			case "alias":
				resolved = append(resolved, Entry{Ref: shard.Ref, Contract: "Params"})
			case "params":
				ref.SHA256 = fmt.Sprintf("%064x", 99)
			case "root":
				current.Assembly = shard.Ref
			case "duplicate_root":
				required = append(required, root)
			}
			if _, err := CompileDeliveryAssemblyClosure(workflow, current, ref, required, resolved); err == nil {
				t.Fatal("invalid assembly closure accepted", scenario)
			}
		})
	}
	roots[0] = Entry{}
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
		t.Fatal("borrowed or incomplete closure", count)
	}
}

func TestAssemblyLargeInventoryUsesBoundedDirectRoots(t *testing.T) {
	ref := func(n int) Ref { return Ref{ArtifactID: uuid.New(), SHA256: fmt.Sprintf("%064x", n)} }
	root, config := Entry{Ref: ref(1), Contract: "DeliveryAssemblyInput"}, Entry{Ref: ref(2), Contract: "ExportPresetConfig"}
	required := []Entry{root, config}
	for n := 0; n < 160; n++ {
		required = append(required, Entry{Ref: ref(n + 3), Contract: "DeliverySlotShard"})
	}
	resolved := append([]Entry(nil), required...)
	for n := 0; n < 1000; n++ {
		resolved = append(resolved, Entry{Ref: ref(n + 200), Contract: "EncodedExport"})
	}
	params := DeliveryAssembleParams{SchemaVersion: 1, Assembly: root.Ref}
	sha, err := canonical.Hash(params)
	if err != nil {
		t.Fatal(err)
	}
	paramsRef := Ref{ArtifactID: uuid.New(), SHA256: sha}
	closure, err := CompileDeliveryAssemblyClosure(uuid.New(), params, paramsRef, required, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if len(closure.Direct) != 2 || closure.Direct[0] != paramsRef || closure.Direct[1] != root.Ref {
		t.Fatal("unbounded direct transport", len(closure.Direct))
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
		t.Fatal("indexed dependencies dropped", count)
	}
}
