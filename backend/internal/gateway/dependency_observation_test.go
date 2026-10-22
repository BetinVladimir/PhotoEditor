package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"log/slog"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"testing"
)

func TestDependencyObservationDoesNotExposePrivateErrorContent(t *testing.T) {
	var output bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(old)
	const private = "private-password SQL Bearer token https://storage/private-key"
	dependencyObservation("download_input", &pgconn.PgError{Code: "42501", Message: private, Detail: private, Where: private})
	dependencyObservation("download_storage", errors.New(private))
	dependencyObservation("download_storage", objectstore.ErrIntegrity)
	if strings.Contains(output.String(), private) || strings.Contains(output.String(), "private-key") {
		t.Fatal("private error content leaked")
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatal("unexpected observations")
	}
	var row map[string]any
	if json.Unmarshal([]byte(lines[0]), &row) != nil || row["sqlstate"] != "42501" || row["stage"] != "download_input" {
		t.Fatal("missing safe diagnostics")
	}
	if json.Unmarshal([]byte(lines[2]), &row) != nil || row["category"] != "object_integrity" {
		t.Fatal("missing integrity category")
	}
	before := output.Len()
	dependencyObservation("download_input", nil)
	dependencyObservation("download_input", ErrCapability)
	dependencyObservation("download_input", objectstore.ErrMissing)
	if output.Len() != before {
		t.Fatal("expected refusals logged as dependencies")
	}
	output.Reset()
	dependencyObservation("download_input", &pgconn.PgError{Code: private, Message: private})
	if strings.Contains(output.String(), private) || strings.Contains(output.String(), "sqlstate") {
		t.Fatal("invalid SQLSTATE leaked")
	}
}
