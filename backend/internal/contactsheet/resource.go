package contactsheet

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
)

//go:embed NotoSans-Regular.ttf OFL-1.1.txt
var bundled embed.FS

const FontName = "contact_sheet_font_v1"

type ImmutableStore interface {
	PutImmutable(context.Context, string, io.Reader, int64, string, string) (objectstore.Object, error)
	RecoverImmutable(context.Context, string, int64, string, string) (objectstore.Object, error)
}

type ResourceManager struct {
	DB      *pgxpool.Pool
	Objects ImmutableStore
	Bucket  string
}

type resourceReservation struct {
	ID, Token uuid.UUID
	Ready     bool
}

func fontBytes() ([]byte, string, error) {
	raw, e := bundled.ReadFile("NotoSans-Regular.ttf")
	if e != nil {
		return nil, "", e
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}
func resourceKey(tenant, id uuid.UUID) string {
	return fmt.Sprintf("tenants/%s/resources/%s", tenant, id)
}

func (m ResourceManager) EnsureFont(ctx context.Context, who auth.Identity, tenantID uuid.UUID) (artifactjson.Ref, error) {
	if m.DB == nil || m.Objects == nil || tenantID == uuid.Nil || m.Bucket == "" {
		return artifactjson.Ref{}, errors.New("contact sheet resources unavailable")
	}
	raw, digest, e := fontBytes()
	if e != nil {
		return artifactjson.Ref{}, e
	}
	var r resourceReservation
	var outcome string
	e = tenant.WithTenant(ctx, m.DB, who, tenantID, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		return tx.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_contact_sheet_font($1,$2,$3,$4)`, tenantID, digest, len(raw), m.Bucket).Scan(&outcome, &r.ID, &r.Token, &r.Ready)
	})
	if e != nil {
		return artifactjson.Ref{}, e
	}
	if outcome == "quota" {
		return artifactjson.Ref{}, artifactjson.ErrQuota
	}
	if outcome == "conflict" {
		return artifactjson.Ref{}, artifactjson.ErrConflict
	}
	if outcome == "forbidden" || r.ID == uuid.Nil || r.Token == uuid.Nil {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	ref := artifactjson.Ref{ID: r.ID, SHA256: digest}
	if r.Ready {
		return ref, nil
	}
	key := resourceKey(tenantID, r.ID)
	obj, e := m.Objects.PutImmutable(ctx, key, bytes.NewReader(raw), int64(len(raw)), digest, "font/ttf")
	if errors.Is(e, objectstore.ErrExists) || errors.Is(e, objectstore.ErrUnavailable) {
		obj, e = m.Objects.RecoverImmutable(ctx, key, int64(len(raw)), digest, "font/ttf")
	}
	if e != nil {
		return artifactjson.Ref{}, e
	}
	var ok bool
	e = tenant.WithTenant(ctx, m.DB, who, tenantID, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		return tx.QueryRow(ctx, `SELECT photo.complete_contact_sheet_font($1,$2,$3,$4,$5,$6,$7)`, tenantID, r.ID, r.Token, digest, len(raw), m.Bucket, obj.VersionID).Scan(&ok)
	})
	if e != nil {
		return artifactjson.Ref{}, e
	}
	if !ok {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	return ref, nil
}
