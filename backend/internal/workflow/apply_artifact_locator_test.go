package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/review"
	"reflect"
	"strings"
	"testing"
)

type locatorRow struct {
	values []any
	err    error
}

func (r locatorRow) Scan(d ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(d) != len(r.values) {
		return errors.New("locator scan shape")
	}
	for i, v := range r.values {
		reflect.ValueOf(d[i]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

type locatorDB struct {
	query    string
	args     []any
	err      error
	artifact styleArtifact
	style    styleArtifact
	open     bool
}

func (d *locatorDB) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	d.query = q
	d.args = args
	if d.open {
		return locatorRow{err: errors.New("input stream still open")}
	}
	a := d.artifact
	return locatorRow{[]any{a.ArtifactID, a.SHA256, a.Bytes, a.Bucket, a.Key, a.VersionID, a.MediaType}, d.err}
}
func (d *locatorDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	d.open = true
	return &locatorInputRows{owner: d}, nil
}

type locatorInputRows struct {
	pgx.Rows
	owner *locatorDB
	sent  bool
}

func (r *locatorInputRows) Next() bool {
	if r.sent {
		return false
	}
	r.sent = true
	return true
}
func (r *locatorInputRows) Scan(d ...any) error {
	a := r.owner.style
	id, sha, size := a.ArtifactID, a.SHA256, a.Bytes
	return (locatorRow{values: []any{0, uuid.New(), uuid.New(), uuid.New(), uuid.New(), strings.Repeat("b", 64), int64(20), "photos", "source", "v1", "application/json", json.RawMessage(`{}`), json.RawMessage(`{}`), &id, &sha, &size}}).Scan(d...)
}
func (r *locatorInputRows) Close()     { r.owner.open = false }
func (r *locatorInputRows) Err() error { return nil }
func locatorFixture() (ApplyLease, review.ArtifactRef, *locatorDB) {
	a := styleArtifact{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64), Bytes: 42, Bucket: "photos", Key: "style", VersionID: "v1", MediaType: "application/json"}
	return ApplyLease{TenantID: uuid.New(), RunID: uuid.New(), Token: uuid.New(), ItemCount: 1, GraphVersion: 3}, a.ref(), &locatorDB{artifact: a, style: a}
}
func TestApplyArtifactLocatorCarriesExactLease(t *testing.T) {
	l, ref, db := locatorFixture()
	got, e := (ApplyStore{DB: db}).Artifact(context.Background(), l, ref)
	if e != nil || got != db.artifact || !strings.Contains(db.query, "photo.apply_artifact_locator($1,$2,$3,$4,$5)") || !reflect.DeepEqual(db.args, []any{l.TenantID, l.RunID, l.Token, ref.ID, ref.SHA256}) {
		t.Fatal("unbound locator", e)
	}
}
func TestApplyStyleLocatorInputsNonemptyAndStreamClosed(t *testing.T) {
	l, _, db := locatorFixture()
	items, e := (ApplyStore{DB: db}).Inputs(context.Background(), l)
	if e != nil || len(items) != 1 || items[0].Style == nil || *items[0].Style != db.artifact || db.open || !strings.Contains(db.query, "photo.apply_input_style_locator") {
		t.Fatal("nonempty style locator path", e)
	}
}
func TestApplyLocatorRejectsPartialOrDifferentMetadata(t *testing.T) {
	for _, mode := range []string{"id", "sha", "bytes", "bucket", "key", "version", "nullversion", "type"} {
		l, ref, db := locatorFixture()
		switch mode {
		case "id":
			db.artifact.ArtifactID = uuid.New()
		case "sha":
			db.artifact.SHA256 = strings.Repeat("b", 64)
		case "bytes":
			db.artifact.Bytes = 0
		case "bucket":
			db.artifact.Bucket = ""
		case "key":
			db.artifact.Key = ""
		case "version":
			db.artifact.VersionID = ""
		case "nullversion":
			db.artifact.VersionID = "null"
		case "type":
			db.artifact.MediaType = ""
		}
		if _, e := (ApplyStore{DB: db}).Artifact(context.Background(), l, ref); e == nil {
			t.Fatal(mode)
		}
	}
}
func TestApplyLocatorInvalidLeaseRefDoesNotQuery(t *testing.T) {
	for _, mode := range []string{"tenant", "run", "token", "artifact", "sha"} {
		l, ref, db := locatorFixture()
		switch mode {
		case "tenant":
			l.TenantID = uuid.Nil
		case "run":
			l.RunID = uuid.Nil
		case "token":
			l.Token = uuid.Nil
		case "artifact":
			ref.ID = uuid.Nil
		case "sha":
			ref.SHA256 = "notSHA"
		}
		if _, e := (ApplyStore{DB: db}).Artifact(context.Background(), l, ref); e == nil || db.query != "" {
			t.Fatal(mode)
		}
	}
}
func TestApplyLocatorRefusalPropagates(t *testing.T) {
	l, ref, db := locatorFixture()
	db.err = pgx.ErrNoRows
	if _, e := (ApplyStore{DB: db}).Artifact(context.Background(), l, ref); !errors.Is(e, pgx.ErrNoRows) {
		t.Fatal("authority refusal hidden", e)
	}
}
func TestConservativeLocatorCarriesProposalAndLease(t *testing.T) {
	l, ref, db := locatorFixture()
	proposal := applyOutput{TaskID: uuid.New(), Artifact: styleArtifact{ArtifactID: ref.ID, SHA256: ref.SHA256}}
	_, e := (ApplyStore{DB: db}).conservativeProposalReport(context.Background(), l, proposal)
	if e != nil || !strings.Contains(db.query, "photo.apply_conservative_report_locator") || !reflect.DeepEqual(db.args, []any{l.TenantID, l.RunID, l.Token, proposal.TaskID, ref.ID, ref.SHA256}) {
		t.Fatal("report locator binding", e)
	}
}
func TestOperatorArtifactRejectsDifferentLease(t *testing.T) {
	l, ref, db := locatorFixture()
	s := &OperatorApplyLedger{lease: l, objects: map[uuid.UUID]styleArtifact{ref.ID: db.artifact}}
	if _, e := s.Artifact(context.Background(), l, ref); e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"tenant", "run", "token"} {
		wrong := l
		switch mode {
		case "tenant":
			wrong.TenantID = uuid.New()
		case "run":
			wrong.RunID = uuid.New()
		case "token":
			wrong.Token = uuid.New()
		}
		if _, e := s.Artifact(context.Background(), wrong, ref); e == nil {
			t.Fatal(mode)
		}
	}
}
