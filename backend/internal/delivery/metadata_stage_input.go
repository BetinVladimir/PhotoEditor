package delivery

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type MetadataStageInput struct {
 Config MetadataChildConfig
 Encoded v2input.Ref
 EncodedJCS []byte
 EncodedBytes int64
}
type MetadataStageInputStore struct{DB ProofTransaction}
func NewMetadataStageInputStore(db MetadataTransactionBeginner,tenant uuid.UUID)(MetadataStageInputStore,error){
 if db==nil||tenant==uuid.Nil{return MetadataStageInputStore{},v2input.ErrInvalid}
 return MetadataStageInputStore{MetadataTenantQueries{DB:db,Tenant:tenant}},nil
}
func(s MetadataStageInputStore)Load(ctx context.Context,lease MetadataBarrierLease)(MetadataStageInput,error){
 var zero MetadataStageInput
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.DB==nil||!lease.valid(){return zero,v2input.ErrInvalid}
 var raw,encoded string;var id uuid.UUID;var size int64
 err:=s.DB.QueryRow(ctx,`SELECT child_config,encoded_artifact,encoded_jcs,encoded_bytes FROM photo.read_v30_metadata_stage_input($1,$2,$3,$4)`,
 lease.Tenant,lease.ChildRun,lease.Expansion,lease.Token).Scan(&raw,&id,&encoded,&size)
 if errors.Is(err,pgx.ErrNoRows){return zero,v2input.ErrExpired}
 if err!=nil{return zero,err}
 if err=ctx.Err();err!=nil{return zero,err}
 if len(raw)<2||len(raw)>2<<20||len(encoded)<2||len(encoded)>1048576||id==uuid.Nil||size<1||size>2*1024*1024*1024{return zero,v2input.ErrInvalid}
 var config MetadataChildConfig
 decoder:=json.NewDecoder(bytes.NewReader([]byte(raw)));decoder.DisallowUnknownFields()
 if decoder.Decode(&config)!=nil{return zero,v2input.ErrInvalid}
 // SQL jsonb text need not be JCS, but closed typed reconstruction must match.
 exact,err:=canonical.Marshal(config);if err!=nil{return zero,err}
 normalized,err:=canonical.Bytes([]byte(raw));if err!=nil||!bytes.Equal(exact,normalized){return zero,v2input.ErrInvalid}
 if config.QueueClass!="batch"||config.ProjectID==uuid.Nil||config.ExportID==uuid.Nil||config.Request.ChildRunID!=lease.ChildRun||
 config.Metadata.ArtifactID==uuid.Nil||!shaPattern.MatchString(config.Metadata.SHA256)||
 !shaPattern.MatchString(config.FrozenSHA)||!shaPattern.MatchString(config.PublicationSHA)||config.Approved==nil{return zero,v2input.ErrInvalid}
 body,err:=canonical.Bytes([]byte(encoded));if err!=nil||!bytes.Equal(body,[]byte(encoded)){return zero,v2input.ErrInvalid}
 return MetadataStageInput{config,v2input.Ref{ArtifactID:id,SHA256:digest(body)},body,size},nil
}
