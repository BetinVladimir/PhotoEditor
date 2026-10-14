package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/semanticproposal"
)

// Deployment limits contain no tenant identity, qualification or protection refs.
type NativeRecoveryLimits struct {
	SchemaVersion  int                `json:"schema_version"`
	RAM            int64              `json:"ram_bytes"`
	VRAM           int64              `json:"vram_bytes"`
	Scratch        int64              `json:"scratch_bytes"`
	Output         int64              `json:"output_bytes"`
	Metadata       int64              `json:"metadata_bytes"`
	Reserved       int64              `json:"reserved_bytes"`
	TimeoutSeconds int64              `json:"timeout_seconds"`
	Manual         NativeManualLimits `json:"manual"`
	ManualBatch    NativeManualLimits `json:"manual_batch"`
}
type NativeManualLimits struct {
	PointChecks     int64 `json:"point_checks"`
	EdgeEvaluations int64 `json:"edge_evaluations"`
	PayloadBytes    int64 `json:"payload_bytes"`
}

func (c NativeRecoveryLimits) validate() error {
	if c.SchemaVersion != 1 || c.TimeoutSeconds < 1 || c.TimeoutSeconds > 1740 {
		return errors.New("native recovery limits invalid")
	}
	for _, n := range []int64{c.RAM, c.VRAM, c.Scratch, c.Output, c.Metadata, c.Reserved, c.Manual.PointChecks, c.Manual.EdgeEvaluations, c.Manual.PayloadBytes, c.ManualBatch.PointChecks, c.ManualBatch.EdgeEvaluations, c.ManualBatch.PayloadBytes} {
		if n <= 0 || n > 9007199254740991 {
			return errors.New("native recovery limits invalid")
		}
	}
	if c.Metadata > c.RAM/8 {
		return errors.New("native recovery metadata budget invalid")
	}
	return nil
}
func DecodeNativeRecoveryLimits(body []byte) (NativeRecoveryLimits, error) {
	var c NativeRecoveryLimits
	if len(body) == 0 || len(body) > 16384 {
		return c, errors.New("native recovery configuration size invalid")
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return NativeRecoveryLimits{}, errors.New("native recovery configuration invalid")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return NativeRecoveryLimits{}, errors.New("native recovery configuration trailing content")
	}
	if err := c.validate(); err != nil {
		return NativeRecoveryLimits{}, err
	}
	return c, nil
}

// Load before service startup; bounded reads and generic errors avoid logging file contents.
func LoadNativeRecoveryFactory(path, sourceDSN, iccDirectory string) (SchedulerNativeRecoveryFactory, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("native recovery configuration unavailable")
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil {
		return nil, errors.New("native recovery configuration read failed")
	}
	limits, err := DecodeNativeRecoveryLimits(body)
	if err != nil {
		return nil, err
	}
	color, err := semanticproposal.LoadTransferColorProfile(iccDirectory)
	if err != nil {
		return nil, errors.New("native recovery color profile unavailable")
	}
	return func(ctx context.Context, scheduler *pgxpool.Pool, objects *objectstore.Store, bucket string) (ExpansionJob, func(), error) {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if scheduler == nil || scheduler.Config().MaxConns < 2 || objects == nil || len(bucket) < 3 || len(bucket) > 63 {
			return nil, nil, errors.New("native recovery dependencies invalid")
		}
		var role string
		if err := scheduler.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil || role != "photo_scheduler" {
			return nil, nil, errors.New("native recovery scheduler role unavailable")
		}
		cfg, err := pgxpool.ParseConfig(sourceDSN)
		if err != nil {
			return nil, nil, errors.New("native recovery source database configuration invalid")
		}
		cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
		cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
		cfg.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
		cfg.MaxConns = 4
		api, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			return nil, nil, errors.New("native recovery source database unavailable")
		}
		if err := api.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil || role != "photo_api" {
			api.Close()
			return nil, nil, errors.New("native recovery source database role unavailable")
		}
		if err := verifyNativeRecoverySourceInstallation(ctx, api); err != nil {
			api.Close()
			return nil, nil, err
		}
		manual := func(m NativeManualLimits) semanticproposal.ManualRasterVerificationLimits {
			return semanticproposal.ManualRasterVerificationLimits{PointChecks: m.PointChecks, EdgeEvaluations: m.EdgeEvaluations, PayloadBytes: m.PayloadBytes}
		}
		factory := semanticproposal.SQLNativeParentRecoveryFactory{API: api, Scheduler: scheduler, Objects: objects, Bucket: bucket, Capacity: semanticproposal.TransferPreparation{Color: color, RAM: limits.RAM, VRAM: limits.VRAM, Scratch: limits.Scratch, Output: limits.Output, MetadataBytes: limits.Metadata}, ReservedBytes: limits.Reserved, ManualLimits: manual(limits.Manual), ManualBatchLimits: manual(limits.ManualBatch), Timeout: time.Duration(limits.TimeoutSeconds) * time.Second}
		return &semanticproposal.SQLNativeRecoveryDispatcher{Parent: semanticproposal.SQLNativeParentRecoveryDispatcher{Factory: factory}, Initial: semanticproposal.SQLNativeInitialRecoveryDispatcher{Factory: factory}}, api.Close, nil
	}, nil
}
