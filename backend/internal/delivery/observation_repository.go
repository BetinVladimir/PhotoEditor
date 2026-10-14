package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// ReadExportObservation reads one exact admitted slot under the parent lease.
// Caller must reread/fence the complete matrix before assembly publication.
func (s ExpansionLeaseStore) ReadExportObservation(ctx context.Context, lease ExpansionLease, plan PreparePlan, child ExportChildIntent) (ExportObservation, error) {
	if err := ctx.Err(); err != nil {
		return ExportObservation{}, err
	}
	if s.DB == nil || !lease.valid() || lease.ParentTask != plan.TaskID() || child.SlotIndex >= lease.Cursor {
		return ExportObservation{}, v2input.ErrInvalid
	}
	if _, err := plan.ChildUsesWatermark(lease.Run, child); err != nil {
		return ExportObservation{}, err
	}
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT photo.v30_delivery_export_observation($1,$2,$3)`, lease.Expansion, lease.Token, child.SlotIndex).Scan(&raw); err != nil {
		return ExportObservation{}, err
	}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ExportObservation{}, domain.NewError("DELIVERY_OBSERVATION_STALE", 409)
	}
	if len(raw) > 16384 {
		return ExportObservation{}, v2input.ErrInvalid
	}
	var observation ExportObservation
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&observation) != nil || decoder.Decode(new(any)) != io.EOF {
		return ExportObservation{}, v2input.ErrInvalid
	}
	export, err := plan.NewChildExportID(lease.Run, child)
	if err != nil {
		return ExportObservation{}, err
	}
	if observation.ExportID != export || observation.Binding != child.Binding || observation.SnapshotDigest != child.SnapshotDigest || observation.PresetSHA != child.Preset.PresetSHA {
		return ExportObservation{}, v2input.ErrInvalid
	}
	return observation, nil
}

// ReadObservationPage batches network IO; SQL still repeats all per-slot checks.
func (s ExpansionLeaseStore) ReadObservationPage(ctx context.Context, lease ExpansionLease, plan PreparePlan, cursor, limit int) ([]ChildObservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil || !lease.valid() || lease.ParentTask != plan.task || cursor < 0 || cursor > lease.Cursor || limit < 1 || limit > 100 {
		return nil, v2input.ErrInvalid
	}
	page, err := plan.ExportChildren(lease.Run, cursor, limit)
	if err != nil {
		return nil, err
	}
	if page.NextCursor > lease.Cursor {
		return nil, v2input.ErrInvalid
	}
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT photo.v30_delivery_observation_page($1,$2,$3,$4)`, lease.Expansion, lease.Token, cursor, limit).Scan(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, domain.NewError("DELIVERY_OBSERVATION_STALE", 409)
	}
	if len(raw) > 2<<20 {
		return nil, v2input.ErrInvalid
	}
	var values []ChildObservation
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&values) != nil || decoder.Decode(new(any)) != io.EOF || values == nil || len(values) != len(page.Items) {
		return nil, v2input.ErrInvalid
	}
	for index, child := range page.Items {
		value := values[index]
		export, err := plan.NewChildExportID(lease.Run, child)
		if err != nil {
			return nil, err
		}
		if value.SlotIndex != child.SlotIndex || value.Export.ExportID != export || value.Export.Binding != child.Binding || value.Export.SnapshotDigest != child.SnapshotDigest || value.Export.PresetSHA != child.Preset.PresetSHA {
			return nil, v2input.ErrInvalid
		}
	}
	return values, nil
}
