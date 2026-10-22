package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/canonical"
	"reflect"
	"strings"
	"testing"
	"time"
)

type row struct {
	values []any
	err    error
}

func (r row) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("fixture row width")
	}
	for i, v := range r.values {
		if v == nil {
			reflect.ValueOf(dest[i]).Elem().SetZero()
		} else {
			reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(v))
		}
	}
	return nil
}

type rows struct {
	pgx.Rows
	values [][]any
	i      int
}

func (r *rows) Next() bool          { return r.i < len(r.values) }
func (r *rows) Scan(d ...any) error { e := (row{values: r.values[r.i]}).Scan(d...); r.i++; return e }
func (r *rows) Close()              {}
func (r *rows) Err() error          { return nil }

type claimTX struct {
	frozen json.RawMessage
	pgx.Tx
	s                             scope
	cancelled                     int
	noLease                       bool
	claimErr, commitErr, queryErr error
	inputMismatch                 bool
	committed, rolled             bool
	queries                       int
}

func (x *claimTX) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	if strings.Contains(q, "cancel_unsupported_apply_plans") {
		return row{values: []any{x.cancelled}}
	}
	if strings.Contains(q, "claim_apply_build_versioned") {
		if x.claimErr != nil {
			return row{err: x.claimErr}
		}
		if x.noLease {
			return row{err: pgx.ErrNoRows}
		}
		return row{values: []any{x.s.Tenant, x.s.Run, &x.s.Collection, &x.s.Style, x.s.Phase, 1, uuid.New(), time.Now(), x.s.Graph, (*int)(nil)}}
	}
	if strings.Contains(q, "photo.apply_v4_frozen_plan") {
		raw := x.frozen
		if len(raw) == 0 {
			raw = fixedFrozen()
		}
		return row{values: []any{raw}}
	}
	if strings.Contains(q, "SELECT bucket,object_key,object_version,media_type") {
		return row{values: []any{"photos", "style", "version1", "application/json"}}
	}
	return row{err: errors.New("unexpected queryrow")}
}
func (x *claimTX) Query(_ context.Context, q string, args ...any) (pgx.Rows, error) {
	x.queries++
	if x.queryErr != nil {
		return nil, x.queryErr
	}
	if !strings.Contains(q, "photo.apply_inputs") && !strings.Contains(q, "photo.apply_v4_inputs_with_metadata") {
		return nil, errors.New("unexpected query")
	}
	s := x.s
	item := s.Item
	if x.inputMismatch {
		item = uuid.New()
	}
	styleID := uuid.New()
	size := int64(8)
	values := []any{0, item, s.Asset, s.Version, s.Master, s.MasterSHA, int64(2), "photos", "master", "version1", "application/json", json.RawMessage(`{}`), json.RawMessage(`{"style_config": {"mode": "fixed"}}`), &styleID, &s.StyleSHA, &size}
	if x.s.Graph == 4 {
		values = append(values, json.RawMessage(`{}`))
	}
	return &rows{values: [][]any{values}}, nil
}
func (x *claimTX) Commit(context.Context) error {
	if x.commitErr != nil {
		return x.commitErr
	}
	x.committed = true
	return nil
}
func (x *claimTX) Rollback(context.Context) error {
	if x.committed {
		return pgx.ErrTxClosed
	}
	x.rolled = true
	return nil
}
func validScope() scope {
	h := strings.Repeat("a", 64)
	raw, _ := canonical.Bytes([]byte(`{"style_config": {"mode": "fixed"}}`))
	sha := sha256.Sum256(raw)
	return scope{Schema: 1, ObservedAt: time.Now().UTC(), Head: 2649, Tenant: uuid.New(), Project: project, Collection: collection, Item: uuid.New(), Asset: uuid.New(), Version: uuid.New(), Master: uuid.New(), MasterSHA: h, Style: uuid.New(), StyleSHA: h, Run: uuid.New(), ConfigSHA: hex.EncodeToString(sha[:]), Phase: "color", Graph: 3, CPUReceiptSHA: h, HumanReviewSHA: h, APIReceiptSHA: h, SourceManifestSHA: h}
}
func TestProductionClaimRollsBackOutsideScopeBeforeAnyCompile(t *testing.T) {
	for _, name := range []string{"foreign-run", "foreign-tenant", "foreign-style", "foreign-collection", "phase", "unsupported-housekeeping", "nil-lease", "claim-error", "input-mismatch", "input-error", "commit-error"} {
		t.Run(name, func(t *testing.T) {
			s := validScope()
			x := &claimTX{s: s}
			switch name {
			case "foreign-run":
				x.s.Run = uuid.New()
			case "foreign-tenant":
				x.s.Tenant = uuid.New()
			case "foreign-style":
				x.s.Style = uuid.New()
			case "foreign-collection":
				x.s.Collection = uuid.New()
			case "phase":
				x.s.Phase = "quality"
			case "unsupported-housekeeping":
				x.cancelled = 1
			case "nil-lease":
				x.noLease = true
			case "claim-error":
				x.claimErr = errors.New("claim")
			case "input-mismatch":
				x.inputMismatch = true
			case "input-error":
				x.queryErr = errors.New("input")
			case "commit-error":
				x.commitErr = errors.New("commit")
			}
			l, e := checkedClaim(context.Background(), func(context.Context) (pgx.Tx, error) { return x, nil }, s)
			if l != nil || x.committed || !x.rolled {
				t.Fatal("unapproved claim escaped rollback")
			}
			if name != "unsupported-housekeeping" && name != "nil-lease" && e == nil {
				t.Fatal("scope failure missing")
			}
		})
	}
}
func TestProductionClaimCommitsOnlyExactSnapshotAndCanonicalConfig(t *testing.T) {
	s := validScope()
	x := &claimTX{s: s}
	l, e := checkedClaim(context.Background(), func(context.Context) (pgx.Tx, error) { return x, nil }, s)
	if e != nil || l == nil || !x.committed || x.rolled || x.queries != 1 {
		t.Fatalf("exact claim rejected: %v", e)
	}
}
func TestLegacyAdoptionRetainsExactLeaseWithoutUnsupportedInputRead(t *testing.T) {
	s := validScope()
	s.Phase = "adopt"
	x := &claimTX{s: s}
	l, e := checkedClaim(context.Background(), func(context.Context) (pgx.Tx, error) { return x, nil }, s)
	if e != nil || l == nil || !x.committed || x.queries != 0 {
		t.Fatal("legacy adopt accessor misuse", e)
	}
}
func TestScopeFailsClosedWithoutActualApprovalAndFreshIdentity(t *testing.T) {
	s := validScope()
	if e := s.validate(time.Now()); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"old", "future", "head", "collection", "missing-style", "missing-human", "missing-api", "missing-cpu", "phase"} {
		t.Run(name, func(t *testing.T) {
			v := s
			switch name {
			case "old":
				v.ObservedAt = time.Now().Add(-121 * time.Second)
			case "future":
				v.ObservedAt = time.Now().Add(time.Second)
			case "head":
				v.Head = 2648
			case "collection":
				v.Collection = uuid.New()
			case "missing-style":
				v.Style = uuid.Nil
			case "missing-human":
				v.HumanReviewSHA = ""
			case "missing-api":
				v.APIReceiptSHA = ""
			case "missing-cpu":
				v.CPUReceiptSHA = ""
			case "phase":
				v.Phase = "render.compose"
			}
			if v.validate(time.Now()) == nil {
				t.Fatal("invalid scope allowed")
			}
		})
	}
}
func TestPrivateReferenceRejectsChangedModeSymlinkAndUnknownSchema(t *testing.T) {
	p := filepath.Join(t.TempDir(), "scope.json")
	raw := []byte(`{"approved_for_coordinator_tick":true,"scope_sha256":"x","source_manifest_sha256":"y","reviewer":"root"}`)
	if e := os.WriteFile(p, raw, 0600); e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(raw)
	pin := hex.EncodeToString(h[:])
	var a approval
	if privateJSON(p, pin, &a) != nil {
		t.Fatal("valid private file refused")
	}
	os.Chmod(p, 0644)
	if privateJSON(p, pin, &a) == nil {
		t.Fatal("public mode accepted")
	}
	os.Chmod(p, 0600)
	link := p + ".link"
	os.Symlink(p, link)
	if privateJSON(link, pin, &a) == nil {
		t.Fatal("symlink accepted")
	}
	if privateJSON(p, strings.Repeat("b", 64), &a) == nil {
		t.Fatal("changed bytes accepted")
	}
	raw = []byte(`{"foreign":true}`)
	os.WriteFile(p, raw, 0600)
	h = sha256.Sum256(raw)
	if privateJSON(p, hex.EncodeToString(h[:]), &a) == nil {
		t.Fatal("unknown schema accepted")
	}
}

func fixedFrozen() json.RawMessage {
	return json.RawMessage(`{"version":2,"config":{"style_config":{"mode":"fixed"}},"stages":[{"phase":"resolve","operation":"style.resolve","output_role":"resolved_style","cardinality":"item"},{"phase":"color","operation":"image.color","output_role":"working_image","cardinality":"item"},{"phase":"local","operation":"image.local","output_role":"working_image","cardinality":"item"},{"phase":"quality","operation":"quality.check","output_role":"quality_report","cardinality":"item"}]}`)
}
func TestGraphFourFrozenStageAuthorityIsClosed(t *testing.T) {
	for _, name := range []string{"exact", "operation", "role", "cardinality", "phase"} {
		t.Run(name, func(t *testing.T) {
			s := validScope()
			s.Graph = 4
			x := &claimTX{s: s, frozen: fixedFrozen()}
			switch name {
			case "operation":
				x.frozen = json.RawMessage(strings.Replace(string(x.frozen), "image.color", "render.compose", 1))
			case "role":
				x.frozen = json.RawMessage(strings.Replace(string(x.frozen), "resolved_style", "root", 1))
			case "cardinality":
				x.frozen = json.RawMessage(strings.Replace(string(x.frozen), `"cardinality":"item"`, `"cardinality":"aggregate"`, 1))
			case "phase":
				x.frozen = json.RawMessage(strings.Replace(string(x.frozen), `"phase":"color"`, `"phase":"generate"`, 1))
			}
			l, e := checkedClaim(context.Background(), func(context.Context) (pgx.Tx, error) { return x, nil }, s)
			if name == "exact" {
				if e != nil || l == nil || !x.committed {
					t.Fatal("exact graph4 refused", e)
				}
			} else if e == nil || l != nil || x.committed || !x.rolled {
				t.Fatal("modified stage authority allowed")
			}
		})
	}
}
