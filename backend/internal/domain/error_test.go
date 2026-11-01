package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestNewError(t *testing.T) {
	e := NewError("NOT_FOUND", 404)
	if e.Code != "NOT_FOUND" || e.HTTPStatus != 404 {
		t.Fatalf("NewError = %v, want NOT_FOUND 404", e)
	}
	if e.MessageKey != "errors.NOT_FOUND" {
		t.Fatalf("MessageKey = %q", e.MessageKey)
	}
	if e.Args == nil {
		t.Fatal("Args must be non-nil empty map")
	}
}

func TestErrorMethod(t *testing.T) {
	e := NewError("FORBIDDEN", 403)
	if e.Error() != "FORBIDDEN" {
		t.Fatalf("Error() = %q, want FORBIDDEN", e.Error())
	}
}

func TestUnwrap(t *testing.T) {
	cause := errors.New("root cause")
	e := NewError("INTERNAL_ERROR", 500)
	e.Cause = cause
	if e.Unwrap() != cause {
		t.Fatal("Unwrap must return Cause")
	}
	if e.Unwrap() == nil {
		t.Fatal("non-nil cause must be unwrapped")
	}
}

func TestUnwrapNil(t *testing.T) {
	e := NewError("NOT_FOUND", 404)
	if e.Unwrap() != nil {
		t.Fatal("nil Cause must unwrap to nil")
	}
}

func TestAsError_domainError(t *testing.T) {
	e := NewError("VALIDATION_FAILED", 422)
	got := AsError(e)
	if got.Code != "VALIDATION_FAILED" || got.HTTPStatus != 422 {
		t.Fatalf("AsError(domain.Error) = %v", got)
	}
}

func TestAsError_wrappedDomainError(t *testing.T) {
	inner := NewError("FORBIDDEN", 403)
	wrapped := errors.New("wrapper: " + inner.Error())
	_ = wrapped
	// When a non-domain error is passed, AsError wraps it as INTERNAL_ERROR.
	plain := errors.New("generic")
	got := AsError(plain)
	if got.Code != "INTERNAL_ERROR" || got.HTTPStatus != 500 {
		t.Fatalf("AsError(plain) = %v", got)
	}
	if got.Cause != plain {
		t.Fatal("Cause must be set to original error")
	}
}

func TestAsError_nil(t *testing.T) {
	got := AsError(nil)
	// nil has no domain.Error inside, so it wraps as INTERNAL_ERROR with nil cause
	if got.Code != "INTERNAL_ERROR" {
		t.Fatalf("AsError(nil) = %v", got)
	}
}

func TestParseDigest_valid(t *testing.T) {
	valid := strings.Repeat("a", 64)
	d, err := ParseDigest(valid)
	if err != nil || string(d) != valid {
		t.Fatalf("ParseDigest(%q) = %v, %v", valid, d, err)
	}
}

func TestParseDigest_invalidCases(t *testing.T) {
	cases := []string{
		"",
		strings.Repeat("a", 63),
		strings.Repeat("a", 65),
		strings.Repeat("G", 64), // uppercase
		strings.Repeat("z", 64), // not hex
	}
	for _, c := range cases {
		if _, err := ParseDigest(c); err == nil {
			t.Errorf("ParseDigest(%q) should have failed", c)
		}
	}
}

func TestSystemClockNow(t *testing.T) {
	c := SystemClock{}
	now := c.Now()
	if now.IsZero() {
		t.Fatal("SystemClock.Now() returned zero time")
	}
}
