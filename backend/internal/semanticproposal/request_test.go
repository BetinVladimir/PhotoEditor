package semanticproposal

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestCompileRequestOwnsAndBindsImmutableIntent(t *testing.T) {
	tenant, actor := uuid.New(), uuid.New()
	zero := 0.0
	pin := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	intent := v2input.SemanticEditIntent{SchemaVersion: 1, Kind: "local_exposure", Reference: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: pin, CanvasHash: strings.Repeat("b", 64)}, ReferenceMask: pin, TargetSnapshotID: uuid.New(), Strength: .5, EV: &zero, PreservePersons: true}
	raw, _ := canonical.Marshal(intent)
	first, err := CompileRequest(tenant, actor, "stable-key", raw)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := CompileRequest(tenant, actor, "stable-key", raw)
	if err != nil || replay.RunID != first.RunID || replay.RequestSHA != first.RequestSHA {
		t.Fatal("unstable identity", err)
	}
	intent.Strength = .8
	changed, _ := canonical.Marshal(intent)
	other, err := CompileRequest(tenant, actor, "stable-key", changed)
	if err != nil || other.RunID != first.RunID || other.RequestSHA == first.RequestSHA {
		t.Fatal("changed request not fenced", err)
	}
	for i := range raw {
		raw[i] = 0
	}
	if first.Body[0] != '{' || *first.Intent.EV != 0 {
		t.Fatal("input alias")
	}
	foreign, err := CompileRequest(uuid.New(), actor, "stable-key", changed)
	if err != nil || foreign.RunID == other.RunID || foreign.RequestSHA == other.RequestSHA {
		t.Fatal("tenant not bound", err)
	}
}
func TestCompileRequestRejectsInvalidScopeAndIntent(t *testing.T) {
	if _, err := CompileRequest(uuid.Nil, uuid.New(), "key", []byte(`{}`)); err == nil {
		t.Fatal("invalid scope")
	}
	if _, err := CompileRequest(uuid.New(), uuid.New(), "key", []byte(`{"kind":"remove_object"}`)); err == nil {
		t.Fatal("incomplete intent")
	}
}

func TestBindTargetsRequiresExactSnapshotPinAndIdentity(t *testing.T) {
	pin := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: pin, CanvasHash: strings.Repeat("b", 64)}
	snapshotID := uuid.New()
	_, body, err := v2input.CompilePinnedSessionInputSnapshot(snapshotID, []v2input.SessionSnapshotItem{{Ordinal: 0, Binding: binding, WorkingImage: pin, Recipe: pin}})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	request := Request{Intent: v2input.SemanticEditIntent{SchemaVersion: 1, Kind: "remove_object", Reference: binding, ReferenceMask: pin, TargetSnapshotID: snapshotID, Strength: .5, PreservePersons: true}}
	targets, err := BindTargets(request, ref, body)
	if err != nil || len(targets) != 1 || targets[0] != binding {
		t.Fatal("binding", err)
	}
	bad := ref
	bad.SHA256 = strings.Repeat("f", 64)
	if _, err := BindTargets(request, bad, body); err == nil {
		t.Fatal("wrong content pin")
	}
	request.Intent.TargetSnapshotID = uuid.New()
	if _, err := BindTargets(request, ref, body); err == nil {
		t.Fatal("wrong snapshot identity")
	}
}
