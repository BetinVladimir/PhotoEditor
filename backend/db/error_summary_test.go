package migrations

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
)

type noFormatError struct{ cause error }

func (e noFormatError) Error() string { panic("outer SQL error must not be formatted") }
func (e noFormatError) Unwrap() error { return e.cause }
func TestSummarizeMigrationError(t *testing.T) {
	pg := &pgconn.PgError{Code: "55000", Message: "NATIVE_REPLAY_NEW_ABI_ACL", Detail: "PRIVATE_SQL_SENTINEL", InternalQuery: "PRIVATE_SQL_SENTINEL"}
	outer := fmt.Errorf("SQL %s: %w", strings.Repeat("PRIVATE_SQL_SENTINEL", 1600000), pg)
	result := SummarizeError(outer)
	if len(result.Error()) > 4096 || strings.Contains(result.Error(), "PRIVATE_SQL_SENTINEL") || !strings.Contains(result.Error(), "55000") || !strings.Contains(result.Error(), pg.Message) {
		t.Fatal("unsafe or missing summary")
	}
	var actual *pgconn.PgError
	if !errors.As(result, &actual) || actual != pg || !errors.Is(result, outer) {
		t.Fatal("lost error chain")
	}
	if got := SummarizeError(noFormatError{pg}); !strings.Contains(got.Error(), pg.Message) {
		t.Fatal("missing PostgreSQL message")
	}
}
func TestSummarizeMigrationErrorBounds(t *testing.T) {
	for _, err := range []error{errors.New(strings.Repeat("secret", 100000)), noFormatError{}, errors.Join(errors.New("secret"), &pgconn.PgError{Code: "55000", Message: strings.Repeat("写真", 4000)})} {
		s := SummarizeError(err).Error()
		if len(s) > 4096 || !utf8.ValidString(s) || strings.Contains(s, "secret") {
			t.Fatal("unbounded or unsafe fallback")
		}
	}
	if SummarizeError(nil) != nil {
		t.Fatal("nil changed")
	}
}
