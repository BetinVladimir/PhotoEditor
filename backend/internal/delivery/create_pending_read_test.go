package delivery

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"testing"
)

type recoveryRow struct{ scan func(...any) error }

func (r recoveryRow) Scan(dest ...any) error { return r.scan(dest...) }

type recoveryTx struct {
	calls   int
	allowed bool
	hash    string
	status  *int
	live    bool
	missing bool
}

func (tx *recoveryTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	tx.calls++
	call := tx.calls
	return recoveryRow{func(dest ...any) error {
		if call == 1 {
			*dest[0].(*bool) = tx.allowed
			return nil
		}
		if tx.missing {
			return pgx.ErrNoRows
		}
		*dest[0].(*string) = tx.hash
		*dest[1].(**int) = tx.status
		*dest[2].(*[]byte) = nil
		*dest[3].(*bool) = tx.live
		return nil
	}}
}
func TestCreateEarlyRecoveryAbsentCompletedConflictExpiredAndAuthorization(t *testing.T) {
	intent, session, _, _ := createDocumentsFixture(t)
	who := auth.Identity{UserID: intent.actor, SessionID: session}
	status := 202
	for _, tc := range []struct {
		name                 string
		tx                   recoveryTx
		wantError, completed bool
		calls                int
	}{
		{"absent", recoveryTx{allowed: true, missing: true}, false, false, 2},
		{"empty pending", recoveryTx{allowed: true, hash: intent.RequestSHA(), live: true}, false, false, 2},
		{"completed", recoveryTx{allowed: true, hash: intent.RequestSHA(), status: &status}, false, true, 2},
		{"conflict", recoveryTx{allowed: true, hash: "different", live: true}, true, false, 2},
		{"expired", recoveryTx{allowed: true, hash: intent.RequestSHA()}, true, false, 2},
		{"denied", recoveryTx{}, true, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := tc.tx
			result, err := intent.ReadPending(context.Background(), &tx, who, intent.tenant)
			if (err != nil) != tc.wantError || result.Completed != tc.completed || tx.calls != tc.calls {
				t.Fatal(result, err, tx.calls)
			}
		})
	}
	tx := &recoveryTx{}
	if _, err := intent.ReadPending(context.Background(), tx, who, uuid.New()); err == nil || tx.calls != 0 {
		t.Fatal("foreign scope queried")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := intent.ReadPending(ctx, tx, who, intent.tenant); err == nil || tx.calls != 0 {
		t.Fatal("cancelled query")
	}
}

func TestSelectedCreateRecoveryAcceptsOnlySelectionOrPromotedOrdinary(t *testing.T) {
	intent, session, catalog, items := createDocumentsFixture(t)
	selection, err := intent.EncodeWatermarkSelectionPending(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := intent.EncodePending(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{selection, ordinary} {
		pending, err := intent.decodeRecoveryPending(context.Background(), session, raw, false, true)
		if err != nil || pending.RequestSHA != intent.RequestSHA() {
			t.Fatal("valid selected-create state refused", err)
		}
	}
	if _, err := intent.decodeRecoveryPending(context.Background(), session, ordinary, true, false); err == nil {
		t.Fatal("watermark-only reader accepted ordinary executable state")
	}
	corrupted := append([]byte(nil), ordinary...)
	corrupted[len(corrupted)-2] ^= 1
	if _, err := intent.decodeRecoveryPending(context.Background(), session, corrupted, false, true); err == nil {
		t.Fatal("corrupt pending state accepted")
	}
}
