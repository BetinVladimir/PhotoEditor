// Package artifactjson publishes bounded backend-authored contract documents.
package artifactjson

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
)

type Scope struct {
	Tenant, Run, Owner uuid.UUID
	Purpose            string
}
type Ref struct {
	ID     uuid.UUID `json:"artifact_id"`
	SHA256 string    `json:"sha256"`
}
type Reservation struct {
	ArtifactID, Token uuid.UUID
	Ready             *Ref
}

// Repository must durably reserve a stable artifact ID for the scope before
// returning, reject changed content for that scope, and fence Complete with
// Token. It must verify that the run/owner belongs to the authorized tenant.
type Repository interface {
	Reserve(context.Context, Scope, string, int64) (Reservation, error)
	Complete(context.Context, Scope, Reservation, objectstore.Object) (Ref, error)
}
type Objects interface {
	PutImmutable(context.Context, string, io.Reader, int64, string, string) (objectstore.Object, error)
	RecoverImmutable(context.Context, string, int64, string, string) (objectstore.Object, error)
}
type Publisher struct {
	Repository Repository
	Objects    Objects
}

func DocumentKey(s Scope, id uuid.UUID) (string, error) {
	if s.Tenant == uuid.Nil || s.Run == uuid.Nil || s.Owner == uuid.Nil || id == uuid.Nil ||
		(s.Purpose != "task_params" && s.Purpose != "review_selection" &&
			s.Purpose != "v2_session_plan_params" && s.Purpose != "v23_evaluate_params" &&
			s.Purpose != "v23_training_manifest" && !v24CapturePurpose(s.Purpose) && !v25LensPurpose(s.Purpose) &&
			s.Purpose != "v23_training_snapshot" && s.Purpose != "v23_fit_params" &&
			s.Purpose != "v23_zero_profile" &&
			s.Purpose != "v23_frozen_base" &&
			s.Purpose != "v23_frozen_approved" &&
			!v2InputPurpose(s.Purpose) && !v22AnalysisPurpose(s.Purpose)) {
		return "", objectstore.ErrInvalid
	}
	return objectstore.DocumentKey(s.Tenant, s.Run, id)
}

func v22AnalysisPurpose(purpose string) bool {
	if purpose == "v2_analysis_input_snapshot" || purpose == "v2_analysis_context" || purpose == "v2_analysis_consents" {
		return true
	}
	const prefix = "v2_analysis_describe_params_"
	if !strings.HasPrefix(purpose, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(purpose, prefix)
	ordinal, err := strconv.Atoi(suffix)
	return err == nil && ordinal >= 0 && ordinal < 40 && strconv.Itoa(ordinal) == suffix
}

func maxDocumentBytes(purpose string) int {
	if purpose == "v2_analysis_input_snapshot" || purpose == "v2_analysis_consents" || purpose == "v23_training_manifest" || purpose == "v24_capture_context" || purpose == "v24_capture_consents" {
		return 8 * 1048576
	}
	return 1048576
}

func v2InputPurpose(purpose string) bool {
	if purpose == "v2_input_manifest" {
		return true
	}
	const prefix = "v2_input_chunk_"
	if !strings.HasPrefix(purpose, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(purpose, prefix)
	ordinal, err := strconv.Atoi(suffix)
	return err == nil && ordinal >= 0 && ordinal <= 1953 && strconv.Itoa(ordinal) == suffix
}

// Publish never performs object IO inside a DB transaction. Reserve commits
// first, PUT/recovery pins a verified version, then Complete atomically records
// ready state. Cancellation/unknown completion retains the same reservation
// for retry; neither overwriting nor deleting shared keys is permitted.
func (p Publisher) Publish(ctx context.Context, scope Scope, raw []byte) (Ref, error) {
	if p.Repository == nil || p.Objects == nil || len(raw) > maxDocumentBytes(scope.Purpose) {
		return Ref{}, objectstore.ErrInvalid
	}
	if _, err := DocumentKey(scope, scope.Owner); err != nil {
		return Ref{}, err
	}
	if err := ctx.Err(); err != nil {
		return Ref{}, err
	}
	body, err := canonical.Bytes(raw)
	if err != nil || len(body) == 0 || body[0] != '{' || len(body) > maxDocumentBytes(scope.Purpose) {
		return Ref{}, objectstore.ErrInvalid
	}
	digest := sha256.Sum256(body)
	sha := hex.EncodeToString(digest[:])
	size := int64(len(body))
	reservation, err := p.Repository.Reserve(ctx, scope, sha, size)
	if err != nil {
		return Ref{}, err
	}
	key, err := DocumentKey(scope, reservation.ArtifactID)
	if err != nil {
		return Ref{}, err
	}
	expected := Ref{reservation.ArtifactID, sha}
	if reservation.Ready != nil {
		if *reservation.Ready != expected {
			return Ref{}, objectstore.ErrIntegrity
		}
		return expected, nil
	}
	if reservation.Token == uuid.Nil {
		return Ref{}, objectstore.ErrInvalid
	}
	object, err := p.Objects.PutImmutable(ctx, key, bytes.NewReader(body), size, sha, "application/json")
	if errors.Is(err, objectstore.ErrExists) || errors.Is(err, objectstore.ErrUnavailable) {
		if ctx.Err() != nil {
			return Ref{}, ctx.Err()
		}
		object, err = p.Objects.RecoverImmutable(ctx, key, size, sha, "application/json")
	}
	if err != nil {
		return Ref{}, err
	}
	if object.Key != key || object.SHA256 != sha || object.Bytes != size || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" {
		return Ref{}, objectstore.ErrIntegrity
	}
	result, err := p.Repository.Complete(ctx, scope, reservation, object)
	if err != nil {
		return Ref{}, err
	}
	if result != expected {
		return Ref{}, objectstore.ErrIntegrity
	}
	return result, nil
}

func v24CapturePurpose(purpose string) bool {
	switch purpose {
	case "v24_capture_prepare_params", "v24_capture_shots", "v24_capture_consents", "v24_capture_context", "v24_capture_assess_params":
		return true
	default:
		return false
	}
}

func v25LensPurpose(purpose string) bool {
	return purpose == "v25_lens_params" || purpose == "v25_lens_original_binding"
}
