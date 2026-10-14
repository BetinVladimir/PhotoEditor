package artifactjson

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
)

func TestSeriesHoldoutUsesDedicatedPublicationAuthority(t *testing.T) {
	scope := Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(),
		Purpose: "v23_series_holdout", Project: uuid.New(), Actor: uuid.New(),
		AuthSession: uuid.New()}
	for _, invalid := range []func(*Scope){
		func(s *Scope) { s.Project = uuid.Nil },
		func(s *Scope) { s.Actor = uuid.Nil },
		func(s *Scope) { s.AuthSession = uuid.Nil },
	} {
		changed := scope
		invalid(&changed)
		if _, err := DocumentKey(changed, uuid.New()); err == nil {
			t.Fatal("unscoped series document accepted")
		}
	}
	db := &donorRouteDB{}
	repository := SQLRepository{DB: db, Bucket: "photos"}
	sha := strings.Repeat("a", 64)
	if _, err := repository.Reserve(context.Background(), scope, sha, 2); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if !strings.Contains(db.query, "photo.reserve_v23_series_document(") ||
		len(db.args) != 9 || db.args[3] != scope.Project ||
		db.args[4] != scope.Actor || db.args[5] != scope.AuthSession {
		t.Fatal("wrong series reservation authority")
	}
	db.complete = true
	ticket := Reservation{ArtifactID: uuid.New(), Token: uuid.New()}
	key, err := DocumentKey(scope, ticket.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	object := objectstore.Object{Key: key, VersionID: "v1",
		SHA256: sha, Bytes: 2, ContentType: "application/json"}
	if _, err := repository.Complete(context.Background(), scope, ticket,
		object); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if !strings.Contains(db.query, "photo.complete_v23_series_document(") ||
		len(db.args) != 12 || db.args[3] != scope.Project ||
		db.args[4] != scope.Actor || db.args[5] != scope.AuthSession {
		t.Fatal("wrong series completion authority")
	}
}
