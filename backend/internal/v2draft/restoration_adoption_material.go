package v2draft

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// RestorationCandidateMaterialLoader reconstructs original manual-mask
// authority for a queued result. Queue metadata alone never authorizes reads.
type RestorationCandidateMaterialLoader struct {
	Candidate    RestorationAdoptionCandidate
	DB           v2portrait.DonorConfirmationTransactions
	BaseCatalog  ArtifactCatalog
	DonorCatalog ArtifactCatalog
	Objects      v2input.SessionAnalysisObjectReader
}

func (l RestorationCandidateMaterialLoader) descriptor(who auth.Identity,
	tenant uuid.UUID, operation string, paramsRaw []byte) (restorationTaskDescriptor, error) {
	var descriptor restorationTaskDescriptor
	c := l.Candidate
	if c.Scope.Tenant == uuid.Nil || c.Scope.Task == uuid.Nil || c.Run == uuid.Nil ||
		c.Project == uuid.Nil || c.Snapshot == uuid.Nil ||
		who != c.Actor || tenant != c.Scope.Tenant || operation != c.Operation ||
		!bytes.Equal(paramsRaw, c.Params) || len(c.Descriptor) < 2 ||
		len(c.Descriptor) > 1<<20 || l.DB == nil || l.BaseCatalog == nil ||
		l.Objects == nil {
		return descriptor, v2input.ErrInvalid
	}
	canonicalDescriptor, err := canonical.Bytes(c.Descriptor)
	if err != nil || !bytes.Equal(canonicalDescriptor, c.Descriptor) ||
		json.Unmarshal(c.Descriptor, &descriptor) != nil ||
		descriptor.SchemaVersion != 1 || descriptor.Task.ID != c.Scope.Task ||
		descriptor.Task.Operation != operation ||
		!bytes.Equal(descriptor.Params, paramsRaw) ||
		!isDocumentRef(descriptor.Origin.Root) ||
		!isDocumentRef(descriptor.Origin.Params) ||
		!isDocumentRef(descriptor.Origin.TargetPixel) ||
		!isDocumentRef(descriptor.Origin.ProtectedPixel) {
		return restorationTaskDescriptor{}, v2input.ErrInvalid
	}
	return descriptor, nil
}

func (l RestorationCandidateMaterialLoader) Load(ctx context.Context,
	who auth.Identity, tenant uuid.UUID, operation string, paramsRaw []byte,
) (v2portrait.RestorationDocuments, error) {
	var zero v2portrait.RestorationDocuments
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	descriptor, err := l.descriptor(who, tenant, operation, paramsRaw)
	if err != nil {
		return zero, err
	}
	current, err := ResolveRestorationManualEvidence(ctx, l.DB, who, tenant,
		l.Candidate.Project, l.Candidate.Snapshot, operation, paramsRaw,
		l.BaseCatalog, l.DonorCatalog, l.Objects)
	if err != nil {
		return zero, err
	}
	if current.Origin != descriptor.Origin {
		return zero, v2input.ErrExpired
	}
	return current.Documents, nil
}
