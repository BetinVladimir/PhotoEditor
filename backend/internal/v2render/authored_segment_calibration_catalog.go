package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// PinnedAuthoredSegmentCalibrationCatalog accepts only an independently pinned
// operator installation for this exact segment build. It does not create measured
// evidence or SQL qualification. Deployment must supply genuine measured profiles.
type PinnedAuthoredSegmentCalibrationCatalog struct {
	catalog *PinnedRenderCalibrationCatalog
}

func NewPinnedAuthoredSegmentCalibrationCatalog(raw []byte, expectedSHA string) (*PinnedAuthoredSegmentCalibrationCatalog, error) {
	if len(raw) < 2 || len(raw) > 1048576 {
		return nil, v2input.ErrInvalid
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != expectedSHA {
		return nil, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return nil, v2input.ErrInvalid
	}
	fields, err := renderCalibrationObject(raw, "schema_version", "operation", "operation_version", "kernel_version", "contracts_sha", "calibrations")
	if err != nil {
		return nil, err
	}
	var envelope struct {
		SchemaVersion int             `json:"schema_version"`
		Operation     string          `json:"operation"`
		Version       string          `json:"operation_version"`
		Kernel        string          `json:"kernel_version"`
		Contracts     string          `json:"contracts_sha"`
		Calibrations  json.RawMessage `json:"calibrations"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&envelope) != nil || envelope.SchemaVersion != 1 || envelope.Operation != "render.authored_segment" || envelope.Version != "1" || envelope.Kernel != AuthoredSegmentKernelVersion || envelope.Contracts != AuthoredSegmentSchemasSHA {
		return nil, v2input.ErrInvalid
	}
	nested := fields["calibrations"]
	nestedSHA := sha256.Sum256(nested)
	catalog, err := NewPinnedRenderCalibrationCatalog(nested, hex.EncodeToString(nestedSHA[:]))
	if err != nil {
		return nil, err
	}
	for qualification := range catalog.profiles {
		if qualification.Backend != "cpu" {
			return nil, v2input.ErrInvalid
		}
	}
	return &PinnedAuthoredSegmentCalibrationCatalog{catalog: catalog}, nil
}

func (c *PinnedAuthoredSegmentCalibrationCatalog) ResolveAuthoredSegment(ctx context.Context, q RenderQualification) (RenderCalibration, error) {
	if err := ctx.Err(); err != nil {
		return RenderCalibration{}, err
	}
	if c == nil || c.catalog == nil || q.Backend != "cpu" {
		return RenderCalibration{}, renderUnavailable()
	}
	return c.catalog.Resolve(ctx, q)
}

var _ AuthoredSegmentCalibrationCatalog = (*PinnedAuthoredSegmentCalibrationCatalog)(nil)
