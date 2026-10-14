package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestTargetPreparationManifestOwnsWholeSnapshotAndDependencyGraph(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	binding := func() v2input.SessionVersionBinding {
		return v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("b", 64)}
	}
	source, target := binding(), binding()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	intent := v2input.SemanticEditIntent{SchemaVersion: 1, Kind: "remove_object", Reference: source, ReferenceMask: ref(), TargetSnapshotID: uuid.New(), Strength: .5, PreservePersons: true}
	raw, _ := canonical.Marshal(intent)
	request, err := CompileRequest(uuid.New(), who.UserID, "target-manifest", raw)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, body, err := v2input.CompilePinnedSessionInputSnapshot(intent.TargetSnapshotID, []v2input.SessionSnapshotItem{{Ordinal: 0, Binding: target, WorkingImage: target.Source, Recipe: ref()}, {Ordinal: 1, Binding: source, WorkingImage: source.Source, Recipe: ref()}})
	if err != nil {
		t.Fatal(err)
	}
	snapshotRef := ref()
	snapshotRef.SHA256, _ = canonical.Hash(snapshot)
	intentRef := ref()
	intentRef.SHA256, _ = canonical.Hash(intent)
	published := PublishedIntent{Ref: intentRef, Inventory: CatalogInventory{Scope: CatalogScope{ProjectID: uuid.New(), ItemCount: 2}, Targets: []v2input.SessionVersionBinding{target, source}}}
	compile := func(r Request, p PublishedIntent) (nativeTargetPreparationManifest, error) {
		return compileNativeTargetPreparationManifest(context.Background(), who, r, "target-manifest", p, snapshotRef, body)
	}
	manifest, err := compile(request, published)
	if err != nil {
		t.Fatal(err)
	}
	needed := &nativeTargetPreparationRequired{manifest: manifest}
	var continuation *nativeTargetPreparationRequired
	if !errors.As(needed, &continuation) || continuation.manifest.sha != manifest.sha || !errors.Is(needed, errTransferProtectionUnavailable) {
		t.Fatal("private continuation lost dependency-unavailable classification")
	}
	var decoded nativeTargetPreparationManifestBody
	if err := json.Unmarshal(manifest.body, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Entries) != 2 || decoded.Entries[0].Binding != target || decoded.Entries[1].Binding != source || decoded.Entries[0].ReuseReference || !decoded.Entries[1].ReuseReference || len(decoded.Entries[1].Stages) != 0 {
		t.Fatal("snapshot order/reference reuse lost")
	}
	stages := decoded.Entries[0].Stages
	if len(stages) != 6 {
		t.Fatal("incomplete target prerequisites")
	}
	seen := map[uuid.UUID]bool{}
	for _, s := range stages {
		if s.ID == uuid.Nil || seen[s.ID] || s.OperationVersion != "1" {
			t.Fatal("aliased stage identity")
		}
		for _, dependency := range s.Requires {
			if !seen[dependency] {
				t.Fatal("unknown or forward dependency")
			}
		}
		seen[s.ID] = true
	}
	if len(stages[0].Requires) != 0 || len(stages[1].Requires) != 0 || len(stages[5].Requires) != 3 {
		t.Fatal("person/face/defect prerequisite separation lost")
	}
	again, err := compile(request, published)
	if err != nil || string(again.body) != string(manifest.body) || again.sha != manifest.sha {
		t.Fatal("unstable replay", err)
	}
	if err := verifyNativeTargetPreparationManifest(context.Background(), who, request, "target-manifest", published, snapshotRef, body, manifest); err != nil {
		t.Fatal(err)
	}

	targetAnchor, err := compileNativeTargetAnchor(context.Background(), who, request, "target-manifest", published, snapshotRef, body, manifest, 0, "mask.person")
	if err != nil || targetAnchor.request.Intent.Reference != source || targetAnchor.entry.Binding != target || targetAnchor.stage.Operation != "mask.person" {
		t.Fatal("target anchor replaced original reference", err)
	}
	if err := verifyNativeTargetAnchor(context.Background(), targetAnchor); err != nil {
		t.Fatal(err)
	}
	for _, ordinal := range []int{-1, 1, 2} {
		if got, err := compileNativeTargetAnchor(context.Background(), who, request, "target-manifest", published, snapshotRef, body, manifest, ordinal, "mask.person"); err == nil || got.seal != "" {
			t.Fatal("reference reuse/out of range anchor accepted", ordinal, err)
		}
	}
	if got, err := compileNativeTargetAnchor(context.Background(), who, request, "target-manifest", published, snapshotRef, body, manifest, 0, "semantic.transfer"); err == nil || got.seal != "" {
		t.Fatal("unknown prerequisite accepted", err)
	}
	changedAnchor := targetAnchor
	changedAnchor.entry.Namespace = uuid.New()
	if err := verifyNativeTargetAnchor(context.Background(), changedAnchor); err == nil {
		t.Fatal("changed target anchor accepted")
	}
	restored, err := restoreNativeTargetPreparationManifest(context.Background(), who, request, "target-manifest", published, snapshotRef, body, manifest.body, manifest.sha)
	if err != nil || restored.sha != manifest.sha || string(restored.body) != string(manifest.body) {
		t.Fatal("saved plan did not restore", err)
	}
	restored.body[0] = '['
	if manifest.body[0] != '{' {
		t.Fatal("restored plan aliases persisted input")
	}
	// Rehashing modified bytes cannot turn a substituted scope/DAG into authority.
	mutations := map[string]func(*nativeTargetPreparationManifestBody){
		"tenant":            func(m *nativeTargetPreparationManifestBody) { m.Tenant = uuid.New() },
		"project":           func(m *nativeTargetPreparationManifestBody) { m.Project = uuid.New() },
		"actor":             func(m *nativeTargetPreparationManifestBody) { m.Actor = uuid.New() },
		"session":           func(m *nativeTargetPreparationManifestBody) { m.Session = uuid.New() },
		"parent":            func(m *nativeTargetPreparationManifestBody) { m.Parent = uuid.New() },
		"request":           func(m *nativeTargetPreparationManifestBody) { m.RequestSHA = strings.Repeat("f", 64) },
		"intent":            func(m *nativeTargetPreparationManifestBody) { m.Intent.ArtifactID = uuid.New() },
		"snapshot":          func(m *nativeTargetPreparationManifestBody) { m.Snapshot.ArtifactID = uuid.New() },
		"namespace":         func(m *nativeTargetPreparationManifestBody) { m.Entries[0].Namespace = uuid.New() },
		"dependency":        func(m *nativeTargetPreparationManifestBody) { m.Entries[0].Stages[5].Requires[0] = uuid.New() },
		"operation_version": func(m *nativeTargetPreparationManifestBody) { m.Entries[0].Stages[0].OperationVersion = "2" },
		"reuse":             func(m *nativeTargetPreparationManifestBody) { m.Entries[0].ReuseReference = true },
		"truncation":        func(m *nativeTargetPreparationManifestBody) { m.Entries = m.Entries[:1] },
	}
	for name, mutate := range mutations {
		t.Run("restore_rejects_"+name, func(t *testing.T) {
			var changed nativeTargetPreparationManifestBody
			if err := json.Unmarshal(manifest.body, &changed); err != nil {
				t.Fatal(err)
			}
			mutate(&changed)
			raw, err := canonical.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(raw)
			got, err := restoreNativeTargetPreparationManifest(context.Background(), who, request, "target-manifest", published, snapshotRef, body, raw, hex.EncodeToString(digest[:]))
			if !errors.Is(err, v2input.ErrConflict) || got.body != nil || got.sha != "" {
				t.Fatal("substitution exposed restored work", err)
			}
		})
	}
	for _, invalid := range []json.RawMessage{nil, []byte("{}"), append(append(json.RawMessage(nil), manifest.body...), '\n'), make(json.RawMessage, (8<<20)+1)} {
		digest := sha256.Sum256(invalid)
		got, err := restoreNativeTargetPreparationManifest(context.Background(), who, request, "target-manifest", published, snapshotRef, body, invalid, hex.EncodeToString(digest[:]))
		if !errors.Is(err, v2input.ErrConflict) || got.body != nil {
			t.Fatal("noncanonical/oversized restore accepted", err)
		}
	}
	tampered := manifest
	tampered.body = append(json.RawMessage(nil), manifest.body...)
	tampered.body[0] = '['
	if err := verifyNativeTargetPreparationManifest(context.Background(), who, request, "target-manifest", published, snapshotRef, body, tampered); !errors.Is(err, v2input.ErrConflict) {
		t.Fatal("substituted manifest accepted", err)
	}
	changed := published
	changed.Inventory.Targets = append([]v2input.SessionVersionBinding(nil), published.Inventory.Targets...)
	changed.Inventory.Targets[0] = source
	if _, err := compile(request, changed); err == nil {
		t.Fatal("substituted/reordered target accepted")
	}
	changed = published
	changed.Ref.SHA256 = strings.Repeat("c", 64)
	if _, err := compile(request, changed); err == nil {
		t.Fatal("foreign intent accepted")
	}
	got, err := restoreNativeTargetPreparationManifest(context.Background(), who, request, "target-manifest", published, snapshotRef, body, manifest.body, strings.Repeat("f", 64))
	if !errors.Is(err, v2input.ErrConflict) || got.body != nil {
		t.Fatal("wrong saved checksum accepted", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := restoreNativeTargetPreparationManifest(canceled, who, request, "target-manifest", published, snapshotRef, body, manifest.body, manifest.sha); !errors.Is(err, context.Canceled) || got.body != nil || got.sha != "" {
		t.Fatal("canceled restore exposed work", err)
	}
	if got, err := compileNativeTargetPreparationManifest(canceled, who, request, "target-manifest", published, snapshotRef, body); !errors.Is(err, context.Canceled) || got.body != nil {
		t.Fatal("canceled planner exposed work", err)
	}
}

func TestTargetPreparationManifestCovers5000TargetsWithoutIdentityAliases(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	namespace := uuid.New()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	source := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("b", 64)}
	intent := v2input.SemanticEditIntent{SchemaVersion: 1, Kind: "remove_object", Reference: source, ReferenceMask: ref(), TargetSnapshotID: uuid.New(), Strength: .5, PreservePersons: true}
	raw, _ := canonical.Marshal(intent)
	request, err := CompileRequest(uuid.New(), who.UserID, "target-limit", raw)
	if err != nil {
		t.Fatal(err)
	}
	items := make([]v2input.SessionSnapshotItem, 5000)
	targets := make([]v2input.SessionVersionBinding, 5000)
	recipe := ref()
	for i := range items {
		name := fmt.Sprintf("%d", i)
		targets[i] = v2input.SessionVersionBinding{AssetVersionID: uuid.NewSHA1(namespace, []byte("version:"+name)), Source: v2input.Ref{ArtifactID: uuid.NewSHA1(namespace, []byte("source:"+name)), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}
		items[i] = v2input.SessionSnapshotItem{Ordinal: i, Binding: targets[i], WorkingImage: targets[i].Source, Recipe: recipe}
	}
	snapshot, body, err := v2input.CompilePinnedSessionInputSnapshot(intent.TargetSnapshotID, items)
	if err != nil {
		t.Fatal(err)
	}
	snapshotRef := ref()
	snapshotRef.SHA256, _ = canonical.Hash(snapshot)
	intentRef := ref()
	intentRef.SHA256, _ = canonical.Hash(intent)
	published := PublishedIntent{Ref: intentRef, Inventory: CatalogInventory{Scope: CatalogScope{ProjectID: uuid.New(), ItemCount: 5000}, Targets: targets}}
	manifest, err := compileNativeTargetPreparationManifest(context.Background(), who, request, "target-limit", published, snapshotRef, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.body) > 8<<20 {
		t.Fatal("manifest exceeded metadata bound")
	}
	var decoded nativeTargetPreparationManifestBody
	if err := json.Unmarshal(manifest.body, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Entries) != 5000 {
		t.Fatal("target batch truncated")
	}
	ids := map[uuid.UUID]bool{}
	for i, entry := range decoded.Entries {
		if entry.Ordinal != i || entry.Binding != targets[i] || len(entry.Stages) != 6 || ids[entry.Namespace] {
			t.Fatal("batch coverage/namespace conflict", i)
		}
		ids[entry.Namespace] = true
		for _, stage := range entry.Stages {
			if ids[stage.ID] {
				t.Fatal("cross-target stage identity alias", i)
			}
			ids[stage.ID] = true
		}
	}
}
