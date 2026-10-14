package workflow

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
	"strconv"
	"time"
)

type DeliveryRuntimeConfig struct {
	Enabled  bool
	PageSize int
	Limits   DeliveryDerivativeLimits
}

// Explicit opt-in while the remaining dynamic stages/package assembly are
// implemented. CPU resource ceilings are admission budgets, not measured peaks.
func DeliveryRuntimeFromEnvironment(get func(string) string) (DeliveryRuntimeConfig, error) {
	c := DeliveryRuntimeConfig{PageSize: 10, Limits: DeliveryDerivativeLimits{RAMBytes: 8 << 30, ScratchBytes: 8 << 30, OutputBytes: 2 << 30}}
	if get == nil {
		return c, errors.New("invalid delivery environment reader")
	}
	switch get("PHOTO_DELIVERY_SCHEDULER_ENABLED") {
	case "", "false":
		return c, nil
	case "true":
		c.Enabled = true
	default:
		return c, errors.New("PHOTO_DELIVERY_SCHEDULER_ENABLED must be true or false")
	}
	for _, setting := range []struct {
		name   string
		target *int64
	}{{"PHOTO_DELIVERY_RAM_BYTES", &c.Limits.RAMBytes}, {"PHOTO_DELIVERY_SCRATCH_BYTES", &c.Limits.ScratchBytes}, {"PHOTO_DELIVERY_OUTPUT_BYTES", &c.Limits.OutputBytes}} {
		if raw := get(setting.name); raw != "" {
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || value < 1 || value > 9007199254740991 {
				return c, errors.New("invalid delivery resource ceiling")
			}
			*setting.target = value
		}
	}
	if raw := get("PHOTO_DELIVERY_PAGE_SIZE"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			return c, errors.New("invalid delivery page size")
		}
		c.PageSize = value
	}
	return c, nil
}

type DeliveryQuotaCleanup interface {
	ReleaseNextQuota(context.Context) (bool, error)
}
type DeliveryExpansionJob struct {
	Driver  DeliveryExpansionDriver
	Cleanup DeliveryQuotaCleanup
 Metadata ExpansionJob
}

func (j DeliveryExpansionJob) ProcessOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if j.Metadata != nil {
	 progress, err := j.Metadata.ProcessOne(ctx)
	 if err != nil || progress { return progress, err }
	}
	bounded, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	released := false
	if j.Cleanup != nil {
		var cleanupErr error
		released, cleanupErr = j.Cleanup.ReleaseNextQuota(bounded)
		if cleanupErr != nil {
			return false, cleanupErr
		}
	}
	progress, err := j.Driver.ProcessOne(bounded)
	return released || progress.Admitted > 0 || progress.Observed > 0 || progress.SigningState == "scanning" || progress.AssemblyState == "activated" || progress.AssemblyState == "finalized_failed", err
}
func NewDeliveryExpansionJob(db *pgxpool.Pool, objects artifactjson.Objects, reader v2input.SessionAnalysisObjectReader, bucket, workSHA string, profiles map[string]string, registry Registry, c DeliveryRuntimeConfig) (DeliveryExpansionJob, error) {
	if !c.Enabled || db == nil || objects == nil || reader == nil || len(bucket) < 3 || len(bucket) > 63 || c.PageSize < 1 || c.PageSize > 100 || c.Limits.RAMBytes < 1 || c.Limits.ScratchBytes < 1 || c.Limits.OutputBytes < 1 {
		return DeliveryExpansionJob{}, v2input.ErrInvalid
	}
	for _, sha := range []string{workSHA, profiles["srgb_v1"], profiles["work_rec2020_gamma24_v1"]} {
		if domainDigestInvalid(sha) {
			return DeliveryExpansionJob{}, v2input.ErrInvalid
		}
	}
	pinned := make(map[string]string, len(profiles))
	for name, sha := range profiles {
		pinned[name] = sha
	}
	resolver := SQLDeliveryChildResolver{DB: db, Objects: reader, Bucket: bucket, WorkingICCSHA: workSHA, TargetICCs: pinned}
	pipeline := DeliveryChildPipeline{DB: db, Objects: objects, Reader: reader, Bucket: bucket, Resolver: resolver, Registry: registry, Limits: c.Limits}
	watermarkResolver:=SQLDeliveryWatermarkChildResolver{DB:db,Objects:reader,Bucket:bucket,WorkingICCSHA:workSHA,TargetICCs:pinned}
 watermarkPipeline:=DeliveryWatermarkChildPipeline{DB:db,Objects:objects,Reader:reader,Bucket:bucket,Resolver:watermarkResolver,Registry:registry,Limits:c.Limits}
 router:=DeliveryChildRouter{Ordinary:pipeline,Watermark:watermarkPipeline,OrdinaryObserver:pipeline,WatermarkObserver:watermarkPipeline}
 assembly := DeliveryAssemblyPipeline{DB: db, Objects: objects, Bucket: bucket, Limits: c.Limits}
 if signedObjects,ok:=objects.(delivery.SignedDependencyObjectReader);ok{assembly.SignedObjects=signedObjects}
	metadata := DeliveryMetadataJob{Leases: delivery.MetadataNextBarrierStore{DB:db}, Stage: DeliveryMetadataStage{DB:db,Objects:objects,Bucket:bucket,Registry:registry,Limits:c.Limits}}
	return DeliveryExpansionJob{Metadata:metadata,Cleanup: delivery.ExpansionLeaseStore{DB: db}, Driver: DeliveryExpansionDriver{Store: DeliveryRuntimePlanStore{ExpansionLeaseStore:delivery.ExpansionLeaseStore{DB:db},Transactions:db}, Children: router, Observer: router, Assembly: assembly, PageSize: c.PageSize}}, nil
}
