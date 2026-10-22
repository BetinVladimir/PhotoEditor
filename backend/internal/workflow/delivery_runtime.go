package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
	"strconv"
	"time"
)

type DeliveryRuntimeConfig struct {
	Enabled     bool
	PageSize    int
	Limits      DeliveryDerivativeLimits
	WorkTimeout time.Duration
}

// Explicit opt-in while the remaining dynamic stages/package assembly are
// implemented. CPU resource ceilings are admission budgets, not measured peaks.
func DeliveryRuntimeFromEnvironment(get func(string) string) (DeliveryRuntimeConfig, error) {
	c := DeliveryRuntimeConfig{PageSize: 10, WorkTimeout: 5 * time.Minute, Limits: DeliveryDerivativeLimits{RAMBytes: 8 << 30, ScratchBytes: 8 << 30, OutputBytes: 2 << 30}}
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
	if raw := get("PHOTO_DELIVERY_WORK_SECONDS"); raw != "" {
		seconds, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || seconds < 1 || seconds > 7200 {
			return c, errors.New("invalid delivery work budget")
		}
		c.WorkTimeout = time.Duration(seconds) * time.Second
	}
	return c, nil
}

type DeliveryQuotaCleanup interface {
	ReleaseNextQuota(context.Context) (bool, error)
}
type DeliveryExpansionJob struct {
	Driver      DeliveryExpansionDriver
	Cleanup     DeliveryQuotaCleanup
	Metadata    ExpansionJob
	WorkTimeout time.Duration
}

func (j DeliveryExpansionJob) ProcessOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	budget := j.WorkTimeout
	if budget == 0 {
		budget = 5 * time.Minute
	}
	if budget < 0 || budget > 2*time.Hour {
		return false, v2input.ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	metadataProgress := false
	var metadataErr error
	if j.Metadata != nil {
		metadataProgress, metadataErr = j.Metadata.ProcessOne(bounded)
		if err := bounded.Err(); err != nil {
			return metadataProgress, errors.Join(metadataErr, err)
		}
	}
	released := false
	var cleanupErr error
	if j.Cleanup != nil {
		released, cleanupErr = j.Cleanup.ReleaseNextQuota(bounded)
	}
	if err := bounded.Err(); err != nil {
		return metadataProgress || released, errors.Join(metadataErr, cleanupErr, err)
	}
	progress, err := j.Driver.ProcessOne(bounded)
	return metadataProgress || released || progress.Admitted > 0 || progress.Observed > 0 || progress.SigningState == "scanning" || progress.AssemblyState == "activated" || progress.AssemblyState == "finalized_failed", errors.Join(metadataErr, cleanupErr, err)
}
func NewDeliveryExpansionJob(db *pgxpool.Pool, objects artifactjson.Objects, reader v2input.SessionAnalysisObjectReader, instance uuid.UUID, bucket, workSHA string, profiles map[string]string, registry Registry, c DeliveryRuntimeConfig) (DeliveryExpansionJob, error) {
	if !c.Enabled || db == nil || objects == nil || reader == nil || instance == uuid.Nil || len(bucket) < 3 || len(bucket) > 63 || c.PageSize < 1 || c.PageSize > 100 || c.WorkTimeout < 0 || c.WorkTimeout > 2*time.Hour || c.Limits.RAMBytes < 1 || c.Limits.ScratchBytes < 1 || c.Limits.OutputBytes < 1 {
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
	watermarkResolver := SQLDeliveryWatermarkChildResolver{DB: db, Objects: reader, Bucket: bucket, WorkingICCSHA: workSHA, TargetICCs: pinned}
	watermarkPipeline := DeliveryWatermarkChildPipeline{DB: db, Objects: objects, Reader: reader, Bucket: bucket, Resolver: watermarkResolver, Registry: registry, Limits: c.Limits}
	router := DeliveryChildRouter{Ordinary: pipeline, Watermark: watermarkPipeline, OrdinaryObserver: pipeline, WatermarkObserver: watermarkPipeline}
	assembly := DeliveryAssemblyPipeline{DB: db, Objects: objects, Bucket: bucket, Limits: c.Limits}
	if signedObjects, ok := objects.(delivery.SignedDependencyObjectReader); ok {
		assembly.SignedObjects = signedObjects
	}
	metadata := DeliveryMetadataJob{Leases: delivery.MetadataNextBarrierStore{DB: db}, Stage: DeliveryMetadataStage{DB: db, Objects: objects, Bucket: bucket, Registry: registry, Limits: c.Limits}}
	return DeliveryExpansionJob{Metadata: metadata, Cleanup: delivery.ExpansionLeaseStore{DB: db, Instance: instance, Bucket: bucket}, WorkTimeout: c.WorkTimeout, Driver: DeliveryExpansionDriver{Store: DeliveryRuntimePlanStore{ExpansionLeaseStore: delivery.ExpansionLeaseStore{DB: db, Instance: instance, Bucket: bucket}, Transactions: db}, Children: router, Observer: router, Assembly: assembly, PageSize: c.PageSize}}, nil
}
