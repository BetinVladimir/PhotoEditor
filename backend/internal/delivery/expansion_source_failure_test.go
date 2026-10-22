package delivery

import (
 "context"
 "errors"
 "strings"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)

type cancelledPhysicalFinalizerDB struct { cancel context.CancelFunc }
type cancelledPhysicalFinalizerRow struct { cancel context.CancelFunc }
func(d cancelledPhysicalFinalizerDB)QueryRow(context.Context,string,...any)pgx.Row{return cancelledPhysicalFinalizerRow{d.cancel}}
func(r cancelledPhysicalFinalizerRow)Scan(dest ...any)error{*dest[0].(*bool)=true;r.cancel();return nil}

func TestPhysicalSourceFinalizerDoesNotAcknowledgeAfterContextCancellation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());defer cancel()
 store:=ExpansionLeaseStore{DB:cancelledPhysicalFinalizerDB{cancel},Instance:uuid.New(),Bucket:"photos"}
 accepted,err:=store.FailPhysicalSource(ctx,validExpansionLease(),v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},"pinned-version","SOURCE_OBJECT_MISSING",0,"{}",false)
 if accepted||!errors.Is(err,context.Canceled){t.Fatal(accepted,err)}
}
