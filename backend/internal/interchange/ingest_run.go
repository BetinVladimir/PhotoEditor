package interchange
import("context";"github.com/google/uuid";"photoeditor/backend/internal/auth";"photoeditor/backend/internal/canonical";"photoeditor/backend/internal/domain";"photoeditor/backend/internal/v2input";"photoeditor/backend/internal/workflow")
type importedSnapshotItem struct{Ordinal int64 `json:"ordinal"`;ArtifactID uuid.UUID `json:"artifact_id"`;SHA256 string `json:"sha256"`;Bytes int64 `json:"bytes"`;Payload workflow.IngestSnapshotPayload `json:"payload"`}
// BeginRun creates the server-verified ingest snapshot and binds all source
// identities atomically. Caller owns scheduler tenant transaction.
func(p ImportedIngestPlan)BeginRun(ctx context.Context,tx ProofTransaction,who auth.Identity,tenant,job uuid.UUID,parsed v2input.Ref,body []byte)error{
 if err:=ctx.Err();err!=nil{return err}
 if tx==nil||tenant==uuid.Nil||job==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||parsed.ArtifactID==uuid.Nil||len(body)<2||len(body)>8<<20||rawSHA(body)!=parsed.SHA256||len(p.Spec.Items)<1||len(p.Spec.Items)>5000||p.Spec.RunID!=uuid.NewSHA1(job,[]byte("interchange:canonical-ingest:1")){return domain.NewError("VALIDATION_FAILED",422)}
 registry,err:=workflow.LoadRegistry();if err!=nil{return err};if _,err:=workflow.CompileIngest(p.Spec,registry);err!=nil{return err}
 items:=make([]importedSnapshotItem,0,len(p.Spec.Items))
 for _,item:=range p.Spec.Items{if item.AssumeProfile!=nil||item.Analyze||item.Tile!=512{return domain.NewError("VALIDATION_FAILED",422)};items=append(items,importedSnapshotItem{item.Ordinal,item.Source.ID,item.Source.SHA256,item.Bytes,workflow.IngestSnapshotPayload{SchemaVersion:1,Suffix:item.Suffix,AssumeProfile:nil,Analyze:false,Tile:512}})}
 raw,err:=canonical.Marshal(items);if err!=nil{return err}
 var accepted bool
 err=tx.QueryRow(ctx,`SELECT photo.begin_v30_interchange_ingest($1,$2,$3,$4,$5,$6,$7,$8)`,tenant,who.UserID,who.SessionID,job,parsed.ArtifactID,parsed.SHA256,string(body),string(raw)).Scan(&accepted)
 if err!=nil{return err};if !accepted{return domain.NewError("INTERCHANGE_SOURCE_STALE",409)};return nil
}
