package auth

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/domain"
	"strconv"
	"time"
)

type User struct {
	ID       uuid.UUID `json:"id"`
	Email    string    `json:"email"`
	Locale   string    `json:"locale"`
	Theme    string    `json:"theme"`
	Timezone string    `json:"timezone"`
	Revision string    `json:"revision"`
}
type Preferences struct {
	Locale   *string `json:"locale,omitempty"`
	Theme    *string `json:"theme,omitempty"`
	Timezone *string `json:"timezone,omitempty"`
}

func (s *Service) GetUser(ctx context.Context, user uuid.UUID) (User, error) {
	var out User
	var rev int64
	err := s.DB.QueryRow(ctx, "SELECT id,email_normalized,locale,theme,timezone,revision FROM photo.users WHERE id=$1 AND status='active'", user).Scan(&out.ID, &out.Email, &out.Locale, &out.Theme, &out.Timezone, &rev)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, denied()
	}
	out.Revision = strconv.FormatInt(rev, 10)
	return out, err
}
func (s *Service) UpdateUser(ctx context.Context, user uuid.UUID, expected int64, p Preferences) (User, error) {
	var out User
	if expected < 1 || p.Locale != nil && !localeOK(*p.Locale) || p.Theme != nil && *p.Theme != "light" && *p.Theme != "dark" && *p.Theme != "system" {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	if p.Timezone != nil {
		if *p.Timezone == "" || *p.Timezone == "Local" {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		if _, err := time.LoadLocation(*p.Timezone); err != nil {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	var revision int64
	err := s.DB.QueryRow(ctx, `UPDATE photo.users SET locale=COALESCE($3,locale),theme=COALESCE($4,theme),timezone=COALESCE($5,timezone),revision=revision+1 WHERE id=$1 AND revision=$2 AND status='active' RETURNING id,email_normalized,locale,theme,timezone,revision`, user, expected, p.Locale, p.Theme, p.Timezone).Scan(&out.ID, &out.Email, &out.Locale, &out.Theme, &out.Timezone, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, domain.NewError("REVISION_CONFLICT", 409)
	}
	out.Revision = strconv.FormatInt(revision, 10)
	return out, err
}
