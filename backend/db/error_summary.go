package migrations

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
)

// migrationError keeps the original error chain without expanding Goose's SQL.
type migrationError struct {
	cause   error
	summary string
}

func (e *migrationError) Error() string { return e.summary }
func (e *migrationError) Unwrap() error { return e.cause }

// SummarizeError makes migration failures safe for bounded operational logs.
// Query, detail, hint, and context fields are deliberately excluded.
func SummarizeError(err error) error {
	if err == nil {
		return nil
	}
	summary := "database migration failed"
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg != nil {
		code := pg.Code
		if len(code) != 5 || strings.IndexFunc(code, func(r rune) bool { return !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z') }) >= 0 {
			code = "unknown"
		}
		message := pg.Message
		const maxMessage = 2048
		if len(message) > maxMessage {
			message = message[:maxMessage]
			for !utf8.ValidString(message) && len(message) > 0 {
				message = message[:len(message)-1]
			}
			message += "…"
		}
		summary += " (SQLSTATE " + code + "): " + message
	}
	return &migrationError{cause: err, summary: summary}
}
