package delivery

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/v2input"
)

type metadataTransportBody struct{ref v2input.Ref;body []byte}
type metadataTransportCompiler struct{task uuid.UUID;documents map[string]metadataTransportBody}
func(c *metadataTransportCompiler)Publish(ctx context.Context,task uuid.UUID,ordinal int,contract string,body []byte)(v2input.Ref,error){
 if err:=ctx.Err();err!=nil{return v2input.Ref{},err}
 if task!=c.task{return v2input.Ref{},v2input.ErrInvalid}
 purpose:=""
 if ordinal==0&&contract=="InputClosureChunk"{purpose="v2_input_chunk_0"}
 if ordinal==1&&contract=="InputClosureManifest"{purpose="v2_input_manifest"}
 if purpose==""{return v2input.Ref{},v2input.ErrInvalid}
 ref:=v2input.Ref{ArtifactID:uuid.NewSHA1(task,[]byte("delivery:metadata:transport:"+purpose+":1")),SHA256:digest(body)}
 c.documents[purpose]=metadataTransportBody{ref,append([]byte(nil),body...)}
 return ref,nil
}

// NewMetadataTransportPublisher recompiles expected transport before any IO.
// Its repository uses current metadata proof SQL around each immutable upload.
func NewMetadataTransportPublisher(ctx context.Context,db MetadataTransactionBeginner,scope MetadataProofScope,plan MetadataWritePlan,bucket string,objects artifactjson.Objects)(v2input.Publisher,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if objects==nil{return nil,v2input.ErrInvalid}
 repository,err:=NewMetadataDocumentRepository(db,scope,plan,bucket)
 if err!=nil{return nil,err}
 compiler:=&metadataTransportCompiler{task:scope.Task,documents:map[string]metadataTransportBody{}}
 if _,err=plan.PublishInputs(ctx,scope.ChildRun,compiler);err!=nil{return nil,err}
 repository.transport=compiler.documents
 documents:=artifactjson.Publisher{Repository:repository,Objects:objects}
 return v2input.ScopedPublisher{Documents:documents,Tenant:scope.Tenant,Run:scope.ChildRun,AssemblyLease:scope.Token},nil
}
