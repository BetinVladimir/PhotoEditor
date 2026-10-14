package v2optical

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type multiAdmissionTx struct {
	perspectiveArrayTx
	kind        string
	transport   []byte
	refuse      bool
	commitError error
	calls       int
}

func (t *multiAdmissionTx) Commit(ctx context.Context) error {
	if t.commitError != nil {
		return t.commitError
	}
	return t.perspectiveArrayTx.Commit(ctx)
}
func (t *multiAdmissionTx) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	if q == "SELECT current_user" {
		return t.negativeLockTx.QueryRow(ctx, q, args...)
	}
	return negativeLockRow(func(dest ...any) error {
		if args[0] != t.tenant {
			return errors.New("foreign tenant")
		}
		expected := map[string]string{
			"read":     "SELECT photo.read_v29_multi_published_transport(",
			"register": "SELECT photo.register_v29_multi_transport(",
			"seal":     "SELECT photo.seal_v29_multi_graph(",
		}[t.kind]
		if expected == "" || !strings.HasPrefix(q, expected) {
			return errors.New("wrong SQL authority")
		}
		t.calls++
		if t.kind == "read" {
			*dest[0].(*[]byte) = append([]byte(nil), t.transport...)
			return nil
		}
		*dest[0].(*bool) = !t.refuse
		return nil
	})
}

type multiAdmissionDB struct {
	txs  []*multiAdmissionTx
	next int
}

func (d *multiAdmissionDB) Begin(context.Context) (pgx.Tx, error) {
	if d.next >= len(d.txs) {
		return nil, errors.New("unexpected transaction")
	}
	tx := d.txs[d.next]
	d.next++
	return tx, nil
}

func TestMultiAdmissionCommitsExactAuthoritiesAndStopsOnRefusal(t *testing.T) {
	for _, mode := range []string{"ok", "registration_refused", "seal_refused", "ambiguous_commit"} {
		t.Run(mode, func(t *testing.T) {
			operation := "optical.panorama"
			pin, locks, report, calibration, params, originals := multiQualifiedFixture(t, operation)
			qualified, err := VerifyMultiQualificationEvidence(operation, pin, locks, report, calibration, params, originals)
			if err != nil {
				t.Fatal(err)
			}
			capacity, err := MultiResources(operation, params, originals)
			if err != nil {
				t.Fatal(err)
			}
			tenant := uuid.New()
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			draft, err := CompileMultiDraft(context.Background(), who, tenant, uuid.New(), uuid.New(), uuid.NewString(), operation, params, originals, qualified, capacity)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := draft.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			published := v2input.Ref{ArtifactID: snapshot.ParamsDocument, SHA256: snapshot.ParamsSHA}
			plan, err := CompileMultiTaskPlan(context.Background(), operation, params, originals, qualified, capacity)
			if err != nil {
				t.Fatal(err)
			}
			graph, err := BuildMultiTaskTransport(context.Background(), snapshot.Task, plan, originals, qualified, published, &negativePublisher{})
			if err != nil {
				t.Fatal(err)
			}
			transport, err := canonical.Marshal(map[string]any{"manifest": graph.Built.Manifest, "chunks": graph.Built.Chunks, "count": graph.Built.Count, "sha256": graph.Built.SHA256})
			if err != nil {
				t.Fatal(err)
			}
			db := &multiAdmissionDB{}
			for _, kind := range []string{"read", "register", "seal"} {
				db.txs = append(db.txs, &multiAdmissionTx{perspectiveArrayTx: perspectiveArrayTx{negativeLockTx: negativeLockTx{tenant: tenant}}, kind: kind, transport: transport})
			}
			switch mode {
			case "registration_refused":
				db.txs[1].refuse = true
			case "seal_refused":
				db.txs[2].refuse = true
			case "ambiguous_commit":
				db.txs[2].commitError = errors.New("ambiguous commit")
			}
			err = AdmitMultiTask(context.Background(), db, "photos", who, tenant, draft, qualified, published)
			if mode == "ok" {
				if err != nil || db.next != 3 {
					t.Fatal("admission failed", err)
				}
				for _, tx := range db.txs {
					if !tx.committed || tx.calls != 1 {
						t.Fatal("authority not committed")
					}
				}
			} else {
				if err == nil {
					t.Fatal("refusal/commit error ignored")
				}
				if mode == "registration_refused" && db.next != 2 {
					t.Fatal("seal reached after registration refusal")
				}
				last := db.txs[db.next-1]
				if last.committed || !last.rolledBack {
					t.Fatal("failed authority committed")
				}
			}
		})
	}
}
