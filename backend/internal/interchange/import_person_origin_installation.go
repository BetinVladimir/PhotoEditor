package interchange

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/workflow"
)

// ImportPersonOriginRecorderInstallation is operator-owned and never read from
// archive bytes. A key is pinned to the SHA256 of its exact Ed25519 public key.
type ImportPersonOriginRecorderInstallation struct {
	modelLock string
	trusted   map[PortablePersonOriginKey]ed25519.PublicKey
}

func NewImportPersonOriginRecorderInstallation(modelLock string,
	trusted map[PortablePersonOriginKey]ed25519.PublicKey,
) (*ImportPersonOriginRecorderInstallation, error) {
	if !portablePersonOriginSHA.MatchString(modelLock) || len(trusted) == 0 {
		return nil, v2input.ErrInvalid
	}
	owned := &ImportPersonOriginRecorderInstallation{modelLock: modelLock,
		trusted: make(map[PortablePersonOriginKey]ed25519.PublicKey, len(trusted))}
	for key, public := range trusted {
		digest := sha256.Sum256(public)
		if key.IssuerID == uuid.Nil || len(public) != ed25519.PublicKeySize ||
			hex.EncodeToString(digest[:]) != key.KeyID {
			return nil, v2input.ErrInvalid
		}
		owned.trusted[key] = append(ed25519.PublicKey(nil), public...)
	}
	return owned, nil
}

type importCandidateParsedSource struct {
	candidate workflow.InterchangeImportCandidate
}

func (s importCandidateParsedSource) ResolveReviewParsed(ctx context.Context) (v2input.Ref, objectstore.Object, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, objectstore.Object{}, err
	}
	c := s.candidate
	ref := v2input.Ref{ArtifactID: c.Artifact, SHA256: c.SHA}
	object := objectstore.Object{Key: c.Key, VersionID: c.Version, SHA256: c.SHA, Bytes: c.Bytes, ContentType: "application/json"}
	if !validImportRef(ref) || c.Tenant == uuid.Nil || c.Workflow == uuid.Nil || c.Run == uuid.Nil ||
		c.Bucket == "" || object.Key == "" || object.VersionID == "" || object.VersionID == "null" ||
		object.Bytes < 2 || object.Bytes > 8<<20 {
		return v2input.Ref{}, objectstore.Object{}, v2input.ErrInvalid
	}
	return ref, object, nil
}
