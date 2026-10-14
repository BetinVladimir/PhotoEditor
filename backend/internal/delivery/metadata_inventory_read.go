package delivery

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "io"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
const metadataInventoryEntryQuery=`SELECT entry_json,publication_jcs,publication_sha,snapshot_digest FROM photo.read_v30_metadata_inventory_entry($1,$2,$3,$4,$5,$6,$7,$8)`
type MetadataInventoryEntryStore struct{DB ProofTransaction}
func NewMetadataInventoryEntryStore(db MetadataTransactionBeginner,tenant uuid.UUID)(MetadataInventoryEntryStore,error){
 if db==nil||tenant==uuid.Nil{return MetadataInventoryEntryStore{},v2input.ErrInvalid}
 return MetadataInventoryEntryStore{MetadataTenantQueries{DB:db,Tenant:tenant}},nil
}
// CompileChild reads one fenced parent entry, restores the normalized source
// against external child binding/policy/consent, and compiles closed config.
func(s MetadataInventoryEntryStore)CompileChild(ctx context.Context,tenant,project,expansion,token,exportID uuid.UUID,
 request FrozenExportRequest,policy string,approved []string)([]byte,string,error){
 invalid:=func()([]byte,string,error){return nil,"",v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return nil,"",err}
 if s.DB==nil||tenant==uuid.Nil||project==uuid.Nil||expansion==uuid.Nil||token==uuid.Nil||exportID==uuid.Nil||request.ParentRunID==uuid.Nil||request.Ordinal<0||request.Ordinal>=5000||!shaPattern.MatchString(request.SnapshotDigest)||policy=="strip"{return invalid()}
 fields,err:=normalizeMetadataApproval(policy,approved);if err!=nil{return invalid()}
 binding,err:=canonical.Marshal(request.Binding);if err!=nil{return invalid()}
 var entryRaw,body,pin,snapshot string
 err=s.DB.QueryRow(ctx,metadataInventoryEntryQuery,tenant,request.ParentRunID,expansion,token,request.Ordinal,string(binding),policy,append([]string{},fields...)).Scan(&entryRaw,&body,&pin,&snapshot)
 if errors.Is(err,pgx.ErrNoRows){return nil,"",v2input.ErrExpired};if err!=nil{return nil,"",err}
 if err=ctx.Err();err!=nil{return nil,"",err}
 if len(entryRaw)>8192||len(body)>1048576||snapshot!=request.SnapshotDigest{return invalid()}
 var entry MetadataInventoryEntry;var envelope metadataSourcePublicationEnvelope
 decoder:=json.NewDecoder(bytes.NewReader([]byte(entryRaw)));decoder.DisallowUnknownFields()
 if decoder.Decode(&entry)!=nil||decoder.Decode(new(any))!=io.EOF{return invalid()}
 expectedID,identityErr:=MetadataSourceIdentity(request.ParentRunID,request.Binding,policy,fields)
 if identityErr!=nil||entry.Metadata.ArtifactID!=expectedID{return invalid()}
 if json.Unmarshal([]byte(body),&envelope)!=nil||entry.Ordinal!=request.Ordinal||entry.Binding!=request.Binding||
 envelope.Scope.Tenant!=tenant||envelope.Scope.Project!=project||entry.Metadata!=envelope.MetadataSource||entry.PublicationSHA256!=pin||entry.FrozenSHA256!=envelope.FrozenSHA256{return invalid()}
 publication,err:=RestoreMetadataSourcePublication([]byte(body),pin,envelope.Scope,entry.Metadata,[]byte(envelope.MetadataJCS),policy,fields)
 if err!=nil{return nil,"",err}
 return CompileMetadataChildConfig(project,request,exportID,publication,policy,fields)
}
