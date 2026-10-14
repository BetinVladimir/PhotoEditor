package delivery

import (
 "context"
 "encoding/json"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
const metadataSourceRetryQuery=`SELECT outcome,publication_jcs,publication_sha FROM photo.read_v30_metadata_source_retry($1,$2,$3,$4,$5,$6,$7,$8,$9)`
// ReadMetadataSourceRetry requires a freshly resolved original scope. A saved
// publication may have older session provenance, but all other scope fields
// must equal current independently resolved authority.
func ReadMetadataSourceRetry(ctx context.Context,db ProofTransaction,run uuid.UUID,current MetadataSourcePublicationScope,policy string,approved []string)(MetadataSourcePublication,bool,error){
 var zero MetadataSourcePublication
 if err:=ctx.Err();err!=nil{return zero,false,err}
 if db==nil||current.Session==uuid.Nil{return zero,false,v2input.ErrInvalid}
 fields,err:=normalizeMetadataApproval(policy,approved);if err!=nil{return zero,false,err}
 id,err:=MetadataSourceIdentity(run,current.Binding,policy,fields);if err!=nil{return zero,false,err}
 binding,err:=canonical.Marshal(current.Binding);if err!=nil{return zero,false,err}
 var outcome string;var raw,pin *string
 err=db.QueryRow(ctx,metadataSourceRetryQuery,current.Tenant,current.Project,run,current.Actor,current.Session,id,string(binding),policy,append([]string{},fields...)).Scan(&outcome,&raw,&pin)
 if errors.Is(err,pgx.ErrNoRows){return zero,false,v2input.ErrExpired};if err!=nil{return zero,false,err}
 if err=ctx.Err();err!=nil{return zero,false,err}
 if outcome=="absent"&&raw==nil&&pin==nil{return zero,false,nil}
 if outcome!="saved"||raw==nil||pin==nil||len(*raw)>1048576{return zero,false,v2input.ErrInvalid}
 var envelope metadataSourcePublicationEnvelope
 if json.Unmarshal([]byte(*raw),&envelope)!=nil{return zero,false,v2input.ErrInvalid}
 expected:=current;expected.Session=envelope.Scope.Session
 if envelope.Scope!=expected||envelope.MetadataSource.ArtifactID!=id{return zero,false,v2input.ErrInvalid}
 plan,err:=RestoreMetadataSourcePublication([]byte(*raw),*pin,expected,envelope.MetadataSource,[]byte(envelope.MetadataJCS),policy,fields)
 if err!=nil{return zero,false,err};return plan,true,nil
}
