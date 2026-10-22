package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"io"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
)

// OperatorDocumentPublisher retains compiler-authored Params as private bytes.
// It deliberately does not implement artifactjson.Repository: there is no SQL
// ready/document publication masquerading as an accepted runtime receipt.
type OperatorDocumentPublisher struct {
	Objects *OperatorPhysicalObjects
	Ledger  *OperatorApplyLedger
}

func (p OperatorDocumentPublisher) Publish(ctx context.Context, scope artifactjson.Scope, raw []byte) (artifactjson.Ref, error) {
	if p.Objects == nil || p.Ledger == nil || scope.Purpose != "task_params" || scope.Tenant != p.Ledger.lease.TenantID || scope.Run != p.Ledger.lease.RunID {
		return artifactjson.Ref{}, errors.New("operator Params publication scope invalid")
	}
	if e := ctx.Err(); e != nil {
		return artifactjson.Ref{}, e
	}
	plan, ok := p.Ledger.plans[scope.Owner]
	if !ok || !bytes.Equal(plan.Params, raw) || len(raw) < 2 || len(raw) > 8<<20 {
		return artifactjson.Ref{}, errors.New("operator Params not compiler authored")
	}
	canonicalRaw, e := canonical.Bytes(raw)
	if e != nil || !bytes.Equal(raw, canonicalRaw) {
		return artifactjson.Ref{}, errors.New("operator Params must be canonical")
	}
	sum := sha256.Sum256(raw)
	sha := hex.EncodeToString(sum[:])
	if sha != plan.ParamsSHA {
		return artifactjson.Ref{}, errors.New("operator Params digest changed")
	}
	id := uuid.NewSHA1(scope.Run, []byte(scope.Owner.String()+"/"+scope.Purpose+"/"+sha))
	if _, exists := p.Ledger.objects[id]; !exists {
		if e = operatorObjectBudget(p.Ledger, p.Objects.maxObjectBytes, int64(len(raw))); e != nil {
			return artifactjson.Ref{}, e
		}
	}
	key := id.String()
	path := p.Objects.root + string(os.PathSeparator) + key
	file, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(e, os.ErrExist) {
		existing, readErr := p.Objects.Open(ctx, styleArtifact{Key: key, VersionID: sha, SHA256: sha, Bytes: int64(len(raw)), MediaType: "application/json"}.object())
		if readErr != nil {
			return artifactjson.Ref{}, readErr
		}
		retained, readErr := io.ReadAll(io.LimitReader(existing, int64(len(raw))+1))
		closeErr := existing.Close()
		if readErr != nil {
			return artifactjson.Ref{}, readErr
		}
		if closeErr != nil {
			return artifactjson.Ref{}, closeErr
		}
		if !bytes.Equal(retained, raw) {
			return artifactjson.Ref{}, errors.New("operator Params replay conflict")
		}
	} else if e != nil {
		return artifactjson.Ref{}, e
	} else {
		_, writeErr := file.Write(raw)
		syncErr := file.Sync()
		closeErr := file.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			_ = os.Remove(path)
			if writeErr != nil {
				return artifactjson.Ref{}, writeErr
			}
			if syncErr != nil {
				return artifactjson.Ref{}, syncErr
			}
			return artifactjson.Ref{}, closeErr
		}
	}
	p.Ledger.objects[id] = styleArtifact{ArtifactID: id, SHA256: sha, Bucket: "operator-measurement", Key: key, VersionID: sha, Bytes: int64(len(raw)), MediaType: "application/json"}
	return artifactjson.Ref{ID: id, SHA256: sha}, nil
}

var _ ContractPublisher = OperatorDocumentPublisher{}
