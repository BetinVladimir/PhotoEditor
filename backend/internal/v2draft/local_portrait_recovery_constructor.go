package v2draft

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// NewSQLLocalPortraitRecoveryService assembles all recovery dependencies without
// opening connections or enabling operations. Pools must use scheduler/API roles.
func NewSQLLocalPortraitRecoveryService(scheduler, catalog *pgxpool.Pool, bucket string,
	objects v2input.SessionAnalysisObjectReader, documents v2input.DocumentPublisher,
	runtimeSHA string, capacity v2portrait.LocalResourceEstimate) (LocalPortraitRecoveryService, error) {
	var zero LocalPortraitRecoveryService
	if scheduler == nil || catalog == nil || bucket == "" || objects == nil || documents == nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(runtimeSHA); err != nil {
		return zero, v2input.ErrInvalid
	}
	for _, value := range []int64{capacity.PixelCount, capacity.RAMBytes, capacity.ScratchBytes, capacity.OutputBytes} {
		if value < 1 || value > 9007199254740991 {
			return zero, v2input.ErrInvalid
		}
	}
	if capacity.VRAMBytes < 0 || capacity.VRAMBytes > 9007199254740991 {
		return zero, v2input.ErrInvalid
	}
	return LocalPortraitRecoveryService{Owners: SQLLocalPortraitOwnerScope{DB: scheduler},
		Factory: SQLLocalPortraitRecoveryFactory{DB: scheduler, CatalogDB: catalog, Bucket: bucket, Objects: objects, Documents: documents}, RuntimeSHA: runtimeSHA, Capacity: capacity}, nil
}
