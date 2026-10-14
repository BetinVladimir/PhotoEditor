package v2learning

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"

	"github.com/google/uuid"
	"testing"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
)

type evaluationManifestBytes struct{ body []byte }

func (s evaluationManifestBytes) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.body)), nil
}

func TestReadFrozenEvaluationManifestRequiresVersionHashAndCanonicalBytes(t *testing.T) {
	_, _, _, manifest := compiledTrainingDraftFixture(t)
	body, err := canonical.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	object := objectstore.Object{Key: "documents/manifest.json", VersionID: "v1",
		ContentType: "application/json", Bytes: int64(len(body)),
		SHA256: hex.EncodeToString(sum[:])}
	loaded, sha, err := ReadFrozenEvaluationManifest(context.Background(),
		evaluationManifestBytes{body}, object, "photo", "photo")
	if err != nil || sha != object.SHA256 || loaded.ProfileID != manifest.ProfileID {
		t.Fatalf("valid frozen manifest rejected: %v", err)
	}
	changed := append([]byte(nil), body...)
	changed[len(changed)-2] = ' '
	if _, _, err := ReadFrozenEvaluationManifest(context.Background(),
		evaluationManifestBytes{changed}, object, "photo", "photo"); err == nil {
		t.Fatal("changed bytes accepted")
	}
	if _, _, err := ReadFrozenEvaluationManifest(context.Background(),
		evaluationManifestBytes{body}, object, "other", "photo"); err == nil {
		t.Fatal("foreign bucket accepted")
	}
	object.VersionID = "null"
	if _, _, err := ReadFrozenEvaluationManifest(context.Background(),
		evaluationManifestBytes{body}, object, "photo", "photo"); err == nil {
		t.Fatal("unversioned object accepted")
	}
}


func TestFrozenEvaluationManifestRejectsInvalidEnvelopeWithValidObjectDigest(t *testing.T) {
	for _, change := range []string{"profile", "consent", "duplicate", "epoch", "policy", "split"} {
		t.Run(change, func(t *testing.T) {
			_, _, _, manifest := compiledTrainingDraftFixture(t)
			switch change {
			case "profile": manifest.ProfileID = uuid.Nil
			case "consent": manifest.Consents[0].ConsentID = uuid.Nil
			case "duplicate": manifest.Consents = append(manifest.Consents, manifest.Consents[0])
			case "epoch": manifest.Consents[0].Epoch = "9223372036854775808"
			case "policy": manifest.PolicySHA = strings.Repeat("f", 64)
			case "split":
				if manifest.Examples[0].Split == "train" { manifest.Examples[0].Split = "holdout" } else { manifest.Examples[0].Split = "train" }
			}
			body, err := canonical.Marshal(manifest)
			if err != nil { t.Fatal(err) }
			sum := sha256.Sum256(body)
			object := objectstore.Object{Key: "documents/manifest.json", VersionID: "v1",
				ContentType: "application/json", Bytes: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}
			if _, _, err := ReadFrozenEvaluationManifest(context.Background(),
				evaluationManifestBytes{body}, object, "photo", "photo"); err == nil {
				t.Fatal("accepted structurally invalid manifest with matching immutable object digest")
			}
		})
	}
}
