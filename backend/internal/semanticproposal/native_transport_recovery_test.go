package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestNativeTransportRecovery(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	saved := nativeRecoveredTransport{SchemaVersion: 1, TaskID: ids[2], Manifest: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Chunks: []v2input.Ref{{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}}, Count: 3, EntriesSHA: strings.Repeat("c", 64)}
	raw, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	tx := &nativeSealTx{role: "photo_scheduler", raw: raw}
	built, err := recoverNativeTransportInTransaction(context.Background(), tx, ids[0], ids[1], ids[2], ids[3], ids[4])
	if err != nil || built.Manifest != saved.Manifest || built.Count != 3 || built.SHA256 != saved.EntriesSHA {
		t.Fatal("recovery", err)
	}
	if len(tx.calls) != 3 || tx.args[1][0] != ids[0].String() || len(tx.args[2]) != 5 {
		t.Fatal("authority order")
	}
	for i, id := range ids {
		if tx.args[2][i] != id {
			t.Fatal("substituted recovery scope")
		}
	}
	for _, body := range [][]byte{nil, []byte("null"), []byte("[]"), append(append([]byte(nil), raw...), []byte(" {}")...), []byte(strings.Replace(string(raw), "\"schema_version\":1", "\"schema_version\":2", 1)), []byte(strings.Replace(string(raw), "\"count\":3", "\"count\":4098", 1)), []byte(strings.Replace(string(raw), "{", "{\"unknown\":true,", 1))} {
		if _, err := decodeNativeRecoveredTransport(body, ids[2]); err == nil {
			t.Fatal("accepted invalid transport")
		}
	}
	if _, err := decodeNativeRecoveredTransport(raw, uuid.New()); err == nil {
		t.Fatal("foreign task")
	}
	saved.Chunks[0] = saved.Manifest
	duplicate, _ := json.Marshal(saved)
	if _, err := decodeNativeRecoveredTransport(duplicate, ids[2]); err == nil {
		t.Fatal("duplicate document")
	}
	for _, broken := range []*nativeSealTx{{role: "photo_api", raw: raw}, {role: "photo_scheduler"}, {role: "photo_scheduler", raw: raw, sqlErr: errors.New("read failure")}, {role: "photo_scheduler", raw: raw, execErr: errors.New("scope failure")}} {
		result, err := recoverNativeTransportInTransaction(context.Background(), broken, ids[0], ids[1], ids[2], ids[3], ids[4])
		if err == nil || result.Manifest.ArtifactID != uuid.Nil {
			t.Fatal("failed recovery exposed transport")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx = &nativeSealTx{role: "photo_scheduler", raw: raw}
	if _, err := recoverNativeTransportInTransaction(ctx, tx, ids[0], ids[1], ids[2], ids[3], ids[4]); !errors.Is(err, context.Canceled) || len(tx.calls) != 0 {
		t.Fatal("cancellation precedence")
	}
}

func assertFrozenNativeRecovery(t *testing.T, task uuid.UUID, built v2input.Built, recover func(context.Context, pgx.Tx) (v2input.Built, error)) {
	t.Helper()
	saved := nativeRecoveredTransport{SchemaVersion: 1, TaskID: task, Manifest: built.Manifest, Chunks: built.Chunks, Count: built.Count, EntriesSHA: built.SHA256}
	raw, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := recover(context.Background(), &nativeSealTx{role: "photo_scheduler", raw: raw})
	if err != nil || recovered.Manifest != built.Manifest || recovered.SHA256 != built.SHA256 {
		t.Fatal("frozen recovery", err)
	}
	saved.EntriesSHA = strings.Repeat("f", 64)
	raw, err = json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := recover(context.Background(), &nativeSealTx{role: "photo_scheduler", raw: raw})
	if err == nil || rejected.Manifest.ArtifactID != uuid.Nil {
		t.Fatal("accepted substituted frozen entries")
	}
}

// A transaction fixture separates persisted transport from the admission receipt.
type nativeResumeTx struct {
	nativeSealTx
	receipt               []byte
	committed, rolledBack bool
	commitErr             error
}

func (tx *nativeResumeTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if query == "SELECT photo.seal_semantic_native_preparation($1,$2,$3::jsonb)" {
		tx.calls = append(tx.calls, query)
		tx.args = append(tx.args, args)
		return nativeSealRow{raw: tx.receipt}
	}
	return tx.nativeSealTx.QueryRow(ctx, query, args...)
}
func (tx *nativeResumeTx) Commit(context.Context) error   { tx.committed = true; return tx.commitErr }
func (tx *nativeResumeTx) Rollback(context.Context) error { tx.rolledBack = true; return nil }

func assertNativeResume(t *testing.T, task uuid.UUID, built v2input.Built, ticket v2input.JobTicket, resume func(context.Context, pgx.Tx) (v2input.JobTicket, error)) {
	t.Helper()
	saved := nativeRecoveredTransport{SchemaVersion: 1, TaskID: task, Manifest: built.Manifest, Chunks: built.Chunks, Count: built.Count, EntriesSHA: built.SHA256}
	raw, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := json.Marshal(ticket)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"success", "unknown_commit", "aborted_commit", "missing", "substituted"} {
		attempts := 0
		var transactions []*nativeResumeTx
		failure := errors.New("commit outcome unknown")
		result, err := nativeTicketWithRetry(context.Background(), func(context.Context) (pgx.Tx, error) {
			attempts++
			tx := &nativeResumeTx{nativeSealTx: nativeSealTx{role: "photo_scheduler", raw: raw}, receipt: receipt}
			switch mode {
			case "unknown_commit":
				tx.commitErr = failure
			case "aborted_commit":
				if attempts == 1 {
					tx.commitErr = &pgconn.PgError{Code: "40001"}
				}
			case "missing":
				tx.raw = nil
			case "substituted":
				changed := saved
				changed.EntriesSHA = strings.Repeat("f", 64)
				tx.raw, _ = json.Marshal(changed)
			}
			transactions = append(transactions, tx)
			return tx, nil
		}, func(tx pgx.Tx) (v2input.JobTicket, error) { return resume(context.Background(), tx) })
		switch mode {
		case "success", "aborted_commit":
			if err != nil || result != ticket {
				t.Fatal("resume ticket", mode, err)
			}
			expected := 1
			if mode == "aborted_commit" {
				expected = 2
			}
			if attempts != expected {
				t.Fatal("retry count", mode, attempts)
			}
			for _, tx := range transactions {
				if len(tx.calls) != 6 || tx.calls[2] != "SELECT photo.semantic_native_registered_transport($1,$2,$3,$4,$5)" || tx.calls[5] != "SELECT photo.seal_semantic_native_preparation($1,$2,$3::jsonb)" || !tx.committed {
					t.Fatal("recovery did not precede full admission", mode)
				}
			}
		case "unknown_commit":
			if !errors.Is(err, failure) || result != (v2input.JobTicket{}) || attempts != 1 {
				t.Fatal("unknown commit exposed receipt or retried")
			}
		default:
			if err == nil || result != (v2input.JobTicket{}) || attempts != 1 || transactions[0].committed || !transactions[0].rolledBack || len(transactions[0].calls) != 3 {
				t.Fatal("invalid recovery reached sealing", mode, err)
			}
		}
	}
}
