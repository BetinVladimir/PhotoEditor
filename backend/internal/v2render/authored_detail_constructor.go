package v2render

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2input"
)

// NewSQLAuthoredDetailMaskPreparation builds the concrete publication, actual
// calibrated segment admission, physical producer readback and phase commit
// chain. A deployment must supply the real pinned segment catalog; there is no
// success callback, neutral profile, default qualification, or source override.
func NewSQLAuthoredDetailMaskPreparation(scheduler, api *pgxpool.Pool, objects interface {
	artifactjson.Objects
	v2input.SessionAnalysisObjectReader
}, bucket string, catalog *PinnedAuthoredSegmentCalibrationCatalog, ram, scratch, output int64) (*SQLAuthoredDetailMaskPreparation, error) {
	if scheduler == nil || api == nil || objects == nil || bucket == "" || catalog == nil || ram < 1073741824 || scratch < 1 || output < 1 || ram > 9007199254740991 || scratch > 9007199254740991 || output > 9007199254740991 {
		return nil, v2input.ErrInvalid
	}
	phase := SQLAuthoredPhaseObjects{API: api, Objects: objects, Bucket: bucket}
	segment := SQLAuthoredSegmentContinuation{Preparation: SQLAuthoredSegmentPreparation{API: api, Params: AuthoredSegmentParamsPublication{Scheduler: scheduler, Objects: objects, Bucket: bucket}, PhaseObjects: phase, Calibrations: catalog}, Producers: AuthoredSegmentProducerReader{Scheduler: scheduler, Objects: objects, Bucket: bucket}, Objects: objects}
	detail := SQLAuthoredDetailMaterialization{Original: phase, Transforms: SQLAuthoredDetailTransformReader{Scheduler: scheduler, Objects: objects, Bucket: bucket}, Masks: AuthoredLayerMaskPublication{Scheduler: scheduler, Objects: objects, Bucket: bucket}, Continuation: SQLAuthoredDetailContinuation{Style: AuthoredStyleBindingPublication{Scheduler: scheduler, Objects: objects, Bucket: bucket}, Params: AuthoredDetailParamsPublication{Scheduler: scheduler, Objects: objects, Bucket: bucket}, Layers: AuthoredCurrentDetailLayersPublication{Scheduler: scheduler, Objects: objects, Bucket: bucket}, Segment: segment}}
	return &SQLAuthoredDetailMaskPreparation{Materialization: detail, Native: AuthoredMaskTransformPreparation{Scheduler: scheduler, Params: AuthoredMaskTransformParamsPublication{Scheduler: scheduler, Objects: objects, Bucket: bucket}}, RAM: ram, Scratch: scratch, Output: output}, nil
}
