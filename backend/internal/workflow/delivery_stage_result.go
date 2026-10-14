package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

type DeliveryStageOutput struct {
	Ref       v2input.Ref `json:"ref"`
	Role      string      `json:"role"`
	Bucket    string      `json:"bucket"`
	Key       string      `json:"object_key"`
	Version   string      `json:"object_version"`
	Bytes     int64       `json:"bytes"`
	MediaType string      `json:"media_type"`
}
type DeliveryStageResult struct {
	TaskID   uuid.UUID             `json:"task_id"`
	ResultID uuid.UUID             `json:"result_id"`
	Attempt  int                   `json:"attempt"`
	Fence    int64                 `json:"fence"`
	Outputs  []DeliveryStageOutput `json:"outputs"`
}

// ReadDeliveryStageResult discovers fenced producer metadata only. Physical
// output verification and dedicated adoption must precede continuation admission.
func ReadDeliveryStageResult(ctx context.Context, db artifactjson.RowQuerier, l delivery.ExpansionLease, p delivery.PreparePlan, c delivery.ExportChildIntent, operation, bucket string) (*DeliveryStageResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if db == nil || l.ParentTask != p.TaskID() || (operation != "derivative.resize" && operation != "derivative.sharpen") || bucket == "" {
		return nil, v2input.ErrInvalid
	}
	if _, _, err := p.ExportRequest(l.Run, c); err != nil {
		return nil, err
	}
	var raw []byte
	if err := db.QueryRow(ctx, `SELECT photo.v30_delivery_stage_result($1,$2,$3,$4)`, l.Expansion, l.Token, c.SlotIndex, operation).Scan(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	workflow, err := v2input.WorkflowID(c.RunID)
	if err != nil {
		return nil, err
	}
	task, err := v2input.TaskID(workflow, operation, 0)
	if err != nil {
		return nil, err
	}
	return parseDeliveryStageResult(raw, task, bucket)
}
func parseDeliveryStageResult(raw []byte, task uuid.UUID, bucket string) (*DeliveryStageResult, error) {
	if len(raw) > 16384 {
		return nil, v2input.ErrInvalid
	}
	var result DeliveryStageResult
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || result.TaskID != task || task == uuid.Nil || result.ResultID == uuid.Nil || result.Attempt < 1 || result.Fence < 1 || len(result.Outputs) < 3 || len(result.Outputs) > 4 {
		return nil, v2input.ErrInvalid
	}
	roles := map[string]bool{}
	ids := map[uuid.UUID]bool{}
	for _, o := range result.Outputs {
		if o.Ref.ArtifactID == uuid.Nil || ids[o.Ref.ArtifactID] || roles[o.Role] || len(o.Ref.SHA256) != 64 || o.Bucket != bucket || o.Key == "" || o.Version == "" || o.Version == "null" || o.Bytes < 1 {
			return nil, v2input.ErrInvalid
		}
		for _, ch := range o.Ref.SHA256 {
			if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
				return nil, v2input.ErrInvalid
			}
		}
		switch o.Role {
		case "working_image", "derivative_report":
			if o.MediaType != "application/json" || o.Bytes > 1<<20 {
				return nil, v2input.ErrInvalid
			}
		case "working_rgb", "working_alpha":
			if o.MediaType != "application/octet-stream" {
				return nil, v2input.ErrInvalid
			}
		default:
			return nil, v2input.ErrInvalid
		}
		ids[o.Ref.ArtifactID] = true
		roles[o.Role] = true
	}
	if !roles["working_image"] || !roles["derivative_report"] || !roles["working_rgb"] {
		return nil, v2input.ErrInvalid
	}
	return &result, nil
}
