package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"reflect"
	"strings"
	"testing"
)

type downloadOutcomeRows struct {
	pgx.Rows
	values [][]any
	i      int
	closed bool
}

func (r *downloadOutcomeRows) Close()     { r.closed = true }
func (r *downloadOutcomeRows) Err() error { return nil }
func (r *downloadOutcomeRows) Next() bool { return r.i < len(r.values) }
func (r *downloadOutcomeRows) Scan(dest ...any) error {
	for i, x := range r.values[r.i] {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(x))
	}
	r.i++
	return nil
}

type downloadOutcomeDB struct{ rows *downloadOutcomeRows }

func (d downloadOutcomeDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return d.rows, nil
}

func TestDownloadEncodeOutcomesWaitAndValidateCanonicalValues(t *testing.T) {
	tenant, run, version, artifact := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	sha := strings.Repeat("a", 64)
	rows := &downloadOutcomeRows{values: [][]any{{int64(0), version, "ready", (*uuid.UUID)(nil), (*string)(nil), (*int64)(nil), (*string)(nil)}}}
	items, ready, err := (DownloadEncodeOutcomeSource{DB: downloadOutcomeDB{rows}}).Load(context.Background(), tenant, run)
	if err != nil || ready || items != nil || !rows.closed {
		t.Fatal(items, ready, err)
	}
	rows = &downloadOutcomeRows{values: [][]any{{int64(0), version, "succeeded", &artifact, &sha, func() *int64 { x := int64(20); return &x }(), func() *string { x := "photo.jpg"; return &x }()}}}
	items, ready, err = (DownloadEncodeOutcomeSource{DB: downloadOutcomeDB{rows}}).Load(context.Background(), tenant, run)
	if err != nil || !ready || len(items) != 1 || items[0].ArtifactID != artifact {
		t.Fatal(items, ready, err)
	}
}

func TestDownloadPackagingPersistsCanonicalPlan(t *testing.T) {
	q := &expansionQueries{row: expansionRow{values: []any{true}}}
	source := downloadSource(0, 5, "photo.jpg", strings.Repeat("a", 64))
	plan, _ := PlanDownloadVolumes([]DownloadSource{source}, 10)
	ok, err := (DownloadPackagingRepository{DB: q}).Begin(context.Background(), uuid.New(), uuid.New(), plan)
	if err != nil || !ok || len(q.args) != 3 {
		t.Fatal(ok, err, q.args)
	}
}

func TestDownloadPackagingCancelledBeforeIO(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 tenant,run:=uuid.New(),uuid.New()
 if _,_,err:=(DownloadEncodeOutcomeSource{}).Load(ctx,tenant,run);err!=context.Canceled{t.Fatal("outcome cancellation lost",err)}
 if _,err:=(DownloadPackagingRepository{}).Begin(ctx,tenant,run,nil);err!=context.Canceled{t.Fatal("packaging cancellation lost",err)}
 if _,err:=(DownloadArchiveFinalizer{}).Finish(ctx,tenant,run);err!=context.Canceled{t.Fatal("finalizer cancellation lost",err)}
}

func TestDownloadEncodeOutcomesRejectMalformedPendingInventory(t *testing.T) {
 for _,ordinal:=range []int64{-1,1,5000} {
  rows:=&downloadOutcomeRows{values:[][]any{{ordinal,uuid.New(),"running",(*uuid.UUID)(nil),(*string)(nil),(*int64)(nil),(*string)(nil)}}}
  _,ready,err:=(DownloadEncodeOutcomeSource{DB:downloadOutcomeDB{rows}}).Load(context.Background(),uuid.New(),uuid.New())
  if err==nil || ready || !rows.closed {t.Fatal("malformed pending inventory accepted",ordinal,ready,err)}
 }
 rows:=&downloadOutcomeRows{values:[][]any{{int64(0),uuid.Nil,"running",(*uuid.UUID)(nil),(*string)(nil),(*int64)(nil),(*string)(nil)}}}
 if _,_,err:=(DownloadEncodeOutcomeSource{DB:downloadOutcomeDB{rows}}).Load(context.Background(),uuid.New(),uuid.New());err==nil {t.Fatal("missing asset version accepted")}
}

func TestDownloadEncodeFailureWaitsForRemainingItems(t *testing.T) {
 for _,state:=range []string{"running","failed","cancelled","skipped"} {
  rows:=&downloadOutcomeRows{values:[][]any{
   {int64(0),uuid.New(),"failed",(*uuid.UUID)(nil),(*string)(nil),(*int64)(nil),(*string)(nil)},
   {int64(1),uuid.New(),state,(*uuid.UUID)(nil),(*string)(nil),(*int64)(nil),(*string)(nil)},
  }}
  items,ready,err:=(DownloadEncodeOutcomeSource{DB:downloadOutcomeDB{rows}}).Load(context.Background(),uuid.New(),uuid.New())
  if ready || items!=nil || !rows.closed {t.Fatal(items,ready,err)}
  if state=="running" {if err!=nil {t.Fatal("failed before remaining work completed",err)}} else if err!=errDownloadEncodeFailed {t.Fatal("terminal failure lost",err)}
 }
}
func TestDownloadPackagingFailureIsPersisted(t *testing.T) {
 rows:=&downloadOutcomeRows{values:[][]any{{int64(0),uuid.New(),"failed",(*uuid.UUID)(nil),(*string)(nil),(*int64)(nil),(*string)(nil)}}}
 q:=&expansionQueries{row:expansionRow{values:[]any{true}}}
 changed,err:=(DownloadPackagingCoordinator{Outcomes:DownloadEncodeOutcomeSource{DB:downloadOutcomeDB{rows}},Plans:DownloadPackagingRepository{DB:q}}).Advance(context.Background(),uuid.New(),uuid.New())
 if !changed || err!=nil || len(q.args)!=2 {t.Fatal(changed,err,q.args)}
}

func TestDownloadMissingSucceededOutputWaitsThenFails(t *testing.T) {
 for _,pending:=range []bool{true,false} {
  state:="skipped";if pending{state="running"}
  rows:=&downloadOutcomeRows{values:[][]any{
   {int64(0),uuid.New(),"succeeded",(*uuid.UUID)(nil),(*string)(nil),(*int64)(nil),(*string)(nil)},
   {int64(1),uuid.New(),state,(*uuid.UUID)(nil),(*string)(nil),(*int64)(nil),(*string)(nil)},
  }}
  items,ready,err:=(DownloadEncodeOutcomeSource{DB:downloadOutcomeDB{rows}}).Load(context.Background(),uuid.New(),uuid.New())
  if ready||items!=nil{t.Fatal(items,ready,err)}
  if pending{if err!=nil{t.Fatal(err)}}else if err!=errDownloadEncodeFailed{t.Fatal("lost output did not fail",err)}
 }
}
