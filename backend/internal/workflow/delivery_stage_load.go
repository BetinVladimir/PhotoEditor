package workflow

import (
	"bytes"
	"context"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

// VerifiedDeliveryStage is local read evidence, never a durable adoption receipt.
// Commit must repeat producer/fence/current authority inside its SQL transaction.
type VerifiedDeliveryStage struct {
	producer DeliveryStageResult
	output   DeliveryPresentationOutput
}

func (v VerifiedDeliveryStage) Producer() DeliveryStageResult {
	result := v.producer
	result.Outputs = append([]DeliveryStageOutput(nil), result.Outputs...)
	return result
}
func (v VerifiedDeliveryStage) Output() DeliveryPresentationOutput {
	result := v.output
	result.ImageJSON = append([]byte(nil), result.ImageJSON...)
	result.ReportJSON = append([]byte(nil), result.ReportJSON...)
	return result
}

// LoadVerifiedDeliveryStage compiles the expected pending operation from the
// frozen prefix, reads fenced metadata, verifies physical output outside SQL,
// then rereads the producer under current authority. Any change or NULL after
// IO refuses the evidence. This cannot close the final read-to-commit race.
func LoadVerifiedDeliveryStage(ctx context.Context, db artifactjson.RowQuerier, objects v2input.SessionAnalysisObjectReader, bucket string, lease delivery.ExpansionLease, plan delivery.PreparePlan, child delivery.ExportChildIntent, source []byte, prefix []DeliveryPresentationOutput, targetICC string, registry Registry, limits DeliveryDerivativeLimits) (*VerifiedDeliveryStage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if db == nil || objects == nil {
		return nil, v2input.ErrInvalid
	}
	next, err := CompileDeliveryContinuation(plan, lease.Run, child, source, prefix, targetICC, registry, limits)
	if err != nil {
		return nil, err
	}
	if next.Node.Operation != "derivative.resize" && next.Node.Operation != "derivative.sharpen" {
		return nil, v2input.ErrInvalid
	}
	producer, err := ReadDeliveryStageResult(ctx, db, lease, plan, child, next.Node.Operation, bucket)
	if err != nil || producer == nil {
		return nil, err
	}
	expected, err := canonical.Marshal(producer)
	if err != nil {
		return nil, err
	}
	output, err := VerifyDeliveryStageOutput(ctx, objects, bucket, next.Node, next.SourceJCS, *producer)
	if err != nil {
		return nil, err
	}
	fresh, err := ReadDeliveryStageResult(ctx, db, lease, plan, child, next.Node.Operation, bucket)
	if err != nil {
		return nil, err
	}
	if fresh == nil {
		return nil, v2input.ErrInvalid
	}
	actual, err := canonical.Marshal(fresh)
	if err != nil || !bytes.Equal(expected, actual) {
		return nil, v2input.ErrInvalid
	}
	return &VerifiedDeliveryStage{producer: *fresh, output: output}, nil
}
