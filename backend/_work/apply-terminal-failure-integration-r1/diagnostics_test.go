package main

import (
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"testing"
)

func TestClosedFailureDiagnostic(t *testing.T) {
	secret := "postgres://secret /private/path SELECT token"
	for _, e := range []error{errors.New(secret), &pgconn.PgError{Code: "23514", Message: secret, Detail: secret}} {
		got := string(safeFailureJSON("FROZEN_PLAN", e))
		if strings.Contains(got, secret) || strings.Contains(got, "postgres") || strings.Contains(got, "/private") {
			t.Fatal("unsafe diagnostic")
		}
	}
	got := safeFailure("forged arbitrary text", errors.New(secret))
	if got["phase"] != "COMPONENT" {
		t.Fatal("unclosed phase")
	}
}
func TestQueryDiagnosticClosed(t *testing.T) {
	if phaseForQuery("SELECT photo.apply_v4_frozen_plan($1,$2,$3)") != "FROZEN_PLAN" || phaseForQuery("arbitrary secret query") != "OTHER_DATABASE_CALL" {
		t.Fatal("query identity")
	}
}
