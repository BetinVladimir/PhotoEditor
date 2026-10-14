package workflow

import (
 "bytes"
 "context"
 "encoding/json"
 "io"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

// ReadSavedWatermarkContinuationParams requires a tenant-scoped query adapter.
// A saved ref proves publication only; it never implies activation or adoption.
func ReadSavedWatermarkContinuationParams(ctx context.Context, db artifactjson.RowQuerier, lease delivery.ExpansionLease, child delivery.ExportChildIntent, node CompiledDownloadNode) (*v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if db == nil || (node.Operation != "derivative.sharpen" && node.Operation != "derivative.watermark" && node.Operation != "export.encode") {
		return nil, v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(child.RunID)
	if err != nil {
		return nil, err
	}
	expected, err := v2input.TaskID(workflow, node.Operation, 0)
	if err != nil || expected != node.ID {
		return nil, v2input.ErrInvalid
	}
	sha := renderDerivativeBytesSHA(node.Params)
	var raw []byte
	if err := db.QueryRow(ctx, `SELECT photo.v30_watermark_saved_continuation_params($1,$2,$3,$4,$5)`, lease.Expansion, lease.Token, child.SlotIndex, node.Operation, sha).Scan(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	if len(raw) > 512 {
		return nil, v2input.ErrInvalid
	}
	var ref v2input.Ref
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&ref) != nil || decoder.Decode(new(any)) != io.EOF || ref.ArtifactID == uuid.Nil || ref.SHA256 != sha {
		return nil, v2input.ErrInvalid
	}
	return &ref, nil
}
