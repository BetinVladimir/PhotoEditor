package maintenance

import (
 "context"
 "errors"
 "strings"
 "testing"
 "time"

 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
)

type historicalReportDB struct {tx *historicalReportTx;calls int;options pgx.TxOptions;deadline time.Time}
func(d *historicalReportDB)BeginTx(ctx context.Context,options pgx.TxOptions)(pgx.Tx,error){d.calls++;d.options=options;d.deadline,_=ctx.Deadline();return d.tx,nil}
type historicalReportTx struct {pgx.Tx;operator bool;entries []HistoricalGCEntry;probeErrors []error;probeBytes []int64;probes int;rollbacks int;commits int;sql []string;cancel context.CancelFunc}
func(t *historicalReportTx)QueryRow(_ context.Context,query string,_ ...any)pgx.Row{t.sql=append(t.sql,query);return historicalReportRow{apply:func(dest []any)error{*dest[0].(*bool)=t.operator;*dest[1].(*time.Time)=time.Unix(1,0);return nil}}}
func(t *historicalReportTx)Exec(_ context.Context,query string,_ ...any)(pgconn.CommandTag,error){t.sql=append(t.sql,query);return pgconn.CommandTag{},nil}
func(t *historicalReportTx)Query(_ context.Context,query string,_ ...any)(pgx.Rows,error){t.sql=append(t.sql,query);return &historicalReportRows{entries:t.entries},nil}
func(t *historicalReportTx)Begin(context.Context)(pgx.Tx,error){index:=t.probes;t.probes++;return &historicalReportProbe{parent:t,index:index},nil}
func(t *historicalReportTx)Rollback(context.Context)error{t.rollbacks++;return nil}
func(t *historicalReportTx)Commit(context.Context)error{t.commits++;return nil}
type historicalReportProbe struct {pgx.Tx;parent *historicalReportTx;index int}
func(p *historicalReportProbe)QueryRow(_ context.Context,query string,_ ...any)pgx.Row{p.parent.sql=append(p.parent.sql,query);return historicalReportRow{apply:func(dest []any)error{if p.parent.cancel!=nil{p.parent.cancel()};if p.parent.probeErrors[p.index]!=nil{return p.parent.probeErrors[p.index]};*dest[0].(*int64)=p.parent.probeBytes[p.index];return nil}}}
func(p *historicalReportProbe)Rollback(context.Context)error{p.parent.rollbacks++;return nil}
type historicalReportRow struct {apply func([]any)error}
func(r historicalReportRow)Scan(dest ...any)error{return r.apply(dest)}
type historicalReportRows struct {pgx.Rows;entries []HistoricalGCEntry;index int}
func(r *historicalReportRows)Next()bool{if r.index>=len(r.entries){return false};r.index++;return true}
func(r *historicalReportRows)Scan(dest ...any)error{entry:=r.entries[r.index-1];*dest[0].(*string)=entry.ArtifactID;*dest[1].(*int64)=entry.CatalogBytes;return nil}
func(r *historicalReportRows)Err()error{return nil}
func(r *historicalReportRows)Close(){}
const historicalTenant="52a488cc-2fbc-4c17-86ab-03282ece9bad"
func TestHistoricalReportReadOnlyBoundedBatchAndClassification(t *testing.T){
 tx:=&historicalReportTx{operator:true,entries:[]HistoricalGCEntry{{ArtifactID:"a",CatalogBytes:12},{ArtifactID:"b",CatalogBytes:5},{ArtifactID:"c",CatalogBytes:7}},probeBytes:[]int64{12,0},probeErrors:[]error{nil,&pgconn.PgError{Code:"23514",Message:"ARTIFACT_ACCOUNTING_PROVENANCE_UNKNOWN"}}}
 db:=&historicalReportDB{tx:tx}
 report,err:=ReadHistoricalGCReport(context.Background(),db,historicalTenant,"",2)
 if err!=nil{t.Fatal(err)}
 if db.options.AccessMode!=pgx.ReadOnly||db.options.IsoLevel!=pgx.RepeatableRead{t.Fatal("report not readonly snapshot")}
 if db.deadline.IsZero()||time.Until(db.deadline)>5*time.Second{t.Fatal("unbounded report")}
 if len(report.Entries)!=2||report.NextAfter!="b"||tx.probes!=2{t.Fatal("batch/cursor escaped")}
 if report.Entries[0].Classification!="exact_charge_provenance"||*report.Entries[0].ProvableReceiptBytes!=12||report.Entries[1].Classification!="unknown_provenance"{t.Fatal("incorrect provenance")}
 if report.EvidenceLimit==""||tx.commits!=1||tx.rollbacks<2{t.Fatal("missing evidence/savepoint boundary")}
 for _,query:=range tx.sql{if strings.Contains(query,"UPDATE ")||strings.Contains(query,"finish_artifact_gc")||strings.Contains(query,"SET ROLE"){t.Fatal("report attempted mutation/escalation")}}
}
func TestHistoricalReportRecognizesOnlyExactExpectedRefusals(t *testing.T){
 for message,want:=range map[string]string{"ARTIFACT_ACCOUNTING_DEDICATED_INGEST":"dedicated_ingest","ARTIFACT_ACCOUNTING_PROVENANCE_UNKNOWN":"unknown_provenance","ARTIFACT_ACCOUNTING_RECEIPT_MISMATCH":"receipt_mismatch","ARTIFACT_ACCOUNTING_PROVENANCE_AMBIGUOUS":"ambiguous_receipts"}{got,ok:=historicalGCClassification(&pgconn.PgError{Code:"23514",Message:message});if !ok||got!=want{t.Fatal(message)}}
 for _,err:=range []error{context.DeadlineExceeded,&pgconn.PgError{Code:"23514",Message:"other"},&pgconn.PgError{Code:"42501",Message:"ARTIFACT_ACCOUNTING_PROVENANCE_UNKNOWN"}}{if _,ok:=historicalGCClassification(err);ok{t.Fatal("database failure hidden")}}
}
func TestHistoricalReportRefusesInvalidScopeAndCancelledAdmission(t *testing.T){
 db:=&historicalReportDB{}
 for _,input:=range []struct{tenant,after string;limit int}{{"","",1},{historicalTenant,"bad",1},{historicalTenant,"",0},{historicalTenant,"",101}}{if _,err:=ReadHistoricalGCReport(context.Background(),db,input.tenant,input.after,input.limit);err==nil{t.Fatal("bad scope accepted")}}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=ReadHistoricalGCReport(ctx,db,historicalTenant,"",1);!errors.Is(err,context.Canceled)||db.calls!=0{t.Fatal("cancelled/invalid report reached database")}
}
func TestHistoricalReportRefusesRuntimeRoleAndLateCancellation(t *testing.T){
 tx:=&historicalReportTx{}
 if _,err:=ReadHistoricalGCReport(context.Background(),&historicalReportDB{tx:tx},historicalTenant,"",1);err==nil||tx.commits!=0{t.Fatal("runtime role accepted")}
 ctx,cancel:=context.WithCancel(context.Background());defer cancel()
 tx=&historicalReportTx{operator:true,entries:[]HistoricalGCEntry{{ArtifactID:"a",CatalogBytes:1}},probeBytes:[]int64{1},probeErrors:[]error{nil},cancel:cancel}
 report,err:=ReadHistoricalGCReport(ctx,&historicalReportDB{tx:tx},historicalTenant,"",1)
 if !errors.Is(err,context.Canceled)||len(report.Entries)!=0||tx.commits!=0{t.Fatal("late cancellation escaped")}
}
func TestHistoricalReportDoesNotHideProbeFailureOrInvalidBytes(t *testing.T){
 expected:=errors.New("connection lost")
 for _,probe:=range []struct{err error;bytes int64}{{expected,0},{nil,2}}{
  tx:=&historicalReportTx{operator:true,entries:[]HistoricalGCEntry{{ArtifactID:"a",CatalogBytes:1}},probeBytes:[]int64{probe.bytes},probeErrors:[]error{probe.err}}
  report,err:=ReadHistoricalGCReport(context.Background(),&historicalReportDB{tx:tx},historicalTenant,"",1)
  if err==nil||len(report.Entries)!=0||tx.commits!=0{t.Fatal("failed/invalid byte proof accepted")}
  if probe.err!=nil&&!errors.Is(err,expected){t.Fatal("database failure discarded")}
 }
}
