package domain

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

type TenantID uuid.UUID
type UserID uuid.UUID
type RunID uuid.UUID
type TaskID uuid.UUID
type ArtifactID uuid.UUID
type Revision int64
type Digest string

type Error struct {
	Code       string         `json:"code"`
	MessageKey string         `json:"message_key"`
	Args       map[string]any `json:"args"`
	HTTPStatus int            `json:"-"`
	Retryable  bool           `json:"-"`
	Cause      error          `json:"-"`
}

func (e *Error) Error() string { return e.Code }
func (e *Error) Unwrap() error { return e.Cause }
func NewError(code string, status int) *Error {
	return &Error{Code: code, MessageKey: "errors." + code, Args: map[string]any{}, HTTPStatus: status}
}
func AsError(err error) *Error {
	var de *Error
	if errors.As(err, &de) {
		return de
	}
	result := NewError("INTERNAL_ERROR", 500)
	result.Cause = err
	return result
}

var hashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

func ParseDigest(value string) (Digest, error) {
	if !hashRE.MatchString(value) {
		return "", fmt.Errorf("invalid SHA-256")
	}
	return Digest(value), nil
}

type Role string

const (
	Owner  Role = "owner"
	Admin  Role = "admin"
	Editor Role = "editor"
	Viewer Role = "viewer"
)

func (r Role) Allows(required Role) bool {
	rank := map[Role]int{Viewer: 1, Editor: 2, Admin: 3, Owner: 4}
	actual, ok := rank[r]
	needed, valid := rank[required]
	return ok && valid && actual >= needed
}

type Principal struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
	TenantID  uuid.UUID
	Role      Role
}
type Clock interface{ Now() time.Time }
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }
