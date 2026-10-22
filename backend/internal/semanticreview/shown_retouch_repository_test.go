package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

type retouchPublicationRow func(...any) error

func (r retouchPublicationRow) Scan(d ...any) error { return r(d...) }

type retouchPublicationQuery struct {
	t         *testing.T
	calls     int
	id, token uuid.UUID
	outcome   string
	done      bool
}

func (q *retouchPublicationQuery) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	q.calls++
	if strings.Contains(sql, "reserve_semantic_shown_retouch_transport_document") {
		if len(args) != 18 || args[14] != q.id {
			q.t.Fatal("reserve scope", args)
		}
		return retouchPublicationRow(func(d ...any) error {
			*(d[0].(*string)) = q.outcome
			id, token := q.id, q.token
			*(d[1].(**uuid.UUID)) = &id
			*(d[2].(**uuid.UUID)) = &token
			*(d[3].(*bool)) = q.outcome == "replayed"
			return nil
		})
	}
	if strings.Contains(sql, "complete_semantic_shown_retouch_transport_document") {
		if len(args) != 20 || args[14] != q.id {
			q.t.Fatal("complete scope", args)
		}
		return retouchPublicationRow(func(d ...any) error { *(d[0].(*bool)) = q.done; return nil })
	}
	q.t.Fatal("unexpected SQL", sql)
	return nil
}
func TestShownRetouchRepositoryReservationAndReplay(t *testing.T) {
	for _, outcome := range []string{"created", "resumed", "replayed", "conflict", "stale", "forbidden", "quota", "unknown"} {
		q := &retouchPublicationQuery{t: t, id: uuid.New(), token: uuid.New(), outcome: outcome}
		scope := artifactjson.Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: "task_params"}
		r := shownRetouchDocumentRepository{db: q, scope: scope, params: v2input.Ref{ArtifactID: q.id, SHA256: strings.Repeat("a", 64)}}
		got, err := r.Reserve(context.Background(), scope, r.params.SHA256, 2)
		valid := outcome == "created" || outcome == "resumed" || outcome == "replayed"
		if valid {
			if err != nil || got.ArtifactID != q.id || (got.Ready != nil) != (outcome == "replayed") {
				t.Fatal(outcome, got, err)
			}
		} else if err == nil {
			t.Fatal("invalid reservation outcome", outcome)
		}
		before := q.calls
		foreign := scope
		foreign.Tenant = uuid.New()
		if _, err = r.Reserve(context.Background(), foreign, r.params.SHA256, 2); err == nil || q.calls != before {
			t.Fatal("foreign scope reached DB")
		}
	}
}
func TestShownRetouchRepositoryCompletionVersionAndScope(t *testing.T) {
	for _, mode := range []string{"valid", "stale", "version", "key", "sha", "token", "scope", "canceled"} {
		q := &retouchPublicationQuery{t: t, id: uuid.New(), token: uuid.New(), done: mode != "stale"}
		scope := artifactjson.Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: "task_params"}
		r := shownRetouchDocumentRepository{db: q, scope: scope, params: v2input.Ref{ArtifactID: q.id, SHA256: strings.Repeat("a", 64)}}
		ticket := artifactjson.Reservation{ArtifactID: q.id, Token: q.token}
		key, err := artifactjson.DocumentKey(scope, q.id)
		if err != nil {
			t.Fatal(err)
		}
		object := objectstore.Object{Key: key, SHA256: r.params.SHA256, Bytes: 2, ContentType: "application/json", VersionID: "v1"}
		ctx, cancel := context.WithCancel(context.Background())
		switch mode {
		case "version":
			object.VersionID = "null"
		case "key":
			object.Key = "foreign"
		case "sha":
			object.SHA256 = strings.Repeat("b", 64)
		case "token":
			ticket.Token = uuid.Nil
		case "scope":
			scope.Owner = uuid.New()
		case "canceled":
			cancel()
		}
		got, err := r.Complete(ctx, scope, ticket, object)
		cancel()
		if mode == "valid" {
			if err != nil || got.ID != q.id {
				t.Fatal(got, err)
			}
		} else if err == nil || got.ID != uuid.Nil {
			t.Fatal("invalid completion", mode, err)
		}
		if mode != "valid" && mode != "stale" && q.calls != 0 {
			t.Fatal("invalid object reached DB", mode)
		}
	}
}
