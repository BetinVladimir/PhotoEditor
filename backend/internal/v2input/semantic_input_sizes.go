package v2input

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
)

// exactSemanticInputBytes rejects missing, extra, duplicate and hash-substituted
// catalog rows. Rows must already be independently authorized and ready.
func exactSemanticInputBytes(params Ref, inputs []Ref, sizes []SemanticInputSize) (int64, error) {
	const safe int64 = 9007199254740991
	if len(inputs) == 0 || len(inputs) > 4096 || len(sizes) != len(inputs)+1 {
		return 0, ErrInvalid
	}
	known := make(map[uuid.UUID]SemanticInputSize, len(sizes))
	for _, size := range sizes {
		if size.Ref.ArtifactID == uuid.Nil || size.Bytes <= 0 || size.Bytes > safe {
			return 0, ErrInvalid
		}
		if _, err := domain.ParseDigest(size.Ref.SHA256); err != nil {
			return 0, ErrInvalid
		}
		if _, exists := known[size.Ref.ArtifactID]; exists {
			return 0, ErrInvalid
		}
		known[size.Ref.ArtifactID] = size
	}
	seen := make(map[uuid.UUID]bool, len(sizes))
	var total int64
	refs := append([]Ref{params}, inputs...)
	for _, ref := range refs {
		size, exists := known[ref.ArtifactID]
		if !exists || size.Ref != ref || seen[ref.ArtifactID] || size.Bytes > safe-total {
			return 0, ErrInvalid
		}
		seen[ref.ArtifactID] = true
		total += size.Bytes
	}
	if known[params.ArtifactID].Bytes > 1048576 {
		return 0, ErrInvalid
	}
	return total, nil
}
