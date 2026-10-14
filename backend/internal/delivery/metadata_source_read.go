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

type MetadataSourceReadScope struct {
 Tenant,ParentRun uuid.UUID
 Binding v2input.SessionVersionBinding
 Metadata v2input.Ref
 Policy string
 ApprovedFields []string
}
type MetadataSourceStore struct{DB ProofTransaction}
func NewMetadataSourceStore(db MetadataTransactionBeginner,tenant uuid.UUID)(MetadataSourceStore,error){
 if db==nil||tenant==uuid.Nil{return MetadataSourceStore{},v2input.ErrInvalid}
 return MetadataSourceStore{MetadataTenantQueries{DB:db,Tenant:tenant}},nil
}
// Load repeats live publication evidence then restores owned canonical bytes
// against external child binding/policy/approval. It performs no object IO.
func(s MetadataSourceStore)Load(ctx context.Context,scope MetadataSourceReadScope)(MetadataSourcePublication,error){
 var zero MetadataSourcePublication
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.DB==nil||scope.Tenant==uuid.Nil||scope.ParentRun==uuid.Nil||scope.Metadata.ArtifactID==uuid.Nil||
 !shaPattern.MatchString(scope.Metadata.SHA256)||scope.Binding.AssetVersionID==uuid.Nil||
 scope.Binding.Source.ArtifactID==uuid.Nil||!shaPattern.MatchString(scope.Binding.Source.SHA256)||!shaPattern.MatchString(scope.Binding.CanvasHash)||
 scope.Metadata.ArtifactID==scope.Binding.Source.ArtifactID||scope.Policy!="copyright"&&scope.Policy!="approved_exif"{return zero,v2input.ErrInvalid}
 scope.ApprovedFields=append([]string{},scope.ApprovedFields...)
 if _,err:=ProjectDeliveryMetadata(MetadataDocument{},scope.Policy,scope.ApprovedFields);err!=nil{return zero,err}
 if scope.Policy!="approved_exif"&&len(scope.ApprovedFields)>0{return zero,v2input.ErrInvalid}
 binding,err:=canonical.Marshal(scope.Binding);if err!=nil{return zero,err}
 ref,err:=canonical.Marshal(scope.Metadata);if err!=nil{return zero,err}
 var body,pin string
 err=s.DB.QueryRow(ctx,`SELECT publication_jcs,publication_sha FROM photo.read_v30_metadata_source($1,$2,$3,$4,$5,$6)`,
 scope.Tenant,scope.ParentRun,string(binding),string(ref),scope.Policy,scope.ApprovedFields).Scan(&body,&pin)
 if errors.Is(err,pgx.ErrNoRows){return zero,v2input.ErrExpired}
 if err!=nil{return zero,err}
 if err=ctx.Err();err!=nil{return zero,err}
 if len(body)<2||len(body)>1048576||digest([]byte(body))!=pin{return zero,v2input.ErrInvalid}
 var envelope metadataSourcePublicationEnvelope
 decoder:=json.NewDecoder(bytes.NewReader([]byte(body)));decoder.DisallowUnknownFields()
 if decoder.Decode(&envelope)!=nil||envelope.Scope.Tenant!=scope.Tenant||envelope.Scope.Binding!=scope.Binding||
 envelope.MetadataSource!=scope.Metadata{return zero,v2input.ErrInvalid}
 return RestoreMetadataSourcePublication([]byte(body),pin,envelope.Scope,scope.Metadata,[]byte(envelope.MetadataJCS),scope.Policy,scope.ApprovedFields)
}
