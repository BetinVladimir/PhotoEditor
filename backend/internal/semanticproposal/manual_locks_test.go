package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

type manualLockRow struct {
	role  string
	ready bool
	err   error
}

func (r manualLockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	switch p := dest[0].(type) {
	case *string:
		*p = r.role
	case *bool:
		*p = r.ready
	}
	return nil
}

type manualLockTx struct {
	pgx.Tx
	calls  []string
	args   [][]any
	role   string
	failAt int
	err    error
}

func (t *manualLockTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	t.calls = append(t.calls, sql)
	t.args = append(t.args, args)
	if len(t.calls) == t.failAt {
		return manualLockRow{err: t.err}
	}
	return manualLockRow{role: t.role, ready: len(t.calls) != t.failAt}
}
func TestManualRetainedProofTransaction(t *testing.T) {
	proof := manualPinTestProof()
	reader := SQLTransferSourceReader{ProjectID: proof.project, Bucket: "verified-bucket"}
	who := auth.Identity{UserID: proof.actor, SessionID: proof.session}
	tx := &manualLockTx{role: "photo_scheduler"}
	if err := reader.lockManualPreparationProof(context.Background(), tx, who, proof.tenant, proof.binding, proof); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 4 || !strings.Contains(tx.calls[1], "manual_evidence") || !strings.Contains(tx.calls[2], "person_inventory") || tx.calls[3] != tx.calls[1] {
		t.Fatal("missing lock/barrier/recheck", tx.calls)
	}
	pins, err := marshalManualEvidencePins(proof, reader.Bucket)
	if err != nil || tx.args[1][9] != string(pins) || tx.args[3][9] != string(pins) || tx.args[1][7] != proof.objects.Pixel.ref.ArtifactID {
		t.Fatal("proof pins changed", err)
	}
	for _, fail := range []int{2, 3, 4} {
		tx := &manualLockTx{role: "photo_scheduler", failAt: fail}
		if err := reader.lockManualPreparationProof(context.Background(), tx, who, proof.tenant, proof.binding, proof); !errors.Is(err, v2input.ErrConflict) || len(tx.calls) != fail {
			t.Fatal("failed lock advanced", fail, err)
		}
	}
	dependencyErr := errors.New("database failure")
	tx = &manualLockTx{role: "photo_scheduler", failAt: 2, err: dependencyErr}
	if err := reader.lockManualPreparationProof(context.Background(), tx, who, proof.tenant, proof.binding, proof); !errors.Is(err, dependencyErr) || len(tx.calls) != 2 {
		t.Fatal("lost SQL error", err)
	}
	tx = &manualLockTx{role: "photo_api"}
	if err := reader.lockManualPreparationProof(context.Background(), tx, who, proof.tenant, proof.binding, proof); !errors.Is(err, v2input.ErrInvalid) || len(tx.calls) != 1 {
		t.Fatal("wrong role", err)
	}
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := reader.lockManualPreparationProof(context.Background(), tx, who, uuid.New(), proof.binding, proof); !errors.Is(err, v2input.ErrInvalid) || len(tx.calls) != 0 {
		t.Fatal("foreign scope reached DB", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := reader.lockManualPreparationProof(ctx, tx, who, proof.tenant, proof.binding, proof); !errors.Is(err, context.Canceled) || len(tx.calls) != 0 {
		t.Fatal("cancel reached DB", err)
	}
}

func TestManualProofBatchLocksBeforeBarrier(t *testing.T) {
	first := manualPinTestProof()
	second := manualPinTestProof()
	second.tenant, second.project, second.actor, second.session, second.binding = first.tenant, first.project, first.actor, first.session, first.binding
	first.objects.Root.ref.ArtifactID = uuid.MustParse("ffffffff-ffff-4fff-8fff-ffffffffffff")
	second.objects.Root.ref.ArtifactID = uuid.MustParse("00000000-0000-4000-8000-000000000001")
	reader := SQLTransferSourceReader{ProjectID: first.project, Bucket: "verified-bucket"}
	who := auth.Identity{UserID: first.actor, SessionID: first.session}
	tx := &manualLockTx{role: "photo_scheduler"}
	if err := reader.lockManualPreparationProofs(context.Background(), tx, who, first.tenant, first.binding, []manualPreparationProof{first, second, first}); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 6 || !strings.Contains(tx.calls[3], "person_inventory") || tx.args[1][7] != second.objects.Pixel.ref.ArtifactID || tx.args[2][7] != first.objects.Pixel.ref.ArtifactID || tx.args[1][9] != tx.args[4][9] || tx.args[2][9] != tx.args[5][9] {
		t.Fatal("batch lock order/pins changed", tx.calls)
	}
	for _, fail := range []int{2, 3, 4, 5, 6} {
		tx := &manualLockTx{role: "photo_scheduler", failAt: fail}
		if err := reader.lockManualPreparationProofs(context.Background(), tx, who, first.tenant, first.binding, []manualPreparationProof{first, second}); !errors.Is(err, v2input.ErrConflict) || len(tx.calls) != fail {
			t.Fatal("failed batch continued", fail, err)
		}
	}
	conflicting := first
	conflicting.objects.Params.object.Key += "-changed"
	invalid := second
	invalid.session = uuid.New()
	tooMany := make([]manualPreparationProof, 65)
	for i := range tooMany {
		tooMany[i] = first
	}
	for _, batch := range [][]manualPreparationProof{nil, tooMany, {first, invalid}, {first, conflicting}} {
		tx := &manualLockTx{role: "photo_scheduler"}
		if err := reader.lockManualPreparationProofs(context.Background(), tx, who, first.tenant, first.binding, batch); err == nil || len(tx.calls) != 0 {
			t.Fatal("invalid batch reached SQL", err)
		}
	}
}
