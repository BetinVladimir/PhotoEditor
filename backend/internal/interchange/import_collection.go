package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

// PrepareImportCollection runs in a scheduler tenant transaction after canonical
// ingest. Nil UUID means pending/stale authority; no partial collection is exposed.
func PrepareImportCollection(ctx context.Context, tx ProofTransaction, tenant, job uuid.UUID, parsed v2input.Ref, body []byte) (uuid.UUID,error) {
 if err:=ctx.Err();err!=nil{return uuid.Nil,err}
 if tx==nil||tenant==uuid.Nil||job==uuid.Nil||parsed.ArtifactID==uuid.Nil||len(body)<2||len(body)>8<<20||rawSHA(body)!=parsed.SHA256 {
  return uuid.Nil,domain.NewError("VALIDATION_FAILED",422)
 }
 var result *uuid.UUID
 err:=tx.QueryRow(ctx,`SELECT photo.prepare_v30_interchange_draft_collection($1,$2,$3)`,tenant,job,string(body)).Scan(&result)
 if err!=nil{return uuid.Nil,err}
 if result==nil{return uuid.Nil,nil}
 if *result!=uuid.NewSHA1(job,[]byte("interchange:draft-collection:1")){return uuid.Nil,domain.NewError("INTERCHANGE_COLLECTION_CONFLICT",409)}
 return *result,nil
}
