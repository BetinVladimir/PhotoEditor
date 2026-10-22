package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
)

// operatorReviewDocuments retains only exact compiler-authored local decision
// and Params bytes. It deliberately is not artifactjson.Repository or a SQL
// review/receipt writer. Every scope/body is fixed before any IO.
type operatorReviewDocuments struct {
	ledger  *OperatorApplyLedger
	objects *OperatorPhysicalObjects
	allowed map[artifactjson.Scope][]byte
}

func operatorCompiledDocumentRef(scope artifactjson.Scope, raw []byte) artifactjson.Ref {
	digest := sha256.Sum256(raw)
	sha := hex.EncodeToString(digest[:])
	return artifactjson.Ref{ID: uuid.NewSHA1(scope.Run, []byte(scope.Owner.String()+"/"+scope.Purpose+"/"+sha)), SHA256: sha}
}
func (p operatorReviewDocuments) Publish(ctx context.Context, scope artifactjson.Scope, raw []byte) (artifactjson.Ref, error) {
	if p.ledger == nil || p.objects == nil || scope.Tenant != p.ledger.lease.TenantID || scope.Run != p.ledger.lease.RunID {
		return artifactjson.Ref{}, errors.New("operator review document scope invalid")
	}
	expected, ok := p.allowed[scope]
	if !ok || !bytes.Equal(expected, raw) || len(raw) < 2 || len(raw) > 1<<20 {
		return artifactjson.Ref{}, errors.New("operator review document was not compiler authored")
	}
	normalized, e := canonical.Bytes(raw)
	if e != nil || !bytes.Equal(normalized, raw) {
		return artifactjson.Ref{}, errors.New("operator review document noncanonical")
	}
	if e = ctx.Err(); e != nil {
		return artifactjson.Ref{}, e
	}
	ref := operatorCompiledDocumentRef(scope, raw)
	path := p.objects.root + string(os.PathSeparator) + ref.ID.String()
	e = writeOperatorExclusive(path, raw)
	if errors.Is(e, os.ErrExist) {
		info, statErr := os.Lstat(path)
		if statErr != nil {
			return artifactjson.Ref{}, statErr
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() != int64(len(raw)) {
			return artifactjson.Ref{}, errors.New("operator review replay file not private exact regular object")
		}
		retained, readErr := os.ReadFile(path)
		if readErr != nil {
			return artifactjson.Ref{}, readErr
		}
		if !bytes.Equal(retained, raw) {
			return artifactjson.Ref{}, errors.New("operator review document replay conflict")
		}
	} else if e != nil {
		return artifactjson.Ref{}, e
	}
	if e = ctx.Err(); e != nil {
		return artifactjson.Ref{}, e
	}
	p.ledger.objects[ref.ID] = styleArtifact{ArtifactID: ref.ID, SHA256: ref.SHA256, Bytes: int64(len(raw)), Bucket: "operator-measurement", Key: ref.ID.String(), VersionID: ref.SHA256, MediaType: "application/json"}
	return ref, nil
}

var _ ContractPublisher = operatorReviewDocuments{}
