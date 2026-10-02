package portal

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/objectstore"
)

type intentRow struct{ mode string }

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
	}
	return nil
}

type intentDB struct {
	mode string
	args []any
}

func (d *intentDB) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	d.args = args
	return intentRow{d.mode}
}
func (d *intentDB) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }

type intentStore struct {
	portalMultipartFake
	name   string
	object objectstore.Object
	ttl    time.Duration
}

func (s *intentStore) SignDownload(_ context.Context, o objectstore.Object, name string, ttl time.Duration) (objectstore.ReadURL, error) {
	s.name, s.object, s.ttl = name, o, ttl
	return objectstore.ReadURL{URL: "https://storage.test/download", ExpiresAt: time.Now().Add(time.Minute)}, nil
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
