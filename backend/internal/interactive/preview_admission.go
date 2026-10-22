package interactive

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
)

type previewAdmissionQuery interface {
 QueryRow(context.Context, string, ...any) pgx.Row
}

// The request's existing tenant transaction supplies scope. Preview executes
// in the media pool with the CPU backend. Resource class and backend are
// separate registry dimensions; a generic CPU-pool worker cannot admit it.
func admitPreviewWorker(ctx context.Context, query previewAdmissionQuery) error {
 if err := ctx.Err(); err != nil { return err }
 var ready bool
 if err := query.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM photo.tenant_capabilities() WHERE operation='editor.preview' AND resource_class='media' AND supported_backends ? 'cpu' AND available_backends ? 'cpu')`).Scan(&ready); err != nil { return err }
 if !ready { return domain.NewError("OPERATION_UNAVAILABLE", 503) }
 return ctx.Err()
}
