package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestExportPersonOriginArchiveInstallationFailsClosedWithoutTrust(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if authority, err := (SQLExportPersonOriginArchiveInstallation{}).
		BuildExportPersonOriginArchiveAuthority(ctx, SQLExportOutputCatalog{}, v2input.Ref{}, nil); err != context.Canceled || authority != nil {
		t.Fatal("cancelled archive installation opened database", err)
	}
	if authority, err := (SQLExportPersonOriginArchiveInstallation{}).
		BuildExportPersonOriginArchiveAuthority(context.Background(), SQLExportOutputCatalog{}, v2input.Ref{}, nil); err != v2input.ErrInvalid || authority != nil {
		t.Fatal("archive installation accepted missing trust", err)
	}
}

func TestExportPersonOriginPhysicalReadRejectsWrongVersionBytes(t *testing.T) {
	body := []byte(`{"schema_version":1}`)
	object := objectstore.Object{Key: "receipt", VersionID: uuid.NewString(), SHA256: rawSHA(body),
		Bytes: int64(len(body) + 1), ContentType: "application/json"}
	if _, err := readExportPersonOriginObject(context.Background(),
		personOriginPublishedReader{body: body}, object, 16384); err == nil {
		t.Fatal("short physical receipt accepted")
	}
}
