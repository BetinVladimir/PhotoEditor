package v2learning

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

type ActivateProfileRequest struct {
	ExpectedRevision string    `json:"expected_revision"`
	VersionID        uuid.UUID `json:"version_id"`
	Reason           string    `json:"reason"`
}

type LearningSafetyReviewRequest struct {
	ExpectedRevision string `json:"expected_revision"`
	EvaluationSHA    string `json:"evaluation_sha"`
	Accept           bool   `json:"accept"`
	Reason           string `json:"reason"`
}

func exactFields(body []byte, names ...string) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return fmt.Errorf("expected object")
	}
	fields := make(map[string]json.RawMessage, len(names))
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := keyToken.(string)
		if !ok {
			return fmt.Errorf("expected field name")
		}
		if _, exists := fields[key]; exists {
			return fmt.Errorf("duplicate %s", key)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		fields[key] = value
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return fmt.Errorf("expected object end")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("unexpected trailing input")
	}
	if len(fields) != len(names) {
		return fmt.Errorf("expected %d fields", len(names))
	}
	for _, name := range names {
		if _, ok := fields[name]; !ok {
			return fmt.Errorf("missing %s", name)
		}
	}
	return nil
}

func (r *ActivateProfileRequest) UnmarshalJSON(body []byte) error {
	if err := exactFields(body, "expected_revision", "version_id", "reason"); err != nil {
		return err
	}
	type wire ActivateProfileRequest
	var decoded wire
	if err := json.Unmarshal(body, &decoded); err != nil {
		return err
	}
	*r = ActivateProfileRequest(decoded)
	return nil
}

func (r *LearningSafetyReviewRequest) UnmarshalJSON(body []byte) error {
	if err := exactFields(body, "expected_revision", "evaluation_sha", "accept", "reason"); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return err
	}
	if string(fields["accept"]) != "true" && string(fields["accept"]) != "false" {
		return fmt.Errorf("accept must be boolean")
	}
	type wire LearningSafetyReviewRequest
	var decoded wire
	if err := json.Unmarshal(body, &decoded); err != nil {
		return err
	}
	*r = LearningSafetyReviewRequest(decoded)
	return nil
}

func positiveRevision(value string) (int64, bool) {
	if value == "" || value[0] == '0' || len(value) > 19 {
		return 0, false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	return n, err == nil && n > 0 && n <= 9007199254740991
}

func profileMutationAuthorized(ctx context.Context, tx pgx.Tx, who auth.Identity,
	tenantID, profileID uuid.UUID, role domain.Role) error {
	var projectID, creator uuid.UUID
	err := tx.QueryRow(ctx, `SELECT project_id,created_by FROM photo.learning_profiles
	 WHERE tenant_id=$1 AND id=$2`, tenantID, profileID).Scan(&projectID, &creator)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		return err
	}
	if _, err = project.Authorize(ctx, tx, tenantID, projectID, who.UserID,
		role, domain.Viewer, false); err != nil {
		return err
	}
	if role == domain.Owner {
		return nil
	}
	if creator != who.UserID || !role.Allows(domain.Editor) {
		return domain.NewError("FORBIDDEN", 403)
	}
	_, err = project.Authorize(ctx, tx, tenantID, projectID, who.UserID,
		role, domain.Editor, false)
	return err
}

func (s ProfileService) ActivateProfile(ctx context.Context, who auth.Identity,
	tenantID, profileID uuid.UUID, key string,
	req ActivateProfileRequest) (idempotency.Response, error) {
	var out idempotency.Response
	if s.DB == nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	revision, valid := positiveRevision(req.ExpectedRevision)
	if profileID == uuid.Nil || req.VersionID == uuid.Nil || !valid ||
		(req.Reason != "promote" && req.Reason != "rollback") {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	if !idempotency.ValidKey(key) {
		return out, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor,
		func(tx pgx.Tx, role domain.Role) error {
			if e := profileMutationAuthorized(ctx, tx, who, tenantID, profileID, role); e != nil {
				return e
			}
			var e error
			out, e = idempotency.Use(ctx, tx, idempotency.Scope{
				Tenant: tenantID, Principal: who.UserID, PrincipalType: "user",
				Route: "V23.ActivateProfile", Key: key,
			}, struct {
				ProfileID uuid.UUID              `json:"profile_id"`
				Request   ActivateProfileRequest `json:"request"`
			}{profileID, req}, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
				var activated bool
				if e := tx.QueryRow(ctx, `SELECT photo.activate_v23_learning_profile(
				 $1,$2,$3,$4,$5,$6,$7)`, tenantID, profileID, revision,
					req.VersionID, req.Reason, who.UserID, who.SessionID).
					Scan(&activated); e != nil {
					return idempotency.Response{}, correctionError(e)
				}
				if !activated {
					return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
				}
				profile, e := profileGet(ctx, tx, who, tenantID, profileID, role)
				if e != nil {
					return idempotency.Response{}, e
				}
				body, e := json.Marshal(profile)
				return idempotency.Response{Status: 200, Body: body,
					ResourceID: &profileID}, e
			})
			return e
		})
	return out, correctionError(err)
}

func (s ProfileService) ReviewLearningSafety(ctx context.Context, who auth.Identity,
	tenantID, versionID uuid.UUID, key string,
	req LearningSafetyReviewRequest) (idempotency.Response, error) {
	var out idempotency.Response
	if s.DB == nil {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	revision, valid := positiveRevision(req.ExpectedRevision)
	if versionID == uuid.Nil || !valid || !utf8.ValidString(req.Reason) ||
		len([]rune(req.Reason)) < 1 || len([]rune(req.Reason)) > 256 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	if _, e := domain.ParseDigest(req.EvaluationSHA); e != nil {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	if !idempotency.ValidKey(key) {
		return out, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor,
		func(tx pgx.Tx, role domain.Role) error {
			var profileID uuid.UUID
			e := tx.QueryRow(ctx, `SELECT profile_id FROM photo.learning_versions
			 WHERE tenant_id=$1 AND id=$2`, tenantID, versionID).Scan(&profileID)
			if errors.Is(e, pgx.ErrNoRows) {
				return domain.NewError("NOT_FOUND", 404)
			}
			if e != nil {
				return e
			}
			if e = profileMutationAuthorized(ctx, tx, who, tenantID, profileID, role); e != nil {
				return e
			}
			out, e = idempotency.Use(ctx, tx, idempotency.Scope{
				Tenant: tenantID, Principal: who.UserID, PrincipalType: "user",
				Route: "V23.ReviewLearningSafety", Key: key,
			}, struct {
				VersionID uuid.UUID                   `json:"version_id"`
				Request   LearningSafetyReviewRequest `json:"request"`
			}{versionID, req}, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
				var reviewID *uuid.UUID
				if e := tx.QueryRow(ctx, `SELECT photo.review_v23_learning_safety(
				 $1,$2,$3,$4,$5,$6,$7,$8)`, tenantID, versionID, revision,
					req.EvaluationSHA, req.Accept, req.Reason, who.UserID,
					who.SessionID).Scan(&reviewID); e != nil {
					return idempotency.Response{}, correctionError(e)
				}
				if reviewID == nil {
					return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
				}
				version, e := learningVersionGet(ctx, tx, tenantID, versionID)
				if e != nil {
					return idempotency.Response{}, e
				}
				body, e := json.Marshal(version)
				return idempotency.Response{Status: 200, Body: body,
					ResourceID: &versionID}, e
			})
			return e
		})
	return out, correctionError(err)
}
