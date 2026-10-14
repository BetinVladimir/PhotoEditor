package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type RelightQualificationCatalog interface {
	ResolveRelightQualification(context.Context, auth.Identity, uuid.UUID, uuid.UUID, RelightQualificationPin) (string, objectstore.Object, objectstore.Object, error)
}

// LoadRelightQualificationEvidence reads exact catalog-owned object versions
// outside SQL transactions and rechecks live authority around every read.
// Expected must come from deployment configuration, never the request body.
func LoadRelightQualificationEvidence(ctx context.Context, catalog RelightQualificationCatalog, objects v2input.SessionAnalysisObjectReader, who auth.Identity, tenant, project uuid.UUID, params RelightParams, pin RelightQualificationPin, expected v2qualification.Expected) (RelightQualificationEvidence, error) {
	var zero RelightQualificationEvidence
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog == nil || objects == nil || tenant == uuid.Nil || project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := pin.Compile(); err != nil {
		return zero, err
	}
	bucket, report, calibration, err := catalog.ResolveRelightQualification(ctx, who, tenant, project, pin)
	if err != nil {
		return zero, err
	}
	if bucket == "" {
		return zero, v2input.ErrInvalid
	}
	checkpoint := func() error {
		b, r, c, e := catalog.ResolveRelightQualification(ctx, who, tenant, project, pin)
		if e != nil {
			return e
		}
		if b != bucket || r != report || c != calibration {
			return v2input.ErrExpired
		}
		return ctx.Err()
	}
	var bodies [2][]byte
	for i, obj := range []objectstore.Object{report, calibration} {
		ref := pin.Report
		if i == 1 {
			ref = pin.Calibration
		}
		if obj.SHA256 != ref.SHA256 || obj.Bytes < 2 || obj.Bytes > 1048576 || obj.ContentType != "application/json" || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" {
			return zero, v2input.ErrInvalid
		}
		if err = checkpoint(); err != nil {
			return zero, err
		}
		reader, e := objects.Open(ctx, obj)
		if e != nil {
			return zero, e
		}
		if reader == nil {
			return zero, v2input.ErrInvalid
		}
		raw, readErr := io.ReadAll(io.LimitReader(reader, obj.Bytes+1))
		closeErr := reader.Close()
		if err = ctx.Err(); err != nil {
			return zero, err
		}
		if readErr != nil {
			return zero, readErr
		}
		if closeErr != nil {
			return zero, closeErr
		}
		if int64(len(raw)) != obj.Bytes {
			return zero, v2input.ErrInvalid
		}
		if err = checkpoint(); err != nil {
			return zero, err
		}
		bodies[i] = raw
	}
	if err = VerifyRelightQualificationEvidence(ctx, params, pin, bodies[0], bodies[1], expected); err != nil {
		return zero, err
	}
	if err = checkpoint(); err != nil {
		return zero, err
	}
	return RelightQualificationEvidence{Report: bodies[0], Calibration: bodies[1]}, nil
}
