package portal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/objectstore"
)

type intentRow struct {
	mode    string
	current bool
}

func (r intentRow) Scan(dest ...any) error {
	switch r.mode {
	case "create":
		id, expiry := uuid.New(), time.Now().Add(time.Minute)
		*dest[0].(*string) = "created"
		*dest[1].(**uuid.UUID) = &id
		*dest[2].(**time.Time) = &expiry
	case "redeem":
		outcome, key, version, sha, media, name, size := "ok", "tenants/t/download.zip", "v1", strings.Repeat("a", 64), "application/zip", "photos-001.zip", int64(42)
		*dest[0].(*string) = outcome
		*dest[1].(**string) = &key
		*dest[2].(**string) = &version
		*dest[3].(**string) = &sha
		*dest[4].(**int64) = &size
		*dest[5].(**string) = &media
		*dest[6].(**string) = &name
	case "current":
		*dest[0].(*bool) = r.current
	}
	return nil
}

type intentDB struct {
	mode    string
	current bool
	args    []any
}

func (d *intentDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	d.args = args
	if strings.Contains(query, "v30_delivery_download_intent_current") {
		return intentRow{mode: "current", current: d.current}
	}
	return intentRow{mode: d.mode}
}
func (d *intentDB) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }

type intentStore struct {
	portalMultipartFake
	name         string
	object       objectstore.Object
	ttl          time.Duration
	checked      objectstore.Object
	checkErr     error
	signedURL    string
	signedExpiry time.Time
}

func (s *intentStore) Check(_ context.Context, object objectstore.Object) error {
	s.checked = object
	return s.checkErr
}

func (s *intentStore) SignDownload(_ context.Context, o objectstore.Object, name string, ttl time.Duration) (objectstore.ReadURL, error) {
	s.name, s.object, s.ttl = name, o, ttl
	raw := s.signedURL
	if raw == "" {
		raw = "https://storage.test/download?versionId=v1"
	}
	expiry := s.signedExpiry
	if expiry.IsZero() {
		expiry = time.Now().Add(time.Minute)
	}
	return objectstore.ReadURL{URL: raw, ExpiresAt: expiry}, nil
}

func TestDownloadIntentRefusesExtendedSignedURLLifetime(t *testing.T) {
	db := &intentDB{mode: "redeem", current: true}
	store := &intentStore{signedExpiry: time.Now().Add(5 * time.Minute)}
	service := Service{DB: db, Pepper: []byte(strings.Repeat("p", 32)), Objects: store, Bucket: "photo-test"}
	if _, err := service.RedeemDownloadIntent(context.Background(), DownloadRedeemRequest{Token: strings.Repeat("A", 43)}); err == nil {
		t.Fatal("overlong signed Delivery download URL exposed")
	}
}

func TestDownloadIntentRefusesUnboundSignedURL(t *testing.T) {
	for _, raw := range []string{
		"https://storage.test/download",
		"https://storage.test/download?versionId=other",
		"https://storage.test/download?versionId=v1&versionId=other",
		"http://storage.test/download?versionId=v1",
		"http://localhost.evil.test/download?versionId=v1",
		"http://192.168.1.2/download?versionId=v1",
		"https://user@storage.test/download?versionId=v1",
	} {
		db := &intentDB{mode: "redeem", current: true}
		store := &intentStore{signedURL: raw}
		service := Service{DB: db, Pepper: []byte(strings.Repeat("p", 32)), Objects: store, Bucket: "photo-test"}
		if _, err := service.RedeemDownloadIntent(context.Background(), DownloadRedeemRequest{Token: strings.Repeat("A", 43)}); err == nil {
			t.Fatalf("unbound signed URL exposed: %s", raw)
		}
	}
}

func TestDownloadIntentAllowsVersionedLoopbackObjectStore(t *testing.T) {
	for _, raw := range []string{
		"http://localhost:9000/download?versionId=v1",
		"http://127.0.0.2:9000/download?versionId=v1",
		"http://[::1]:9000/download?versionId=v1",
	} {
		db := &intentDB{mode: "redeem", current: true}
		store := &intentStore{signedURL: raw}
		service := Service{DB: db, Pepper: []byte(strings.Repeat("p", 32)), Objects: store, Bucket: "photo-test"}
		link, err := service.RedeemDownloadIntent(context.Background(), DownloadRedeemRequest{Token: strings.Repeat("A", 43)})
		if err != nil || link.URL != raw {
			t.Fatalf("local object URL rejected: %s, %v", raw, err)
		}
	}
}

func TestDownloadIntentNeverStoresBearerAndRedeemsAttachment(t *testing.T) {
	db := &intentDB{mode: "create"}
	service := Service{DB: db, Pepper: []byte(strings.Repeat("p", 32)), Random: strings.NewReader(strings.Repeat("r", 32)), Now: func() time.Time { return time.Unix(1000, 0).UTC() }}
	intent, err := service.CreateDownloadIntent(context.Background(), "session", "csrf", "intent-key", uuid.New(), DownloadIntentRequest{Volume: 1})
	if err != nil || intent.Token == "" {
		t.Fatal(intent, err)
	}
	for _, arg := range db.args {
		if value, ok := arg.(string); ok && value == intent.Token {
			t.Fatal("bearer passed to persistence")
		}
	}
	db.mode = "redeem"
	db.current = true
	store := &intentStore{}
	service.Objects = store
	service.Bucket = "photo-test"
	link, err := service.RedeemDownloadIntent(context.Background(), DownloadRedeemRequest{Token: intent.Token})
	if err != nil || link.Bytes != 42 || link.SHA256 != strings.Repeat("a", 64) || store.name != "photos-001.zip" {
		t.Fatal(link, err, store.name)
	}
	if store.ttl != time.Minute {
		t.Fatal("download URL TTL must be one minute", store.ttl)
	}
	if store.object.VersionID != "v1" {
		t.Fatal("object version not pinned")
	}
	if store.checked != store.object {
		t.Fatal("signed object was not checked against storage")
	}
}

func TestDownloadIntentRejectsMissingPhysicalVersionBeforeSigning(t *testing.T) {
	db := &intentDB{mode: "redeem", current: true}
	store := &intentStore{checkErr: errors.New("missing version")}
	service := Service{DB: db, Pepper: []byte(strings.Repeat("p", 32)), Objects: store, Bucket: "photo-test"}
	if _, err := service.RedeemDownloadIntent(context.Background(), DownloadRedeemRequest{Token: strings.Repeat("A", 43)}); err == nil {
		t.Fatal("missing physical version returned a URL")
	}
	if store.checked.VersionID != "v1" || store.object.Key != "" {
		t.Fatal("storage check must precede URL signing")
	}
}

func TestDownloadIntentRejectsRevokedTargetAfterSigning(t *testing.T) {
	db := &intentDB{mode: "redeem", current: false}
	store := &intentStore{}
	service := Service{DB: db, Pepper: []byte(strings.Repeat("p", 32)),
		Objects: store, Bucket: "photo-test"}
	token := strings.Repeat("A", 43)
	if _, err := service.RedeemDownloadIntent(context.Background(),
		DownloadRedeemRequest{Token: token}); err == nil {
		t.Fatal("revoked Delivery target returned a signed URL")
	}
	if store.object.VersionID != "v1" {
		t.Fatal("post-signing authority was not exercised")
	}
}

func TestItemDownloadIntentPinsItemAndPresetWithoutPersistingBearer(t *testing.T) {
	db := &intentDB{mode: "create"}
	service := Service{DB: db, Pepper: []byte(strings.Repeat("p", 32)), Now: func() time.Time { return time.Unix(1000, 0).UTC() }}
	item, preset := uuid.New(), uuid.New()
	intent, err := service.CreateItemDownloadIntent(context.Background(), "session", "csrf", "item-intent-key", item, SingleDownloadRequest{PresetID: preset})
	if err != nil || intent.Token == "" {
		t.Fatal(intent, err)
	}
	if db.args[4] != item || db.args[5] != preset {
		t.Fatal("item or preset not pinned", db.args)
	}
	for _, arg := range db.args {
		if value, ok := arg.(string); ok && value == intent.Token {
			t.Fatal("bearer passed to persistence")
		}
	}
}

func TestDownloadIntentRejectsMalformedBearerBeforeSQL(t *testing.T) {
	db := &intentDB{mode: "redeem"}
	service := Service{DB: db, Pepper: []byte(strings.Repeat("p", 32)), Objects: &intentStore{}, Bucket: "photo-test"}
	if _, err := service.RedeemDownloadIntent(context.Background(), DownloadRedeemRequest{Token: "bad"}); err == nil || db.args != nil {
		t.Fatal("malformed bearer reached database")
	}
}
