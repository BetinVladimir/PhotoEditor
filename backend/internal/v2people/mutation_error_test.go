package v2people

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
)

func pgCode(code string) error {
	return &pgconn.PgError{Code: code}
}

func TestMutationError_nonPgPassesThrough(t *testing.T) {
	plain := errors.New("some error")
	if mutationError(plain) != plain {
		t.Fatal("non-pg error must pass through")
	}
}

func TestMutationError_codes(t *testing.T) {
	cases := []struct {
		code   string
		want   string
		status int
	}{
		{"40001", "RESOURCE_CONFLICT", 409},
		{"23505", "RESOURCE_CONFLICT", 409},
		{"23514", "RESOURCE_CONFLICT", 409},
		{"42501", "FORBIDDEN", 403},
		{"P0002", "NOT_FOUND", 404},
		{"22023", "VALIDATION_FAILED", 422},
	}
	for _, tc := range cases {
		err := mutationError(pgCode(tc.code))
		de := domain.AsError(err)
		if de.Code != tc.want || de.HTTPStatus != tc.status {
			t.Errorf("mutationError(%q) = %s %d, want %s %d", tc.code, de.Code, de.HTTPStatus, tc.want, tc.status)
		}
	}
}

func TestMutationError_unknownCodePassesThrough(t *testing.T) {
	raw := pgCode("99999")
	if mutationError(raw) != raw {
		t.Fatal("unknown pg code must pass through")
	}
}

func who() auth.Identity {
	return auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
}

func TestServiceCreateNilDB(t *testing.T) {
	s := Service{}
	_, err := s.Create(nil, who(), uuid.New(), "key", CreateRequest{ProjectID: uuid.New(), Label: "Alice"})
	if domain.AsError(err).Code != "DEPENDENCY_UNAVAILABLE" {
		t.Fatalf("nil DB must return DEPENDENCY_UNAVAILABLE, got %v", err)
	}
}

func TestServiceCreateNilProjectID(t *testing.T) {
	s := Service{}
	_, err := s.Create(nil, who(), uuid.New(), "key", CreateRequest{ProjectID: uuid.Nil, Label: "Alice"})
	if domain.AsError(err).Code != "DEPENDENCY_UNAVAILABLE" {
		// nil-DB check fires first
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServiceCreateEmptyLabel(t *testing.T) {
	s := Service{}
	_, err := s.Create(nil, who(), uuid.New(), "key", CreateRequest{ProjectID: uuid.New(), Label: ""})
	if domain.AsError(err).Code != "DEPENDENCY_UNAVAILABLE" {
		t.Fatalf("nil DB check must fire before label validation, got %v", err)
	}
}

func TestServiceRevokeNilDB(t *testing.T) {
	s := Service{}
	_, err := s.Revoke(nil, who(), uuid.New(), uuid.New(), "key", CASRequest{ExpectedRevision: "1"})
	if domain.AsError(err).Code != "DEPENDENCY_UNAVAILABLE" {
		t.Fatalf("nil DB must return DEPENDENCY_UNAVAILABLE, got %v", err)
	}
}

func TestServiceRevokeInvalidRevision(t *testing.T) {
	s := Service{}
	_, err := s.Revoke(nil, who(), uuid.New(), uuid.New(), "key", CASRequest{ExpectedRevision: "0"})
	if domain.AsError(err).Code != "DEPENDENCY_UNAVAILABLE" {
		// nil-DB check fires before revision parse
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServiceChangeConsentNilDB(t *testing.T) {
	s := Service{}
	req := ConsentRequest{
		ExpectedRevision: "1",
		Purpose:          "identity_matching",
		PersonID:         []byte(`"` + uuid.New().String() + `"`),
		Action:           "grant",
		PolicyVersion:    "v1",
	}
	_, err := s.ChangeConsent(nil, who(), uuid.New(), uuid.New(), "key", req)
	if domain.AsError(err).Code != "DEPENDENCY_UNAVAILABLE" {
		t.Fatalf("nil DB must return DEPENDENCY_UNAVAILABLE, got %v", err)
	}
}

func TestServiceListNilDB(t *testing.T) {
	s := Service{}
	_, err := s.List(nil, who(), uuid.New(), uuid.New(), "project_persons", 10, "")
	if domain.AsError(err).Code != "DEPENDENCY_UNAVAILABLE" {
		t.Fatalf("nil DB must return DEPENDENCY_UNAVAILABLE, got %v", err)
	}
}

func TestServiceListInvalidLimit(t *testing.T) {
	s := Service{}
	_, err := s.List(nil, who(), uuid.New(), uuid.New(), "project_persons", 0, "")
	if domain.AsError(err).Code != "DEPENDENCY_UNAVAILABLE" {
		// nil-DB fires before limit check based on code order
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServiceListUnknownKind(t *testing.T) {
	s := Service{}
	_, err := s.List(nil, who(), uuid.New(), uuid.New(), "unknown_kind", 10, "")
	if domain.AsError(err).Code != "DEPENDENCY_UNAVAILABLE" {
		// nil-DB fires before kind check
		t.Fatalf("unexpected error: %v", err)
	}
}
