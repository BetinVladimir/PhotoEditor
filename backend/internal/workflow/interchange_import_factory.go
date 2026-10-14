package workflow
import (
 "context"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/objectstore"
)
// Optional startup installation supplied from the executable to avoid the
// interchange -> workflow canonical ingest dependency becoming a cycle.
// A configured factory must return a complete qualified continuation or fail.
type SchedulerInterchangeImportFactory interface{
 BuildSchedulerInterchangeImport(context.Context,*pgxpool.Pool,*objectstore.Store,string)(InterchangeImportContinuation,error)
}
