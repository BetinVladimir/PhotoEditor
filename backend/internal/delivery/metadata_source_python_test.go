package delivery

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
	"time"
)

func TestMetadataSourcePythonResponseExternalAuthority(t *testing.T) {
	scope := MetadataSourcePublicationScope{Tenant: uuid.New(), Project: uuid.New(), Actor: uuid.New(), Session: uuid.New(), Binding: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}, Original: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}, OriginalBytes: 100, OriginalMIME: "image/jpeg"}
	source, err := canonical.Marshal(MetadataDocument{})
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	plan, err := CompileMetadataSourcePublication(scope, v2input.Ref{ArtifactID: id, SHA256: digest(source)}, source, "copyright", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := canonical.Marshal(map[string]any{"schema_version": 1, "publication_jcs": string(plan.JCS()), "publication_sha256": plan.SHA256()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restoreMetadataSourceResponse(raw, scope, id, "copyright", nil); err != nil {
		t.Fatal(err)
	}
	foreign := scope
	foreign.Actor = uuid.New()
	if _, err = restoreMetadataSourceResponse(raw, foreign, id, "copyright", nil); err == nil {
		t.Fatal("foreign authority accepted")
	}
	if _, err = restoreMetadataSourceResponse(raw, scope, uuid.New(), "copyright", nil); err == nil {
		t.Fatal("foreign metadata ID accepted")
	}
	if _, err = restoreMetadataSourceResponse(raw, scope, id, "approved_exif", []string{"gps"}); err == nil {
		t.Fatal("changed consent accepted")
	}
	if _, err = restoreMetadataSourceResponse(append(raw, '\n'), scope, id, "copyright", nil); err == nil {
		t.Fatal("noncanonical wrapper accepted")
	}
}
func TestMetadataSourcePythonEnvironmentAndBoundedOutput(t *testing.T) {
	env := map[string]string{"DATABASE_URL": "postgres://api", "S3_ENDPOINT": "localhost:9000", "S3_BUCKET": "photos", "S3_ACCESS_KEY": "key", "S3_SECRET_KEY": "secret", "PHOTO_METADATA_SOURCE_SCRATCH_ROOT": "/scratch", "PHOTO_METADATA_SOURCE_MAX_BYTES": "1024", "PHOTO_METADATA_SOURCE_RAM_BYTES": "268439552", "PHOTO_METADATA_SOURCE_SCRATCH_BYTES": "8389632"}
	p := PythonMetadataSourcePreparer{Python: "/usr/bin/python3", WorkerRoot: "/workers", Timeout: time.Minute, Environment: env}
	if _, err := p.environment(); err != nil {
		t.Fatal(err)
	}
	rawScope := MetadataSourcePublicationScope{Tenant: uuid.New(), Project: uuid.New(), Actor: uuid.New(), Session: uuid.New(),
		Binding:  v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)},
		Original: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}, OriginalBytes: 100, OriginalMIME: "image/x-adobe-dng"}
	if _, err := p.Prepare(context.Background(), rawScope, uuid.New(), "copyright", nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("RAW without installed tool reached subprocess", err)
	}
	env["PHOTO_METADATA_SOURCE_EXIFTOOL"] = "/opt/photo/bin/exiftool"
	env["PHOTO_METADATA_SOURCE_EXIFTOOL_SHA256"] = strings.Repeat("a", 64)
	if _, err := p.environment(); err != nil {
		t.Fatal("absolute RAW tool refused", err)
	}
	env["PHOTO_METADATA_SOURCE_EXIFTOOL"] = "./exiftool"
	if _, err := p.environment(); err == nil {
		t.Fatal("relative RAW tool accepted")
	}
	delete(env, "PHOTO_METADATA_SOURCE_EXIFTOOL")
	if _, err := p.environment(); err == nil {
		t.Fatal("orphan RAW tool digest accepted")
	}
	delete(env, "PHOTO_METADATA_SOURCE_EXIFTOOL_SHA256")
	env["PYTHONPATH"] = "/untrusted"
	if _, err := p.environment(); err == nil {
		t.Fatal("environment injection accepted")
	}
	delete(env, "PYTHONPATH")
	env["PHOTO_METADATA_SOURCE_RAM_BYTES"] = "1"
	if _, err := p.environment(); err == nil {
		t.Fatal("undersized budget accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Prepare(ctx, MetadataSourcePublicationScope{}, uuid.Nil, "copyright", nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	bounded, stop := context.WithCancel(context.Background())
	defer stop()
	output := metadataSourceOutput{cancel: stop}
	if _, err := output.Write(make([]byte, metadataSourceResponseLimit+1)); err == nil || bounded.Err() == nil || len(output.body) != 0 {
		t.Fatal("overflow not cancelled")
	}
}
