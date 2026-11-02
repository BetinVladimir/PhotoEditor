package migrations

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestSummarizeMigrationErrorInvalidCode covers the code = "unknown" branch
// when the PgError.Code has non-standard characters or wrong length.
func TestSummarizeMigrationErrorInvalidCode(t *testing.T) {
	pg := &pgconn.PgError{Code: "abc", Message: "short code test"} // len != 5
	result := SummarizeError(pg)
	if !strings.Contains(result.Error(), "unknown") {
		t.Fatalf("short code must be sanitized to 'unknown', got: %s", result.Error())
	}
	pg2 := &pgconn.PgError{Code: "55a00", Message: "lowercase in code"} // 'a' < 'A'
	result2 := SummarizeError(pg2)
	if !strings.Contains(result2.Error(), "unknown") {
		t.Fatalf("code with lowercase must be sanitized to 'unknown', got: %s", result2.Error())
	}
}

// TestValidateInstalledSecurityNilDB covers the nil-database guard path.
func TestValidateInstalledSecurityNilDB(t *testing.T) {
	if err := ValidateInstalledSecurity(context.Background(), (*sql.DB)(nil)); err == nil {
		t.Fatal("nil database must return an error")
	}
}

// TestClassifyLineageObservationMissingFamilyCountLength covers the
// len(value.FamilyCounts) != len(lineageObservationQueries) guard.
func TestClassifyLineageObservationMissingFamilyCountLength(t *testing.T) {
	value := observeLineageFixture(t)
	changed := value
	// Add an extra unknown family to FamilyCounts so length mismatches.
	changed.FamilyCounts = make(map[string]int, len(value.FamilyCounts)+1)
	for k, v := range value.FamilyCounts {
		changed.FamilyCounts[k] = v
	}
	changed.FamilyCounts["extra_unknown_family"] = 1
	allowed := []LineageSnapshotAuthority{{Identity: "snap", SemanticSHA256: value.SemanticSHA256}}
	if _, err := ClassifyLineageObservation(changed, allowed); err == nil {
		t.Fatal("extra family in FamilyCounts must be rejected")
	}
}

// TestClassifyLineageObservationWrongLedgerSchema covers the LedgerSchema guard.
func TestClassifyLineageObservationWrongLedgerSchema(t *testing.T) {
	value := observeLineageFixture(t)
	changed := value
	changed.LedgerSchema = "admin"
	allowed := []LineageSnapshotAuthority{{Identity: "snap", SemanticSHA256: value.SemanticSHA256}}
	if _, err := ClassifyLineageObservation(changed, allowed); err == nil {
		t.Fatal("unknown LedgerSchema must be rejected")
	}
}

// TestClassifyLineageObservationWrongServerVersion covers the ServerVersion guard.
func TestClassifyLineageObservationWrongServerVersion(t *testing.T) {
	value := observeLineageFixture(t)
	changed := value
	changed.ServerVersion = "170004"
	allowed := []LineageSnapshotAuthority{{Identity: "snap", SemanticSHA256: value.SemanticSHA256}}
	if _, err := ClassifyLineageObservation(changed, allowed); err == nil {
		t.Fatal("non-18 ServerVersion must be rejected")
	}
}

// TestClassifyLineageObservationNonDigitLedgerVersion covers the digit-check
// inner loop in the ledger step validation.
func TestClassifyLineageObservationNonDigitLedgerVersion(t *testing.T) {
	value := observeLineageFixture(t)
	changed := value
	changed.Ledger = append([]LineageLedgerStep{}, value.Ledger...)
	changed.Ledger[0].Version = "00a01"
	allowed := []LineageSnapshotAuthority{{Identity: "snap", SemanticSHA256: value.SemanticSHA256}}
	if _, err := ClassifyLineageObservation(changed, allowed); err == nil {
		t.Fatal("non-digit ledger version character must be rejected")
	}
}

// TestClassifyLineageObservationOversizedAuthorityIdentity covers the
// len(entry.Identity) > 200 guard in the authority loop.
func TestClassifyLineageObservationOversizedAuthorityIdentity(t *testing.T) {
	value := observeLineageFixture(t)
	allowed := []LineageSnapshotAuthority{{
		Identity:      strings.Repeat("x", 201),
		SemanticSHA256: value.SemanticSHA256,
	}}
	if _, err := ClassifyLineageObservation(value, allowed); err == nil {
		t.Fatal("oversized authority identity must be rejected")
	}
}
