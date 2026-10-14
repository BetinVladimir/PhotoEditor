package delivery

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func inspectionFixture(t *testing.T, id uuid.UUID) ([]byte, Manifest) {
	t.Helper()
	m := Manifest{SchemaVersion: 1, AssemblySHA: strings.Repeat("a", 64), SnapshotDigest: strings.Repeat("b", 64), SlotCount: 2, ReadyCount: 1, OmittedCount: 1, Shards: []v2input.Ref{{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}}}
	body, err := canonical.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"package_id": id, "revision": "2", "manifest": v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(body)}, "expected_manifest": m})
	if err != nil {
		t.Fatal(err)
	}
	return raw, m
}
func TestInspectionCanonicalManifestBinding(t *testing.T) {
	id := uuid.New()
	raw, _ := inspectionFixture(t, id)
	value, err := ValidateInspection(raw, id, "2")
	if err != nil || value.SlotCount != 2 || value.ReadyCount != 1 || value.OmittedCount != 1 {
		t.Fatal(value, err)
	}
	for _, key := range []string{"revision", "package_id", "manifest", "expected_manifest"} {
		var wire map[string]any
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatal(err)
		}
		switch key {
		case "revision":
			wire[key] = "3"
		case "package_id":
			wire[key] = uuid.New().String()
		case "manifest":
			wire[key] = v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("f", 64)}
		case "expected_manifest":
			wire[key].(map[string]any)["ready_count"] = 2
		}
		changed, _ := json.Marshal(wire)
		if _, err := ValidateInspection(changed, id, "2"); err == nil {
			t.Fatal("corruption accepted", key)
		}
	}
}
func TestInspectionReadScopeAndPreflight(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tid, id := uuid.New(), uuid.New()
	raw, _ := inspectionFixture(t, id)
	tx := &deliveryReadTx{raw: raw}
	if _, err := ReadInspection(context.Background(), tx, who, tid, id, "2"); err != nil {
		t.Fatal(err)
	}
	if len(tx.args) != 5 || tx.args[0] != tid || tx.args[1] != id || tx.args[2] != who.UserID || tx.args[3] != who.SessionID || tx.args[4] != int64(2) {
		t.Fatal(tx.args)
	}
	tx.calls = 0
	if _, err := ReadInspection(context.Background(), tx, who, tid, id, "02"); err == nil || tx.calls != 0 {
		t.Fatal("invalid CAS reached SQL")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadInspection(ctx, tx, who, tid, id, "2"); err == nil || tx.calls != 0 {
		t.Fatal("cancelled query reached SQL")
	}
	tx.raw = []byte("null")
	if _, err := ReadInspection(context.Background(), tx, who, tid, id, "2"); err == nil {
		t.Fatal("stale accepted")
	}
}

func TestInspectionPublicBoundary(t *testing.T) {
	id := uuid.New()
	raw, _ := inspectionFixture(t, id)
	valid, err := ValidateInspection(raw, id, "2")
	if err != nil {
		t.Fatal(err)
	}
	if !ValidInspectionResource(valid, id, "2") {
		t.Fatal("valid inspection rejected")
	}
	for _, mutate := range []func(*Inspection){func(v *Inspection) { v.PackageID = uuid.New() }, func(v *Inspection) { v.Revision = "3" }, func(v *Inspection) { v.Manifest.ArtifactID = uuid.Nil }, func(v *Inspection) { v.Manifest.SHA256 = "invalid" }, func(v *Inspection) { v.ReadyCount = 0 }, func(v *Inspection) { v.OmittedCount = 2 }, func(v *Inspection) { v.SlotCount = 80001 }} {
		changed := valid
		mutate(&changed)
		if ValidInspectionResource(changed, id, "2") {
			t.Fatal("invalid inspection accepted", changed)
		}
	}
	if _, err := ValidateInspection([]byte(strings.Repeat(" ", (1<<20)+1)), id, "2"); err == nil {
		t.Fatal("oversized inspection accepted")
	}
}

func TestInspectionRejectsAmbiguousNestedJSON(t *testing.T) {
	id := uuid.New()
	raw, _ := inspectionFixture(t, id)
	base := string(raw)
	if _, err := ValidateInspection(raw, id, "2"); err != nil {
		t.Fatal("valid inspection rejected", err)
	}
	shardKey := `"artifact_id":`
	shardsAt := strings.Index(base, `"shards":[`)
	lastShard := -1
	if shardsAt >= 0 {
		if offset := strings.Index(base[shardsAt:], shardKey); offset >= 0 {
			lastShard = shardsAt + offset
		}
	}
	if lastShard < 0 {
		t.Fatal("shard reference missing")
	}
	shardDuplicate := base[:lastShard] + `"artifact_id":"` + uuid.NewString() + `",` + base[lastShard:]
	for _, altered := range []string{
		strings.Replace(base, `"revision":"2"`, `"revision":"2","revision":"2"`, 1),
		strings.Replace(base, `"manifest":{`, `"manifest":{"sha256":"`+strings.Repeat("a", 64)+`",`, 1),
		strings.Replace(base, `"expected_manifest":{`, `"expected_manifest":{"schema_version":1,`, 1),
		shardDuplicate,
		strings.Replace(base, `"expected_manifest":{`, `"expected_manifest":{"private_key":"secret",`, 1),
	} {
		if altered == base {
			t.Fatal("fixture did not contain target key")
		}
		if _, err := ValidateInspection([]byte(altered), id, "2"); err == nil {
			t.Fatal("ambiguous inspection accepted", altered)
		}
	}
}
