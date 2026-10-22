package interactive

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
)

type sourceAuthorityRow func(...any) error

func (r sourceAuthorityRow) Scan(dest ...any) error { return r(dest...) }

// Model a source read followed by revoked or demoted tenant authority. Project
// authorization must use the fresh role and require its real workspace/ACL grant.
type sourceAuthorityTx struct {
	pgx.Tx
	t *testing.T
	reads int
	revoked bool
	restricted bool
}

func (tx *sourceAuthorityTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	tx.reads++
	switch tx.reads {
	case 1:
		if !strings.Contains(query, "FROM photo.asset_versions") { tx.t.Fatal("source must be read first") }
		return sourceAuthorityRow(func(dest ...any) error {
			*dest[0].(*uuid.UUID) = uuid.New()
			*dest[1].(*uuid.UUID) = uuid.New()
			*dest[2].(*string) = strings.Repeat("a", 64)
			*dest[3].(*int64) = 100
			return nil
		})
	case 2:
		if !strings.Contains(query, "photo.authorize_tenant") { tx.t.Fatal("cached role reached project authorization") }
		return sourceAuthorityRow(func(dest ...any) error {
			if tx.revoked { return pgx.ErrNoRows }
			*dest[0].(*domain.Role) = domain.Viewer
			*dest[1].(*int64) = 2
			return nil
		})
	case 3:
		if !strings.Contains(query, "FROM photo.projects") { tx.t.Fatal("expected current project authorization") }
		return sourceAuthorityRow(func(dest ...any) error {
			*dest[0].(*uuid.UUID) = uuid.New()
			*dest[1].(*uuid.UUID) = uuid.New()
			*dest[2].(*string) = "Source project"
			*dest[3].(*string) = ""
			visibility := "workspace"
			if tx.restricted { visibility = "restricted" }
			*dest[4].(*string) = visibility
			*dest[5].(*bool) = true
			*dest[6].(*int64) = 1
			*dest[7].(*time.Time) = time.Now()
			return nil
		})
	case 4:
		if !tx.restricted || !strings.Contains(query, "FROM photo.project_acl") { tx.t.Fatal("expected project ACL after administrator demotion") }
		return sourceAuthorityRow(func(...any) error { return pgx.ErrNoRows })
	default:
		tx.t.Fatal("unexpected read after revoked authority")
		return sourceAuthorityRow(func(...any) error { return pgx.ErrNoRows })
	}
}

func TestLoadSourceDoesNotRetainAdminAfterDemotion(t *testing.T) {
	for _, restricted := range []bool{false, true} {
		t.Run(map[bool]string{false: "workspace_requires_editor", true: "restricted_requires_acl"}[restricted], func(t *testing.T) {
			tx := &sourceAuthorityTx{t: t, restricted: restricted}
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			_, err := loadSource(context.Background(), tx, who, uuid.New(), uuid.New(), domain.Admin)
			want, reads := "FORBIDDEN", 3
			if restricted { want, reads = "NOT_FOUND", 4 }
			if err == nil || domain.AsError(err).Code != want || tx.reads != reads {
				t.Fatal("cached Admin bypassed current project authority", err, tx.reads)
			}
		})
	}
}

func TestLoadSourceRefusesRevocationAfterSourceRead(t *testing.T) {
	tx := &sourceAuthorityTx{t: t, revoked: true}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	_, err := loadSource(context.Background(), tx, who, uuid.New(), uuid.New(), domain.Admin)
	if err == nil || domain.AsError(err).Code != "NOT_FOUND" || tx.reads != 2 {
		t.Fatal("source read retained cached administrator authority", err, tx.reads)
	}
}
