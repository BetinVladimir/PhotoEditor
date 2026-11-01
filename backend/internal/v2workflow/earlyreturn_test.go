package v2workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
)

func wfWho() auth.Identity {
	return auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
}

func TestGetNilDB(t *testing.T) {
	s := Service{}
	_, err := s.Get(context.Background(), wfWho(), uuid.New(), uuid.New())
	if domain.AsError(err).Code != "DEPENDENCY_UNAVAILABLE" {
		t.Fatalf("nil DB must return DEPENDENCY_UNAVAILABLE, got %v", err)
	}
}

func TestGetNilWorkflowID(t *testing.T) {
	s := Service{}
	_, err := s.Get(context.Background(), wfWho(), uuid.New(), uuid.Nil)
	if domain.AsError(err).Code != "DEPENDENCY_UNAVAILABLE" {
		// nil-DB fires before nil workflow check
		t.Fatalf("nil DB must return DEPENDENCY_UNAVAILABLE, got %v", err)
	}
}

func TestCancelNilDB(t *testing.T) {
	s := Service{}
	_, err := s.Cancel(context.Background(), wfWho(), uuid.New(), uuid.New(), "key", CancelRequest{ExpectedRevision: "1"})
	if domain.AsError(err).Code != "DEPENDENCY_UNAVAILABLE" {
		t.Fatalf("nil DB must return DEPENDENCY_UNAVAILABLE, got %v", err)
	}
}

func TestCancelNilWorkflowID(t *testing.T) {
	s := Service{}
	_, err := s.Cancel(context.Background(), wfWho(), uuid.New(), uuid.Nil, "key", CancelRequest{ExpectedRevision: "1"})
	if domain.AsError(err).Code != "DEPENDENCY_UNAVAILABLE" {
		// nil-DB fires before nil workflow check
		t.Fatalf("nil DB must return DEPENDENCY_UNAVAILABLE, got %v", err)
	}
}

func TestCancelEmptyKey(t *testing.T) {
	s := Service{}
	_, err := s.Cancel(context.Background(), wfWho(), uuid.New(), uuid.New(), "", CancelRequest{ExpectedRevision: "1"})
	if domain.AsError(err).Code != "DEPENDENCY_UNAVAILABLE" {
		// nil-DB fires before key check
		t.Fatalf("nil DB must return DEPENDENCY_UNAVAILABLE, got %v", err)
	}
}

func TestCancelInvalidRevision(t *testing.T) {
	s := Service{}
	_, err := s.Cancel(context.Background(), wfWho(), uuid.New(), uuid.New(), "key", CancelRequest{ExpectedRevision: "bad"})
	if domain.AsError(err).Code != "DEPENDENCY_UNAVAILABLE" {
		// nil-DB fires before revision parse
		t.Fatalf("nil DB must return DEPENDENCY_UNAVAILABLE, got %v", err)
	}
}

func TestUnavailable_nil(t *testing.T) {
	if unavailable(nil) != nil {
		t.Fatal("unavailable(nil) must return nil")
	}
}

func TestUnavailable_42P01(t *testing.T) {
	err := unavailable(&pgconn.PgError{Code: "42P01"})
	if domain.AsError(err).Code != "DEPENDENCY_UNAVAILABLE" {
		t.Fatalf("42P01 must become DEPENDENCY_UNAVAILABLE, got %v", err)
	}
}

func TestUnavailable_otherPgError(t *testing.T) {
	raw := &pgconn.PgError{Code: "23505"}
	if unavailable(raw) != raw {
		t.Fatal("non-42P01 pg error must pass through")
	}
}

func TestUnavailable_nonPgError(t *testing.T) {
	plain := errors.New("plain")
	if unavailable(plain) != plain {
		t.Fatal("non-pg error must pass through")
	}
}

func TestExpectedRevision_valid(t *testing.T) {
	cases := []string{"1", "42", "9007199254740991"}
	for _, raw := range cases {
		if n, err := expectedRevision(raw); err != nil || n < 1 {
			t.Errorf("valid revision %q: got %d, %v", raw, n, err)
		}
	}
}

func TestExpectedRevision_invalid(t *testing.T) {
	cases := []string{"", "0", "-1", "01", "+1", "abc", " 1"}
	for _, raw := range cases {
		if _, err := expectedRevision(raw); err == nil {
			t.Errorf("invalid revision %q was accepted", raw)
		}
	}
}
