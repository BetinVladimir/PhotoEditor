package main

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
)

func TestSceneCreateFixtureDSNIdentity(t *testing.T) {
	for _, original := range []string{"host=/private/tmp/owned port=55433 dbname=photo_test_fixture user=owner password=OWNER options='-c role=photo_fn_owner'", "postgres://owner:OWNER@127.0.0.1:55433/photo_test_fixture?sslmode=disable&options=-c+role%3Dphoto_fn_owner&user=owner&password=OWNER"} {
		before, e := pgxpool.ParseConfig(original)
		if e != nil {
			t.Fatal(e)
		}
		owned := sceneCreateLoginDSN(t, original, "photo_test_scene_safe", "p'ass\\word")
		for _, dsn := range []string{owned, sceneCreateRoleDSN(t, owned, "photo_scheduler")} {
			actual, e := pgxpool.ParseConfig(dsn)
			if e != nil {
				t.Fatal("rewritten fixture DSN invalid")
			}
			if actual.ConnConfig.User != "photo_test_scene_safe" || actual.ConnConfig.Password != "p'ass\\word" || actual.ConnConfig.Host != before.ConnConfig.Host || actual.ConnConfig.Port != before.ConnConfig.Port || actual.ConnConfig.Database != before.ConnConfig.Database {
				t.Fatal("rewritten fixture identity/address drift")
			}
		}
	}
}
