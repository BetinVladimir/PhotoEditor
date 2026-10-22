package main

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"testing"
)

func TestOperatorDiagnosticNeverEmitsMessageDetailQueryOrArbitraryIdentifiers(t *testing.T) {
	secret := "PRIVATE_SECRET_MARKER"
	e := &pgconn.PgError{Code: "42501", Message: secret, Detail: secret, Hint: secret, InternalQuery: secret, Where: secret, Routine: secret, ConstraintName: secret, SchemaName: secret, TableName: secret, ColumnName: secret}
	raw, _ := json.Marshal(diagnostic(e))
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), "Message") || strings.Contains(string(raw), "query") {
		t.Fatal("secret diagnostics leaked")
	}
	if diagnostic(e)["pg_code"] != "42501" {
		t.Fatal("SQLSTATE unavailable")
	}
	raw, _ = json.Marshal(diagnostic(errors.New(secret)))
	if strings.Contains(string(raw), secret) {
		t.Fatal("application error message leaked")
	}
}
func TestOperatorDiagnosticOnlyAllowsStaticRoutineConstraintAndSQLSTATE(t *testing.T) {
	e := &pgconn.PgError{Code: "23514", Routine: "exec_stmt_raise", ConstraintName: "backend_documents_purpose_check"}
	r := diagnostic(e)
	if r["pg_routine"] != "exec_stmt_raise" || r["pg_constraint"] != "backend_documents_purpose_check" {
		t.Fatal("trusted metadata lost")
	}
	e.Code = "PRIVATE_SECRET_MARKER"
	if _, ok := diagnostic(e)["pg_code"]; ok {
		t.Fatal("non-SQLSTATE leaked")
	}
}
