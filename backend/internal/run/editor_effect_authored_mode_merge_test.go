package run

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

// Embedded pgx.Tx makes every unexpected database operation fail immediately.
// Reuse the existing boolean row double; no connection or database is involved.
type r22AuthoredModeTx struct {
	pgx.Tx
	t             *testing.T
	refuse        string
	failure       error
	queries       []string
	binds, legacy int
}

func (tx *r22AuthoredModeTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	tx.queries = append(tx.queries, query)
	switch {
	case strings.Contains(query, "photo.authored_render_base_origin_scope("):
		if len(args) != 11 {
			tx.t.Fatal("authored authorization argument binding")
		}
	case strings.Contains(query, "photo.bind_authored_render_base_child("):
		if len(args) != 7 {
			tx.t.Fatal("authored child argument binding")
		}
		tx.binds++
	case strings.Contains(query, "photo.editor_effect_origin_scope("), strings.Contains(query, "photo.lock_editor_effect_identity("):
		if len(args) != 12 {
			tx.t.Fatal("standard authorization argument binding")
		}
	default:
		tx.t.Fatalf("unexpected query: %s", query)
	}
	refused := tx.refuse != "" && strings.Contains(query, tx.refuse)
	if refused && tx.failure != nil {
		return editorIdentityInstallationRow{err: tx.failure}
	}
	return editorIdentityInstallationRow{ready: !refused}
}
func (tx *r22AuthoredModeTx) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if strings.HasPrefix(query, "INSERT INTO photo.editor_effect_builds(") {
		tx.legacy++
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}
	if !strings.HasPrefix(query, "SELECT ") || !strings.Contains(query, "FOR SHARE") {
		tx.t.Fatalf("unexpected write: %s", query)
	}
	return pgconn.NewCommandTag("SELECT 1"), nil
}
func r22AuthoredModeFixture() (*editorEffectApplyMode, auth.Identity, uuid.UUID) {
	m := &editorEffectApplyMode{ProjectID: uuid.New(), ItemID: uuid.New(), DraftRevision: 1, Binding: v2input.SessionVersionBinding{AssetVersionID: uuid.New()}, ExecutedStyle: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}}
	m.AuthoredBase = &authoredBaseApplyBinding{ParentRunID: uuid.New(), ProgramSHA: strings.Repeat("b", 64), PhasePlanSHA: strings.Repeat("c", 64), OriginalSource: m.Binding, EffectiveStyle: m.ExecutedStyle}
	return m, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New()
}
func TestR22AuthoredModeAuthorizationRefusal(t *testing.T) {
	for _, name := range []string{"precompose", "invalid binding", "scope refused", "scope error"} {
		t.Run(name, func(t *testing.T) {
			m, who, tid := r22AuthoredModeFixture()
			tx := &r22AuthoredModeTx{t: t}
			sentinel := errors.New("scope failed")
			switch name {
			case "precompose":
				m.Precompose = true
			case "invalid binding":
				m.AuthoredBase.ProgramSHA = "invalid"
			case "scope refused":
				tx.refuse = "authored_render_base_origin_scope"
			case "scope error":
				tx.refuse = "authored_render_base_origin_scope"
				tx.failure = sentinel
			}
			err := m.authorize(context.Background(), tx, who, tid, false)
			if err == nil {
				t.Fatal("unauthorized authored mode accepted")
			}
			if name == "scope error" && !errors.Is(err, sentinel) {
				t.Fatal("original authorization failure lost")
			}
			expected := 1
			if name == "precompose" || name == "invalid binding" {
				expected = 0
			}
			if len(tx.queries) != expected || tx.binds != 0 || tx.legacy != 0 {
				t.Fatal("authorization continued after authored refusal")
			}
		})
	}
}
func TestR22AuthoredModeCommitRouting(t *testing.T) {
	for _, name := range []string{"authored", "bind refused", "bind error", "ordinary"} {
		t.Run(name, func(t *testing.T) {
			m, who, tid := r22AuthoredModeFixture()
			tx := &r22AuthoredModeTx{t: t}
			sentinel := errors.New("bind failed")
			switch name {
			case "bind refused":
				tx.refuse = "bind_authored_render_base_child"
			case "bind error":
				tx.refuse = "bind_authored_render_base_child"
				tx.failure = sentinel
			case "ordinary":
				m.AuthoredBase = nil
			}
			err := m.commit(context.Background(), tx, who, tid, ApplyAccepted{RunID: uuid.New()})
			if (err == nil) != (name == "authored" || name == "ordinary") {
				t.Fatal("wrong commit outcome", err)
			}
			if name == "bind error" && !errors.Is(err, sentinel) {
				t.Fatal("original bind failure lost")
			}
			if name == "ordinary" {
				if tx.binds != 0 || tx.legacy != 1 {
					t.Fatal("ordinary commit routing changed")
				}
				return
			}
			if tx.binds != 1 || tx.legacy != 0 {
				t.Fatal("authored commit entered legacy editor-effect insert")
			}
			if len(tx.queries) != 6 || !strings.Contains(tx.queries[0], "authored_render_base_origin_scope") || !strings.Contains(tx.queries[2], "lock_editor_effect_identity") || !strings.Contains(tx.queries[5], "bind_authored_render_base_child") {
				t.Fatal("authored commit bypassed standard authorization and locked recheck")
			}
		})
	}
}
