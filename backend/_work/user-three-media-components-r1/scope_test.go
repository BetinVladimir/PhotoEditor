package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T) scope {
	t.Helper()
	r, e := os.ReadFile(scopePath)
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(r)
	s, e := parseScope(r, hex.EncodeToString(h[:]))
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func row(v map[string]string) map[string]json.RawMessage {
	r := map[string]json.RawMessage{}
	for k, v := range v {
		r[k], _ = json.Marshal(v)
	}
	return r
}
func TestExactDeterministicScopeAndForeignSelection(t *testing.T) {
	s := fixture(t)
	if len(s.tasks) != 12 {
		t.Fatal("cardinality")
	}
	if s.selected(uuid.New(), "media", []byte(`{}`)) == nil {
		t.Fatal("foreign selected")
	}
	for id := range s.tasks {
		if s.selected(uuid.MustParse(id), "media", []byte(`{}`)) != nil {
			t.Fatal("own refused")
		}
		if s.selected(uuid.MustParse(id), "cpu", []byte(`{}`)) == nil {
			t.Fatal("wrong class")
		}
		break
	}
}
func TestOwnBindingRequiresTenantRunOperationAndClass(t *testing.T) {
	s := fixture(t)
	for id, b := range s.tasks {
		v := map[string]string{"id": id, "tenant_id": b.Tenant, "run_id": b.Run, "operation": "media.thumbnail", "resource_class": "media"}
		if !s.own("tasks", row(v)) {
			t.Fatal("valid own")
		}
		for _, k := range []string{"tenant_id", "run_id", "operation", "resource_class"} {
			old := v[k]
			v[k] = "foreign"
			if s.own("tasks", row(v)) {
				t.Fatal("foreign field", k)
			}
			v[k] = old
		}
		if s.own("unknown_table", row(v)) {
			t.Fatal("unknown permitted")
		}
		break
	}
}
func TestSnapshotDeltaAndMembershipFailClosed(t *testing.T) {
	a := snapshot{"tasks": "abc"}
	if !equalSnapshots(a, snapshot{"tasks": "abc"}) {
		t.Fatal("equal")
	}
	for _, b := range []snapshot{{"tasks": "changed"}, {"tasks": "abc", "new_table": "x"}, {}} {
		if equalSnapshots(a, b) {
			t.Fatal("delta missed")
		}
	}
}

func TestActualNormalizationProtectsForeignFairnessAndOwnAuthority(t *testing.T) {
	s := fixture(t)
	for _, tenant := range []string{s.Jobs[0].Tenant, "foreign"} {
		r := row(map[string]string{"tenant_id": tenant, "virtual_finish": "10", "preview_streak": "1", "last_dispatch_at": "old", "weight": "authority"})
		skip, e := s.normalize("scheduler_tenants", r)
		if e != nil || skip {
			t.Fatal("fairness row not protected")
		}
		if text(r, "weight") != "authority" {
			t.Fatal("authority removed")
		}
		_, present := r["virtual_finish"]
		if present == (tenant == s.Jobs[0].Tenant) {
			t.Fatal("wrong tenant counter normalization")
		}
	}
}
func TestActualNormalizationRefusesForeignOwnNodeLeaseAndPreservesOtherNode(t *testing.T) {
	s := fixture(t)
	r := row(map[string]string{"node_id": "rtx-beeloyai-media-eabf3c5add7d", "task_id": uuid.NewString(), "tenant_id": s.Jobs[0].Tenant})
	if _, e := s.normalize("resource_slots", r); e == nil {
		t.Fatal("foreign lease allowed")
	}
	r = row(map[string]string{"node_id": "foreign-node", "task_id": "foreign-task", "capacity": "authority"})
	if _, e := s.normalize("resource_slots", r); e != nil || text(r, "task_id") != "foreign-task" || text(r, "capacity") != "authority" {
		t.Fatal("foreign node changed")
	}
}
func TestActualReceiptBytesDriftAndSymlinkRefused(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := root + "/receipt.json"
	raw := []byte(`{"actual":"fixture-only"}`)
	if e := os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(raw)
	pin := hex.EncodeToString(h[:])
	if e := receipt(path, pin, root); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte(`{"drift":true}`), 0600); e != nil {
		t.Fatal(e)
	}
	if receipt(path, pin, root) == nil {
		t.Fatal("drift allowed")
	}
	link := root + "/link"
	if e := os.Symlink(path, link); e != nil {
		t.Fatal(e)
	}
	if receipt(link, pin, root) == nil {
		t.Fatal("symlink allowed")
	}
}
