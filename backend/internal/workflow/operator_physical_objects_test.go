package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"testing"
)

func TestOperatorPhysicalProofChecksBytesRoleProducerAndVersion(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "work", "measurement", "objects")
	if e := os.MkdirAll(root, 0700); e != nil {
		t.Fatal(e)
	}
	verifier, e := NewOperatorPhysicalObjects(repo, root, map[string]map[string]bool{"geometry.crop": {"working_rgb": true}}, 100)
	if e != nil {
		t.Fatal(e)
	}
	raw := []byte("actual measured bytes")
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	id := uuid.New()
	task := PreparedTask{ID: uuid.New(), Operation: "geometry.crop"}
	path := filepath.Join(root, id.String())
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	proof := OperatorMeasuredOutput{Artifact: styleArtifact{ArtifactID: id, SHA256: digest, Bucket: "operator-measurement", Key: id.String(), VersionID: digest, Bytes: int64(len(raw)), MediaType: "application/octet-stream"}, Role: "working_rgb", Producer: task.ID, Verifier: verifier}
	if e = verifier.VerifyMeasuredObject(context.Background(), task, proof); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"wrong_role", "wrong_producer", "wrong_version", "wrong_digest", "foreign_key"} {
		t.Run(kind, func(t *testing.T) {
			p := proof
			switch kind {
			case "wrong_role":
				p.Role = "document"
			case "wrong_producer":
				p.Producer = uuid.New()
			case "wrong_version":
				p.Artifact.VersionID = "unverified"
			case "wrong_digest":
				p.Artifact.SHA256 = "a" + digest[1:]
				if digest[0] == 'a' {
					p.Artifact.SHA256 = "b" + digest[1:]
				}
				p.Artifact.VersionID = p.Artifact.SHA256
			case "foreign_key":
				p.Artifact.Key = "../escape"
			}
			if e := verifier.VerifyMeasuredObject(context.Background(), task, p); e == nil {
				t.Fatal("invalid proof accepted")
			}
		})
	}
	if e = os.WriteFile(path, []byte("changed measured byte"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = verifier.VerifyMeasuredObject(context.Background(), task, proof); e == nil {
		t.Fatal("mutated bytes accepted")
	}
}
