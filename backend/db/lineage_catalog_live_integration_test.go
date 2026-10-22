package migrations

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"os"
 "strings"
	"testing"
	"time"
)

type lineageLiveReadOnlySource struct {
	connection           *pgx.Conn
	observedTransactions int
}

func (source *lineageLiveReadOnlySource) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	transaction, err := source.connection.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	var readOnly, isolation string
	err = transaction.QueryRow(ctx, "SELECT current_setting('transaction_read_only'),current_setting('transaction_isolation')").Scan(&readOnly, &isolation)
	if err != nil || readOnly != "on" || isolation != "repeatable read" {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = transaction.Rollback(cleanup)
		return nil, errors.New("observer transaction is not actually read-only repeatable-read")
	}
	source.observedTransactions++
	return transaction, nil
}

// This test reads only metadata from an explicitly supplied isolated database.
// A matched observation is deliberately not permission to install migrations.
func TestLineageCatalogLiveProjectionAndStableReadOnlySnapshot(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("explicit isolated PostgreSQL18 database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	connection, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("isolated database connection failed")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		_ = connection.Close(cleanup)
	}()
	source := &lineageLiveReadOnlySource{connection: connection}
	first, err := ObserveLineage(ctx, source, DefaultLineageObservationLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !first.Complete || !first.ExternalDependencyValidationRequired || len(first.Objects) == 0 || len(first.Ledger) == 0 {
		t.Fatal("incomplete observation or external trust boundary lost")
	}
	second, err := ObserveLineage(ctx, source, DefaultLineageObservationLimits())
	if err != nil {
		t.Fatal(err)
	}
	if source.observedTransactions != 2 {
		t.Fatal("read-only modes not observed on both transactions")
	}
	if first.SemanticSHA256 != second.SemanticSHA256 || first.ExactLedgerSHA256 != second.ExactLedgerSHA256 {
		t.Fatal("catalog changed or observation is nondeterministic")
	}
	// A local test reference checks exact-match wiring; it is not a reviewed
	// deployment manifest, schema compatibility claim or installation approval.
	localReference := []LineageSnapshotAuthority{{Identity: "test-reference-no-install-authority", SemanticSHA256: first.SemanticSHA256}}
	identity, err := ClassifyLineageObservation(second, localReference)
	if err != nil || identity != localReference[0].Identity {
		t.Fatal("stable actual catalog did not match the local test reference", err)
	}
	if _, err = ClassifyLineageObservation(second, nil); err == nil {
		t.Fatal("missing independent reference accepted")
	}
	second.Complete = false
	if _, err = ClassifyLineageObservation(second, localReference); err == nil {
		t.Fatal("partial observation accepted")
	}
}


// Database-local metadata must survive a real independent dump/restore. Shared
// cluster roles and external dependencies still require separate provenance.
func TestLineageCatalogIndependentRestoreEquivalence(t *testing.T) {
    mainDSN, referenceDSN := os.Getenv("PHOTO_TEST_DATABASE_URL"), os.Getenv("PHOTO_LINEAGE_REFERENCE_DATABASE_URL")
    if mainDSN == "" || referenceDSN == "" { t.Skip("explicit current and independently restored PostgreSQL databases required") }
    ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second); defer cancel()
    observations := make([]LineageObservation, 0, 2)
    for _, dsn := range []string{mainDSN, referenceDSN} {
        connection, err := pgx.Connect(ctx, dsn); if err != nil { t.Fatal("database connection failed") }
        source := &lineageLiveReadOnlySource{connection: connection}
        value, err := ObserveLineage(ctx, source, DefaultLineageObservationLimits())
        cleanup, stop := context.WithTimeout(context.Background(), 2*time.Second); _ = connection.Close(cleanup); stop()
        if err != nil { t.Fatal(err) }
        if source.observedTransactions != 1 || !value.ExternalDependencyValidationRequired { t.Fatal("observer trust boundary changed") }
        internal := 0
        for _, object := range value.Objects {
            if object.Family == "triggers" && strings.HasPrefix(object.Identity, `["internal_fk",`) { internal++ }
        }
        if internal == 0 { t.Fatal("expected actual FK triggers in restored application schema") }
        observations = append(observations, value)
    }
    if observations[0].SemanticSHA256 != observations[1].SemanticSHA256 || observations[0].ExactLedgerSHA256 != observations[1].ExactLedgerSHA256 {
        t.Fatal("database-local catalog or exact ledger differs after independent restore")
    }
}
