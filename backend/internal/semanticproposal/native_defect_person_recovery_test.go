package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestNativeDefectPersonRecoveryExactPinsAndStrictShape(t *testing.T) {
	ctx := context.Background()
	p := *personDefectBindingFixture(t).person
	p.report = v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}
	p.objects[0] = objectstore.Object{Key: "original/person-root", VersionID: "root-v1", SHA256: p.root.SHA256, Bytes: int64(len(p.body)), ContentType: "application/json"}
	p.objects[1] = objectstore.Object{Key: "original/person-pixels", VersionID: "pixels-v1", SHA256: p.pixels.SHA256, Bytes: 4096, ContentType: "application/x-npy"}
	p.seal, _ = nativePersonInputAuthorityDigest(p)
	m, err := nativeDefectPersonRecoveryFromProof(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := canonical.Marshal(m)
	decoded, err := decodeNativeDefectPersonRecovery(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := matchNativeDefectPersonRecovery(ctx, decoded, p); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"root", "pixels", "report", "authority", "root-version", "pixel-version", "key", "bucket", "bytes", "media", "role"} {
		changed := m
		changed.Files = append([]nativeSkinCompletedFile(nil), m.Files...)
		switch kind {
		case "root":
			changed.Root.ArtifactID = uuid.New()
		case "pixels":
			changed.Pixels.ArtifactID = uuid.New()
		case "report":
			changed.Report.ArtifactID = uuid.New()
		case "authority":
			changed.AuthoritySHA = strings.Repeat("b", 64)
		case "root-version":
			changed.Files[0].Version = "substituted"
		case "pixel-version":
			changed.Files[1].Version = "substituted"
		case "key":
			changed.Files[1].Key = "different"
		case "bucket":
			changed.Files[0].Bucket = "different"
			changed.Files[1].Bucket = "different"
		case "bytes":
			changed.Files[1].Bytes++
		case "media":
			changed.Files[1].Media = "application/octet-stream"
		case "role":
			changed.Files[1].Role = "other"
		}
		if err := matchNativeDefectPersonRecovery(ctx, changed, p); !errors.Is(err, v2input.ErrConflict) {
			t.Fatalf("%s substitution accepted: %v", kind, err)
		}
	}
	for _, kind := range []string{"unknown", "null", "schema", "missing-files", "extra-files", "unknown-file", "duplicate", "trailing", "unversioned", "unsafe-bytes", "same-identity"} {
		var obj map[string]any
		_ = json.Unmarshal(raw, &obj)
		switch kind {
		case "unknown":
			obj["ignored"] = true
		case "schema":
			obj["schema_version"] = 2
		case "missing-files":
			obj["files"] = []any{}
		case "extra-files":
			obj["files"] = append(obj["files"].([]any), obj["files"].([]any)[0])
		case "unknown-file":
			obj["files"].([]any)[0].(map[string]any)["ignored"] = true
		case "unversioned":
			obj["files"].([]any)[1].(map[string]any)["version"] = "null"
		case "unsafe-bytes":
			obj["files"].([]any)[1].(map[string]any)["bytes"] = 9007199254740992.0
		case "same-identity":
			obj["qualification_report"] = obj["root"]
		}
		body, _ := json.Marshal(obj)
		switch kind {
		case "null":
			body = []byte(`null`)
		case "duplicate":
			body = append([]byte(`{"schema_version":1,`), body[1:]...)
		case "trailing":
			body = append(body, []byte(` {}`)...)
		}
		if _, err := decodeNativeDefectPersonRecovery(body); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatalf("%s malformed locator accepted: %v", kind, err)
		}
	}
	// Mutating the decoded locator cannot mutate retained producer evidence.
	decoded.Files[0].Version = "changed"
	if p.objects[0].VersionID != "root-v1" {
		t.Fatal("recovery metadata aliases authority")
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := (SQLTransferSourceReader{}).restoreNativeDefectPersonInput(ctx, auth.Identity{}, Request{}, m); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
