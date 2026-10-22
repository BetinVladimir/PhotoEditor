package editor

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
)

type sourceGateRow func(...any) error
func (r sourceGateRow) Scan(dest ...any) error { return r(dest...) }

type sourceGateTx struct {
	pgx.Tx
	t *testing.T
	asset bool
	reads int
}
func (tx *sourceGateTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	tx.reads++
	if tx.reads == 1 {
		if !strings.Contains(query, "FOR UPDATE") { tx.t.Fatal("expected real source lock path") }
		return sourceGateRow(func(dest ...any) error {
			*dest[0].(*uuid.UUID) = uuid.New()
			if tx.asset {
				*dest[1].(*uuid.UUID) = uuid.New()
				*dest[2].(*string) = strings.Repeat("a", 64)
			} else { *dest[1].(*string) = strings.Repeat("a", 64) }
			return nil
		})
	}
	if tx.reads == 2 && strings.Contains(query, "photo.authorize_tenant") {
		return sourceGateRow(func(...any) error { return pgx.ErrNoRows })
	}
	tx.t.Fatal("cached administrator role reached project access after source lock")
	return sourceGateRow(func(...any) error { return pgx.ErrNoRows })
}

// Script the session becoming invalid while the source query holds/waits for a
// lock. This proves the helper refuses before cached-role project authorization;
// real PostgreSQL scheduling is deliberately not claimed by this unit fixture.
func TestSourceLocksCannotRetainWithdrawnEditorAuthority(t *testing.T) {
	for _, asset := range []bool{false, true} {
		t.Run(map[bool]string{false: "version", true: "asset"}[asset], func(t *testing.T) {
			tx := &sourceGateTx{t: t, asset: asset}
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			var err error
			if asset {
				_, _, err = authorizeAsset(context.Background(), tx, who, uuid.New(), uuid.New(), domain.Admin, domain.Editor, true)
			} else {
				_, err = authorizeVersion(context.Background(), tx, who, uuid.New(), uuid.New(), domain.Admin, domain.Editor, true)
			}
			if err == nil || domain.AsError(err).Code != "NOT_FOUND" || tx.reads != 2 {
				t.Fatal("source lock bypassed current session authority", err, tx.reads)
			}
		})
	}
}
