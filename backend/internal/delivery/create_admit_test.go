package delivery

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
)

type createAdmissionTx struct {
	queries  []string
	args     [][]any
	refuseAt int
}

func (tx *createAdmissionTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	tx.queries = append(tx.queries, query)
	tx.args = append(tx.args, args)
	return proofRow{accepted: tx.refuseAt != len(tx.queries)}
}

func TestCreateAdmissionBindsPendingProofAndPackage(t *testing.T) {
	i, session, catalog, items := createDocumentsFixture(t)
	who := auth.Identity{UserID: i.actor, SessionID: session}
	raw, err := i.EncodePending(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	for _, refuse := range []int{0, 1, 2} {
		tx := &createAdmissionTx{refuseAt: refuse}
		err := i.AdmitPreparedPackage(context.Background(), tx, who, i.tenant, raw)
		if (err != nil) != (refuse != 0) {
			t.Fatalf("refusal %d: %v", refuse, err)
		}
		want := 2
		if refuse == 1 {
			want = 1
		}
		if len(tx.queries) != want || !strings.Contains(tx.queries[0], "save_v30_delivery_prepare_proof") {
			t.Fatal("proof order")
		}
		if want == 2 {
			if !strings.Contains(tx.queries[1], "admit_v30_delivery_package") || tx.args[1][3] != i.identity.Package || tx.args[1][4] != i.identity.Workflow || tx.args[1][5] != i.identity.PrepareTask || tx.args[1][6] != i.request.Name {
				t.Fatal("package scope")
			}
		}
	}
	tx := &createAdmissionTx{}
	if i.AdmitPreparedPackage(context.Background(), tx, who, uuid.New(), raw) == nil || len(tx.queries) != 0 {
		t.Fatal("foreign scope reached SQL")
	}
	if i.AdmitPreparedPackage(context.Background(), tx, who, i.tenant, []byte(`{}`)) == nil || len(tx.queries) != 0 {
		t.Fatal("invalid pending reached SQL")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if i.AdmitPreparedPackage(ctx, tx, who, i.tenant, raw) == nil || len(tx.queries) != 0 {
		t.Fatal("cancelled IO")
	}
}
