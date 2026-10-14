package v2portrait

import (
	"context"
	"io"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// RelightAnalysisQualificationCatalog must authenticate the session, editor ACL,
// imported issuer and current enabled admission policy before returning the
// immutable object version. Implementations must not resolve arbitrary refs.
type RelightAnalysisQualificationCatalog interface {
	ResolveRelightAnalysisQualification(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, RelightAnalysisRuntime) (string, objectstore.Object, error)
}

// CompileQualifiedRelightAnalysisPlan requires original report bytes and a
// deployment-owned acceptance policy. This proves content only; current catalog
// authority and complete input closure remain required when admitting the task.
func CompileQualifiedRelightAnalysisPlan(ctx context.Context, request RelightAnalysisPlanRequest, reportRaw []byte, expected v2qualification.Expected) (RelightAnalysisPlan, error) {
	var zero RelightAnalysisPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	runtime := request.Runtime
	if expected.Operation != request.Operation || expected.Version != "1" || expected.Backend != runtime.Backend || expected.ModelLockSHA != runtime.ModelLockSHA || expected.RuntimeLockSHA != runtime.RuntimeLockSHA || expected.DeviceProfileSHA != runtime.DeviceProfileSHA {
		return zero, v2qualification.ErrUnqualified
	}
	report, err := v2qualification.Verify(reportRaw, runtime.Qualification.SHA256, expected)
	if err != nil {
		return zero, err
	}
	// Measured qualification peaks must fit the resource reservation; a valid
	// report must not be used to bypass scheduler memory or scratch accounting.
	if report.Metrics.PeakVRAMBytes > float64(runtime.Resources.VRAMBytes) || report.Metrics.PeakRSSBytes > float64(runtime.Resources.RAMBytes) || report.Metrics.ScratchBytes > float64(runtime.Resources.ScratchBytes) {
		return zero, v2qualification.ErrUnqualified
	}
	return CompileRelightAnalysisPlan(ctx, request)
}

// LoadQualifiedRelightAnalysisPlan brackets bounded versioned object IO with
// authenticated catalog resolution. SQL transactions must end before Open.
func LoadQualifiedRelightAnalysisPlan(ctx context.Context, catalog RelightAnalysisQualificationCatalog, objects v2input.SessionAnalysisObjectReader, request RelightAnalysisPlanRequest, expected v2qualification.Expected) (RelightAnalysisPlan, error) {
	var zero RelightAnalysisPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog == nil || objects == nil || request.Tenant == uuid.Nil || request.Project == uuid.Nil || request.Actor == uuid.Nil || request.Session == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	who := auth.Identity{UserID: request.Actor, SessionID: request.Session}
	resolve := func() (string, objectstore.Object, error) {
		return catalog.ResolveRelightAnalysisQualification(ctx, who, request.Tenant, request.Project, request.Operation, request.Runtime)
	}
	bucket, object, err := resolve()
	if err != nil {
		return zero, err
	}
	if bucket == "" || object.Key == "" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 || object.ContentType != "application/json" || object.Bytes < 2 || object.Bytes > 1048576 || object.SHA256 != request.Runtime.Qualification.SHA256 {
		return zero, v2input.ErrInvalid
	}
	checkpoint := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		b, o, e := resolve()
		if e != nil {
			return e
		}
		if b != bucket || o != object {
			return v2input.ErrExpired
		}
		return ctx.Err()
	}
	if err = checkpoint(); err != nil {
		return zero, err
	}
	reader, err := objects.Open(ctx, object)
	if err != nil {
		return zero, err
	}
	if reader == nil {
		return zero, v2input.ErrInvalid
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, object.Bytes+1))
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
	if int64(len(raw)) != object.Bytes {
		return zero, v2input.ErrInvalid
	}
	if err = checkpoint(); err != nil {
		return zero, err
	}
	plan, err := CompileQualifiedRelightAnalysisPlan(ctx, request, raw, expected)
	if err != nil {
		return zero, err
	}
	if err = checkpoint(); err != nil {
		return zero, err
	}
	return plan, nil
}
