package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func TestCatalogPersonTaskRetainsExactObjects(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err := json.Unmarshal(raw, &images); err != nil {
		t.Fatal(err)
	}
	for _, body := range images {
		image, err := review.ParseWorkingImage(body)
		if err != nil {
			t.Fatal(err)
		}
		canvas, err := canonical.Hash(image.Canvas)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, CanvasHash: canvas}
		prepared, err := v2input.PrepareSemanticPersonParams(context.Background(), binding, body, strings.Repeat("a", 64), 512)
		if err != nil {
			t.Fatal(err)
		}
		sum = sha256.Sum256(prepared.Params)
		params := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
		who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
		tenant, workflow := uuid.New(), uuid.New()
		for _, mode := range []string{"valid", "revoke", "mutate", "size"} {
			catalog := &detectorCatalogFixture{sizes: map[uuid.UUID]int64{params.ArtifactID: int64(len(prepared.Params))}, calls: map[uuid.UUID]int{}, mode: mode, who: who, tenant: tenant}
			for _, entry := range prepared.Inputs {
				catalog.sizes[entry.Ref.ArtifactID] = 1024
			}
			catalog.sizes[binding.Source.ArtifactID] = int64(len(body))
			got, err := prepareCatalogPersonTask(context.Background(), catalog, who, tenant, workflow, 0, binding, body, params, strings.Repeat("a", 64), 512, v2input.SemanticQualifiedCosts{VRAMBytes: 1 << 30}, 1<<40, 12<<30, 1<<40, 1<<30)
			if mode != "valid" {
				if err == nil {
					t.Fatal("changed catalog accepted", mode)
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			var evidence []v2draft.InputEvidence
			if json.Unmarshal([]byte(got.inputs), &evidence) != nil || len(evidence) != len(prepared.Inputs)+1 {
				t.Fatal("catalog inventory lost")
			}
			if _, err := decodePersonInputEvidence(context.Background(), got.frozen, got.inputs); err != nil {
				t.Fatal(err)
			}
			for _, document := range []uuid.UUID{uuid.New(), uuid.Nil} {
				pin, err := marshalPersonParamsPin(context.Background(), "bucket", sourcePersonPreparation{document: document, catalog: got})
				if document == uuid.Nil {
					if err == nil || pin != nil {
						t.Fatal("missing Params document accepted")
					}
					continue
				}
				if err != nil || len(pin) == 0 {
					t.Fatal("exact Params pin rejected", err)
				}
			}
			pins := []sourcePreparationPin{}
			for _, entry := range evidence {
				if entry.Contract == "Params" {
					continue
				}
				node := v2draft.LayerRef{Ref: entry.Ref, Contract: entry.Contract}
				if entry.Ref == binding.Source {
					node.CanvasHash = binding.CanvasHash
				} else {
					node.ParentRef = binding.Source
				}
				pins = append(pins, sourcePreparationPin{Node: node, Object: entry.Object})
			}
			rank := func(contract string) int {
				switch contract {
				case "WorkingImage":
					return 0
				case "WorkingPixels":
					return 1
				case "WorkingAlpha":
					return 2
				default:
					return 3
				}
			}
			sort.SliceStable(pins, func(i, j int) bool { return rank(pins[i].Node.Contract) < rank(pins[j].Node.Contract) })
			pinBody, err := canonical.Marshal(pins)
			if err != nil {
				t.Fatal(err)
			}
			proof := sourcePreparationProof{binding: binding, pins: string(pinBody)}
			if err := verifyPersonSourceInputPins(context.Background(), got, proof); err != nil {
				t.Fatal("exact source join", err)
			}
			assertPersonDraftPlan(t, catalog, who, tenant, binding, body, params, proof)
			for _, kind := range []string{"version", "key", "size", "missing"} {
				bad := append([]sourcePreparationPin{}, pins...)
				switch kind {
				case "version":
					bad[0].Object.VersionID = "different"
				case "key":
					bad[0].Object.Key += "-other"
				case "size":
					bad[0].Object.Bytes++
				case "missing":
					bad = bad[:len(bad)-1]
				}
				encoded, err := canonical.Marshal(bad)
				if err != nil {
					t.Fatal(err)
				}
				modified := proof
				modified.pins = string(encoded)
				if err := verifyPersonSourceInputPins(context.Background(), got, modified); err == nil {
					t.Fatal("source object substitution accepted", kind)
				}
			}
			for _, kind := range []string{"missing", "duplicate", "version", "hash", "size", "contract"} {
				changed := append([]v2draft.InputEvidence{}, evidence...)
				switch kind {
				case "missing":
					changed = changed[:len(changed)-1]
				case "duplicate":
					changed[0] = changed[1]
				case "version":
					changed[0].Object.VersionID = "null"
				case "hash":
					changed[0].Object.SHA256 = strings.Repeat("f", 64)
				case "size":
					changed[0].Object.Bytes = 0
				case "contract":
					changed[0].Contract = "FaceBoxes"
				}
				encoded, err := canonical.Marshal(changed)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := decodePersonInputEvidence(context.Background(), got.frozen, string(encoded)); err == nil {
					t.Fatal("changed retained person evidence accepted", kind)
				}
			}
			if _, err := decodePersonInputEvidence(context.Background(), got.frozen, got.inputs+" "); err == nil {
				t.Fatal("noncanonical retained evidence accepted")
			}
			for _, entry := range evidence {
				if entry.Object.Bytes != catalog.sizes[entry.Ref.ArtifactID] || entry.Object.VersionID != "v1" || entry.Object.SHA256 != entry.Ref.SHA256 {
					t.Fatal("catalog pins changed")
				}
			}
			for _, count := range catalog.calls {
				if count != 2 {
					t.Fatal("catalog authority not rechecked")
				}
			}
		}
	}
}
