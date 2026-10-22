package maintenance

import (
 "context"
 "errors"
 "fmt"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
)

// HistoricalGCReportDB must be an operator-only pool, never the runtime pool.
// Its connections must already select photo_fn_owner. No role escalation occurs.
type HistoricalGCReportDB interface { BeginTx(context.Context, pgx.TxOptions) (pgx.Tx,error) }

type HistoricalGCEntry struct {
 ArtifactID string `json:"artifact_id"`
 CatalogBytes int64 `json:"catalog_bytes"`
 Classification string `json:"classification"`
 ProvableReceiptBytes *int64 `json:"provable_receipt_bytes,omitempty"`
}

type HistoricalGCReport struct {
 TenantID string `json:"tenant_id"`
 ObservedAt time.Time `json:"observed_at"`
 Entries []HistoricalGCEntry `json:"entries"`
 NextAfter string `json:"next_after,omitempty"`
 // A matching receipt proves publication charge provenance, never that the
 // historical debit was omitted. This report cannot authorize another debit.
 EvidenceLimit string `json:"evidence_limit"`
}

func historicalGCClassification(err error) (string,bool) {
 var failure *pgconn.PgError
 if !errors.As(err,&failure)||failure.Code!="23514" {return "",false}
 switch failure.Message {
 case "ARTIFACT_ACCOUNTING_DEDICATED_INGEST":return "dedicated_ingest",true
 case "ARTIFACT_ACCOUNTING_PROVENANCE_UNKNOWN":return "unknown_provenance",true
 case "ARTIFACT_ACCOUNTING_RECEIPT_MISMATCH":return "receipt_mismatch",true
 case "ARTIFACT_ACCOUNTING_PROVENANCE_AMBIGUOUS":return "ambiguous_receipts",true
 default:return "",false
 }
}

// ReadHistoricalGCReport inspects one tenant and at most 100 artifacts within
// five seconds, in a consistent read-only snapshot. It never calls a cleaner,
// updates accounting or infers a missing historical debit from marker=false.
func ReadHistoricalGCReport(ctx context.Context, db HistoricalGCReportDB, tenant, after string, limit int) (HistoricalGCReport,error) {
 empty:=HistoricalGCReport{}
 if err:=ctx.Err();err!=nil{return empty,err}
 tenantID,err:=uuid.Parse(tenant)
 if err!=nil||tenantID==uuid.Nil{return empty,errors.New("valid tenant required")}
 afterID:=uuid.Nil
 if after!="" {afterID,err=uuid.Parse(after);if err!=nil{return empty,errors.New("valid artifact cursor required")}}
 if db==nil||limit<1||limit>100{return empty,errors.New("operator database and batch limit 1..100 required")}
 ctx,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel()
 tx,err:=db.BeginTx(ctx,pgx.TxOptions{IsoLevel:pgx.RepeatableRead,AccessMode:pgx.ReadOnly})
 if err!=nil{return empty,err}
 defer func(){cleanup,cancel:=context.WithTimeout(context.Background(),time.Second);defer cancel();_ = tx.Rollback(cleanup)}()
 var operator bool
 report:=HistoricalGCReport{TenantID:tenantID.String(),Entries:[]HistoricalGCEntry{},EvidenceLimit:"Current receipt provenance does not prove whether a historical storage debit already occurred; no automatic correction is safe."}
 if err=tx.QueryRow(ctx,"SELECT current_user='photo_fn_owner',transaction_timestamp()").Scan(&operator,&report.ObservedAt);err!=nil{return empty,err}
 if !operator{return empty,errors.New("historical accounting report requires dedicated photo_fn_owner operator connection")}
 // A database statement bound accompanies context cancellation, including
 // per-artifact helper calls. SET LOCAL is transaction configuration, not data.
 if _,err=tx.Exec(ctx,"SET LOCAL statement_timeout='4s'");err!=nil{return empty,err}
 if _,err=tx.Exec(ctx,"SET LOCAL lock_timeout='500ms'");err!=nil{return empty,err}
 rows,err:=tx.Query(ctx,`SELECT id::text,bytes FROM photo.artifacts
 WHERE tenant_id=$1 AND state='deleted' AND NOT gc_storage_settled AND id>$2
 ORDER BY id LIMIT $3`,tenantID.String(),afterID.String(),limit+1)
 if err!=nil{return empty,err}
 for rows.Next(){var entry HistoricalGCEntry;if err=rows.Scan(&entry.ArtifactID,&entry.CatalogBytes);err!=nil{rows.Close();return empty,err};report.Entries=append(report.Entries,entry)}
 err=rows.Err();rows.Close();if err!=nil{return empty,err}
 if len(report.Entries)>limit{report.Entries=report.Entries[:limit];report.NextAfter=report.Entries[limit-1].ArtifactID}
 for i:=range report.Entries {
  if err=ctx.Err();err!=nil{return empty,err}
  // An expected refusal aborts its savepoint, not the report snapshot.
  probe,beginErr:=tx.Begin(ctx);if beginErr!=nil{return empty,beginErr}
  var charged int64
  probeErr:=probe.QueryRow(ctx,"SELECT photo.artifact_gc_charged_bytes($1,$2)",tenantID.String(),report.Entries[i].ArtifactID).Scan(&charged)
  rollbackErr:=probe.Rollback(ctx)
  if err=ctx.Err();err!=nil{return empty,err}
  if rollbackErr!=nil{return empty,rollbackErr}
  if probeErr!=nil {
   classification,known:=historicalGCClassification(probeErr)
   if !known{return empty,fmt.Errorf("historical accounting provenance probe: %w",probeErr)}
   report.Entries[i].Classification=classification
  }else{
   if charged<0||charged!=report.Entries[i].CatalogBytes{return empty,errors.New("invalid historical accounting receipt byte count")}
   report.Entries[i].Classification="exact_charge_provenance"
   report.Entries[i].ProvableReceiptBytes=new(int64);*report.Entries[i].ProvableReceiptBytes=charged
  }
 }
 if err=ctx.Err();err!=nil{return empty,err}
 if err=tx.Commit(ctx);err!=nil{return empty,err}
 if err=ctx.Err();err!=nil{return empty,err}
 return report,nil
}
