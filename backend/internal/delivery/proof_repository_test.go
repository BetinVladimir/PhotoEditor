package delivery

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
 "reflect"
)

type proofRow struct{ accepted bool }

func (r proofRow) Scan(dest ...any) error { *dest[0].(*bool) = r.accepted; return nil }

type proofTx struct {
	calls    int
	args     []any
	accepted bool
}

func (tx *proofTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	tx.calls++
	tx.args = args
	return proofRow{tx.accepted}
}
func TestSavePrepareProofBindsCurrentIdentityAndExactDocuments(t *testing.T) {
	ref, snapshot, policy, configs := freezeFixture(t)
	_, raw, sha, err := CompileFrozenInput(ref, snapshot, policy, configs)
	if err != nil {
		t.Fatal(err)
	}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	scope := PrepareProofScope{Tenant: uuid.New(), Project: uuid.New(), Run: uuid.New(), Task: uuid.New(), Frozen: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}}
	_, _, params, err := CompilePrepareProof(scope.Project, who.SessionID, scope.Frozen, raw, snapshot, configs)
	if err != nil {
		t.Fatal(err)
	}
	scope.Params = v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(params)}
	tx := &proofTx{accepted: true}
	if err := SavePrepareProof(context.Background(), tx, who, scope, raw, snapshot, configs); err != nil {
		t.Fatal(err)
	}
	if tx.calls != 1 || len(tx.args) != 10 || tx.args[0] != scope.Tenant || tx.args[1] != who.UserID || tx.args[2] != who.SessionID || tx.args[3] != scope.Project || tx.args[8] != string(params) {
		t.Fatal("wrong scope")
	}
	tx.accepted = false
	if SavePrepareProof(context.Background(), tx, who, scope, raw, snapshot, configs) == nil {
		t.Fatal("refusal accepted")
	}
	tx.calls = 0
	scope.Params.SHA256 = digest([]byte("foreign"))
	if SavePrepareProof(context.Background(), tx, who, scope, raw, snapshot, configs) == nil || tx.calls != 0 {
		t.Fatal("foreign params reached SQL")
	}
}

// Session authorization is mutable; immutable task Params and content are not.
func TestPrepareProofSessionRotationPreservesImmutableCompilation(t *testing.T) {
 ref, snapshot, policy, configs := freezeFixture(t)
 _, raw, sha, err := CompileFrozenInput(ref, snapshot, policy, configs)
 if err != nil { t.Fatal(err) }
 project, first, second := uuid.New(), uuid.New(), uuid.New()
 frozen := v2input.Ref{ArtifactID:uuid.New(), SHA256:sha}
 a, proofA, paramsA, err := CompilePrepareProof(project, first, frozen, raw, snapshot, configs)
 if err != nil { t.Fatal(err) }
 b, proofB, paramsB, err := CompilePrepareProof(project, second, frozen, raw, snapshot, configs)
 if err != nil { t.Fatal(err) }
 if string(paramsA)!=string(paramsB) || string(proofA)==string(proofB) || a.AuthSessionID!=first || b.AuthSessionID!=second { t.Fatal("session rotation changed immutable Params or failed to rebind witness") }
 a.AuthSessionID=second
 if !reflect.DeepEqual(a,b) { t.Fatal("rotation altered content evidence") }
}
