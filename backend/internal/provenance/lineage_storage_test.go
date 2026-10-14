package provenance

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestSigningLineageStorageBindsExactExportPins(t *testing.T) {
	body, expected := contractBytes(t, SigningLineage{Schema: 1, SourceHashes: []string{strings.Repeat("a", 64)}, Actions: []SigningAction{}})
	ready := ReadySigningExport{Tenant: uuid.New(), Project: uuid.New(), ExportID: uuid.New(), Version: uuid.New(), Task: uuid.New(), Run: uuid.New(), Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}, Object: objectstore.Object{Key: "source/key", VersionID: "source-v1", Bytes: 123, ContentType: "image/png", SHA256: strings.Repeat("b", 64)}}
	expected.ArtifactID = uuid.NewSHA1(ready.ExportID, []byte("signing:lineage:"+expected.SHA256))
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	raw, err := signingLineageStoragePins(who, ready, expected, body, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	var pins map[string]json.RawMessage
	if json.Unmarshal(raw, &pins) != nil || len(pins) != 10 {
		t.Fatal("missing exact SQL pins")
	}
	for key, value := range map[string]any{"project": ready.Project, "version": ready.Version, "task": ready.Task, "run": ready.Run, "artifact": ready.Ref.ArtifactID, "sha256": ready.Ref.SHA256, "key": ready.Object.Key, "object_version": ready.Object.VersionID, "bytes": ready.Object.Bytes, "media_type": ready.Object.ContentType} {
		want, _ := json.Marshal(value)
		if string(pins[key]) != string(want) {
			t.Fatal("wrong SQL pin", key)
		}
	}
	for _, mode := range []string{"actor", "session", "tenant", "project", "version", "task", "run", "export", "hash", "source_hash", "source_version", "source_size", "source_media", "bucket", "body", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			w, r, e, b, bucket := who, ready, expected, body, "bucket"
			switch mode {
			case "actor":
				w.UserID = uuid.Nil
			case "session":
				w.SessionID = uuid.Nil
			case "tenant":
				r.Tenant = uuid.Nil
			case "project":
				r.Project = uuid.Nil
			case "version":
				r.Version = uuid.Nil
			case "task":
				r.Task = uuid.Nil
			case "run":
				r.Run = uuid.Nil
			case "export":
				r.ExportID = uuid.New()
			case "hash":
				e.SHA256 = strings.Repeat("c", 64)
			case "source_hash":
				r.Object.SHA256 = strings.Repeat("c", 64)
			case "source_version":
				r.Object.VersionID = "null"
			case "source_size":
				r.Object.Bytes = 0
			case "source_media":
				r.Object.ContentType = "application/json"
			case "bucket":
				bucket = "a"
			case "body":
				b = []byte(`{}`)
			case "oversize":
				b = make([]byte, (8<<20)+1)
			}
			if _, err := signingLineageStoragePins(w, r, e, b, bucket); err == nil {
				t.Fatal("invalid publication scope accepted")
			}
		})
	}
}
func TestSigningLineageStorageReservationOutcomes(t *testing.T) {
	expected := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	id, token := expected.ArtifactID, uuid.New()
	for _, outcome := range []string{"created", "resumed", "replayed"} {
		r, err := signingLineageReservation(outcome, &id, &token, outcome == "replayed", expected)
		if err != nil || r.ArtifactID != id || r.Token != token || (r.Ready != nil) != (outcome == "replayed") {
			t.Fatal("invalid reservation decode", outcome, err)
		}
		if r.Ready != nil && r.Ready.SHA256 != expected.SHA256 {
			t.Fatal("replay changed hash")
		}
	}
	for outcome, want := range map[string]error{"quota": artifactjson.ErrQuota, "conflict": artifactjson.ErrConflict, "forbidden": artifactjson.ErrStale, "stale": artifactjson.ErrStale, "invalid": artifactjson.ErrStale, "unknown": artifactjson.ErrStale} {
		if _, err := signingLineageReservation(outcome, nil, nil, false, expected); !errors.Is(err, want) {
			t.Fatal(outcome, err)
		}
	}
	other := uuid.New()
	nilUUID := uuid.Nil
	for _, c := range []struct {
		id, token *uuid.UUID
		replay    bool
	}{{nil, &token, false}, {&other, &token, false}, {&id, nil, false}, {&id, &nilUUID, false}, {&id, &token, true}} {
		if _, err := signingLineageReservation("created", c.id, c.token, c.replay, expected); !errors.Is(err, objectstore.ErrIntegrity) {
			t.Fatal("malformed receipt accepted", err)
		}
	}
}
