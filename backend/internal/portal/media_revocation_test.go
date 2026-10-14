package portal

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/objectstore"
)

type mediaRevocationRow struct{ revoked bool }

func (row mediaRevocationRow) Scan(dest ...any) error {
	if row.revoked {
		return pgx.ErrNoRows
	}
	for i := 0; i < 4; i++ {
		*dest[i].(*uuid.UUID) = uuid.New()
	}
	*dest[4].(*int64) = 1
	*dest[5].(*string) = "en"
	*dest[6].(*json.RawMessage) = json.RawMessage(`[]`)
	*dest[7].(*json.RawMessage) = json.RawMessage(`{}`)
	*dest[8].(*string) = "off"
	*dest[9].(*json.RawMessage) = json.RawMessage(`[]`)
	return nil
}

type mediaRevocationRows struct {
	pgx.Rows
	item    uuid.UUID
	version string
	used    bool
}

func (rows *mediaRevocationRows) Close()     {}
func (rows *mediaRevocationRows) Err() error { return nil }
func (rows *mediaRevocationRows) Next() bool {
	if rows.used {
		return false
	}
	rows.used = true
	return true
}
func (rows *mediaRevocationRows) Scan(dest ...any) error {
	*dest[0].(*int64) = 1
	*dest[1].(*int64) = 1
	*dest[2].(*string) = "ok"
	*dest[3].(*uuid.UUID) = rows.item
	*dest[4].(*string) = "thumb320"
	key, version, sha, media := "tenants/t/thumb.jpg", rows.version, strings.Repeat("a", 64), "image/jpeg"
	size, width, height := int64(42), 320, 200
	*dest[5].(**string), *dest[6].(**string), *dest[7].(**string) = &key, &version, &sha
	*dest[8].(**int64), *dest[9].(**string) = &size, &media
	*dest[10].(**int), *dest[11].(**int) = &width, &height
	return nil
}

type mediaRevocationDB struct {
	authCalls     int
	resolveCalls  int
	item          uuid.UUID
	revoke        bool
	changeVersion bool
}

func (db *mediaRevocationDB) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	if !strings.Contains(query, "authorize_portal_session") {
		panic("unexpected query")
	}
	db.authCalls++
	return mediaRevocationRow{revoked: db.revoke && db.authCalls > 1}
}
func (db *mediaRevocationDB) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	if !strings.Contains(query, "resolve_portal_media") {
		panic("unexpected query")
	}
	db.resolveCalls++
	version := "v1"
	if db.changeVersion && db.resolveCalls > 1 {
		version = "v2"
	}
	return &mediaRevocationRows{item: db.item, version: version}, nil
}

type mediaRevocationSigner struct {
	portalMultipartFake
	called  bool
	missing bool
	checked bool
}

func (signer *mediaRevocationSigner) Check(_ context.Context, _ objectstore.Object) error {
	signer.checked = true
	if signer.missing {
		return errors.New("object version missing")
	}
	return nil
}

func (signer *mediaRevocationSigner) SignRead(_ context.Context, object objectstore.Object, _ time.Duration) (objectstore.ReadURL, error) {
	signer.called = true
	return objectstore.ReadURL{URL: "https://storage.test/read?versionId=" + object.VersionID, ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func TestPortalMediaRevocationDuringSigningExposesNoLinks(t *testing.T) {
	item := uuid.New()
	db := &mediaRevocationDB{item: item, revoke: true}
	signer := &mediaRevocationSigner{}
	service := Service{DB: db, Pepper: []byte(strings.Repeat("p", 32)), Objects: signer, Bucket: "photo-test"}
	links, err := service.MediaLinks(context.Background(), "session", "csrf", MediaRequest{ItemIDs: []uuid.UUID{item}, Variants: []string{"thumb320"}})
	if err == nil || len(links.Items) != 0 || !signer.called || db.authCalls != 2 {
		t.Fatal("revoked share exposed a signed media link", links, err, db.authCalls)
	}
}

func TestPortalMediaVersionChangesDuringSigningExposesNoLinks(t *testing.T) {
	item := uuid.New()
	db := &mediaRevocationDB{item: item, changeVersion: true}
	signer := &mediaRevocationSigner{}
	service := Service{DB: db, Pepper: []byte(strings.Repeat("p", 32)), Objects: signer, Bucket: "photo-test"}
	links, err := service.MediaLinks(context.Background(), "session", "csrf", MediaRequest{ItemIDs: []uuid.UUID{item}, Variants: []string{"thumb320"}})
	if err == nil || len(links.Items) != 0 || !signer.called || db.authCalls != 2 || db.resolveCalls != 2 {
		t.Fatal("changed object version exposed a signed media link", links, err, db.authCalls, db.resolveCalls)
	}
}

func TestPortalMediaStableVersionReturnsSignedLink(t *testing.T) {
	item := uuid.New()
	db := &mediaRevocationDB{item: item}
	signer := &mediaRevocationSigner{}
	service := Service{DB: db, Pepper: []byte(strings.Repeat("p", 32)), Objects: signer, Bucket: "photo-test"}
	links, err := service.MediaLinks(context.Background(), "session", "csrf", MediaRequest{ItemIDs: []uuid.UUID{item}, Variants: []string{"thumb320"}})
	if err != nil || len(links.Items) != 1 || links.Items[0].ItemID != item || db.resolveCalls != 2 || !signer.checked {
		t.Fatal("stable object version did not return its signed link", links, err, db.resolveCalls)
	}
}

func TestPortalMediaMissingObjectNeverGetsSigned(t *testing.T) {
	item := uuid.New()
	db := &mediaRevocationDB{item: item}
	signer := &mediaRevocationSigner{missing: true}
	service := Service{DB: db, Pepper: []byte(strings.Repeat("p", 32)), Objects: signer, Bucket: "photo-test"}
	links, err := service.MediaLinks(context.Background(), "session", "csrf", MediaRequest{ItemIDs: []uuid.UUID{item}, Variants: []string{"thumb320"}})
	if err == nil || len(links.Items) != 0 || !signer.checked || signer.called || db.resolveCalls != 1 {
		t.Fatal("missing object version was signed", links, err, db.resolveCalls)
	}
}
