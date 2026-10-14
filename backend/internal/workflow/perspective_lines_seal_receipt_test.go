package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
)

type linesSealReceiptRow struct {
	accepted bool
	err      error
}

func (r linesSealReceiptRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*bool) = r.accepted
	return nil
}

type linesSealReceiptTx struct {
	pgx.Tx
	accepted bool
	err      error
	args     []any
	query    string
}

func (tx *linesSealReceiptTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	tx.query = query
	tx.args = append([]any(nil), args...)
	return linesSealReceiptRow{accepted: tx.accepted, err: tx.err}
}
func TestPerspectiveLinesSealReceiptAdapterPreservesOriginalProofAndRefusal(t *testing.T) {
	tenant, run := uuid.New(), uuid.New()
	proof := []byte(`{"chunk_jcs":"{\"schema_version\":1}","schema_version":1}`)
	witness := []byte(`{"source":{"object_version":"version-2"},"schema_version":1}`)
	queryFailure := errors.New("database unavailable")
	for _, test := range []struct {
		accepted       bool
		queryErr, want error
	}{
		{true, nil, nil}, {false, nil, artifactjson.ErrStale}, {false, queryFailure, queryFailure},
	} {
		tx := &linesSealReceiptTx{accepted: test.accepted, err: test.queryErr}
		err := sealPerspectiveLinesTransportInTx(context.Background(), tx, tenant, run, proof, witness)
		if !errors.Is(err, test.want) {
			t.Fatal(err, test.want)
		}
		if !strings.Contains(tx.query, "photo.seal_v29_perspective_lines_transport") || len(tx.args) != 4 || tx.args[0] != tenant || tx.args[1] != run || tx.args[2] != string(proof) || tx.args[3] != string(witness) {
			t.Fatal("lost scope/original proof")
		}
	}
}
