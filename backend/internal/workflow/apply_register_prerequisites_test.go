package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

type prerequisiteRegistrationDB struct {
	query string
	args  []any
	err   error
}

func (d *prerequisiteRegistrationDB) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	d.query = q
	d.args = args
	return prerequisiteRegistrationRow{d.err}
}
func (*prerequisiteRegistrationDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected rows query")
}

type prerequisiteRegistrationRow struct{ err error }

func (r prerequisiteRegistrationRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 1 {
		return errors.New("registration scan shape")
	}
	v, ok := dest[0].(*bool)
	if !ok {
		return errors.New("registration boolean expected")
	}
	*v = true
	return nil
}

func TestApplyRegisterPrerequisitesCanonicalRootAndEdges(t *testing.T) {
	edge := uuid.New()
	for _, graph := range []int{3, 4} {
		for _, tc := range []struct {
			name  string
			edges []uuid.UUID
		}{{"nil-root", nil}, {"explicit-empty-root", []uuid.UUID{}}, {"dependent", []uuid.UUID{edge}}} {
			t.Run(tc.name+string(rune('0'+graph)), func(t *testing.T) {
				l := ApplyLease{TenantID: uuid.New(), RunID: uuid.New(), Token: uuid.New(), GraphVersion: graph}
				n := applyNodePlan{ID: uuid.New(), Phase: "resolve", NodeKey: "style.resolve/0000", Operation: "style.resolve", SourceItemID: uuid.New(), SourceAssetID: uuid.New(), SourceVersionID: uuid.New(), Inputs: []review.ArtifactRef{{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}}, Prerequisites: tc.edges, ParamsSHA: strings.Repeat("b", 64), Params: json.RawMessage(`{"image":{"artifact_id":"00000000-0000-0000-0000-000000000001","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`)}
				db := &prerequisiteRegistrationDB{}
				ok, err := (ApplyStore{DB: db}).Register(context.Background(), l, n)
				if err != nil || !ok {
					t.Fatal("register failed", err)
				}
				expectedFunction := "photo.register_apply_node"
				if graph == 4 {
					expectedFunction = "photo.register_apply_v4_node"
				}
				if db.query != "SELECT "+expectedFunction+"($1,$2,$3,$4)" || len(db.args) != 4 || !reflect.DeepEqual(db.args[:3], []any{l.TenantID, l.RunID, l.Token}) {
					t.Fatal("registration function or lease changed")
				}
				raw, ok := db.args[3].([]byte)
				if !ok {
					t.Fatal("canonical payload bytes required")
				}
				var fields map[string]json.RawMessage
				if json.Unmarshal(raw, &fields) != nil {
					t.Fatal("payload JSON malformed")
				}
				if string(fields["prerequisites"]) == "null" || len(fields["prerequisites"]) == 0 || fields["prerequisites"][0] != '[' {
					t.Fatalf("SQL prerequisite-array contract violated: %s", fields["prerequisites"])
				}
				var got []uuid.UUID
				if json.Unmarshal(fields["prerequisites"], &got) != nil || len(got) != len(tc.edges) {
					t.Fatal("prerequisites changed")
				}
				if len(tc.edges) > 0 && !reflect.DeepEqual(got, tc.edges) {
					t.Fatal("edge identity/order changed")
				}
				normalized := n
				if normalized.Prerequisites == nil {
					normalized.Prerequisites = []uuid.UUID{}
				}
				expected, err := canonical.Marshal(normalized)
				if err != nil || string(raw) != string(expected) {
					t.Fatal("unrelated canonical node fields changed")
				}
				if tc.edges == nil && n.Prerequisites != nil {
					t.Fatal("caller node mutated")
				}
			})
		}
	}
}
func TestApplyRegisterPrerequisitesPreservesUnderlyingDatabaseFailure(t *testing.T) {
	failure := errors.New("fixture database failure")
	db := &prerequisiteRegistrationDB{err: failure}
	ok, err := (ApplyStore{DB: db}).Register(context.Background(), ApplyLease{GraphVersion: 4}, applyNodePlan{})
	if ok || !errors.Is(err, failure) {
		t.Fatal("underlying database error hidden")
	}
}
