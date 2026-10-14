package workflow
import (
 "context"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/objectstore"
)
// Executable composition avoids the interchange -> workflow dependency cycle.
type SchedulerArchiveCompletionFactory interface{
 BuildArchiveCompletion(context.Context,*pgxpool.Pool,*objectstore.Store,string)(ExpansionJob,error)
}

// Optional companion only fences expired uploads; it never deletes objects or
// releases quota. Cleanup requires a separately installed writer authority.
type SchedulerArchiveExpiryFactory interface{
 BuildArchiveExpiry(context.Context,*pgxpool.Pool)(ExpansionJob,error)
}

type SchedulerArchiveCleanupFactory interface{
 BuildArchiveCleanup(context.Context,*pgxpool.Pool,*objectstore.Store,string)(ExpansionJob,error)
}

type SchedulerArchiveRecoveryFactory interface{
 BuildArchiveRecovery(context.Context,*pgxpool.Pool,*objectstore.Store,string)(ExpansionJob,error)
}

type SchedulerArchiveRevocationFactory interface{
 BuildArchiveRevocation(context.Context,*pgxpool.Pool,*objectstore.Store,string)(ExpansionJob,error)
}

type SchedulerArchiveInvalidationFactory interface{
 BuildArchiveInvalidation(context.Context,*pgxpool.Pool)(ExpansionJob,error)
}
