package interchange
import (
 "context"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type importMaterializerStub struct{calls int;publisher *importDocumentPublisherStub}
func(s *importMaterializerStub)MaterializeImport(_ context.Context,documents ImportPreparedDocuments,_ v2input.DraftTask)error{
 s.calls++;if len(s.publisher.calls)!=len(documents.Documents()){return objectstore.ErrIntegrity};return nil
}
func importMaterializationFixture(t *testing.T)(ImportPreparedDocuments,v2input.DraftTask){
 identity,doc:=importPublicationFixture();documents,err:=FreezeImportDocuments(identity,v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))},doc.Body);if err!=nil{t.Fatal(err)}
 task:=v2input.DraftTask{ID:documents.TaskID(),NodeKey:"v2.interchange.import:0",Operation:"v2.interchange.import",OperationVersion:"1",ResourceClass:"cpu",ParamsArtifactID:documents.params.Ref.ArtifactID,ParamsSHA:documents.params.Ref.SHA256,InputDigest:rawSHA([]byte("inputs")),ExecutionProfile:json.RawMessage(`{"backend":"cpu","renderer_version":"v2.2","resource_estimate":{"output_bytes":25165824,"ram_bytes":1073741824,"scratch_bytes":0,"vram_bytes":0}}`)}
 return documents,task
}
func TestImportMaterializationOrdersDocumentsBeforeShell(t *testing.T){
 documents,task:=importMaterializationFixture(t);publisher:=&importDocumentPublisherStub{};stage:=&importMaterializerStub{publisher:publisher}
 if err:=PrepareImportTask(context.Background(),documents,task,publisher,stage);err!=nil||stage.calls!=1{t.Fatal("materialization",err)}
}
func TestImportMaterializationRejectsForeignTaskBeforePublication(t *testing.T){
 documents,task:=importMaterializationFixture(t);task.ID=uuid.New();publisher:=&importDocumentPublisherStub{};stage:=&importMaterializerStub{publisher:publisher}
 if err:=PrepareImportTask(context.Background(),documents,task,publisher,stage);err==nil||len(publisher.calls)!=0||stage.calls!=0{t.Fatal("foreign task reached publication")}
}
func TestImportMaterializationNeverStagesAfterPublicationFailure(t *testing.T){
 documents,task:=importMaterializationFixture(t);publisher:=&importDocumentPublisherStub{fail:true};stage:=&importMaterializerStub{publisher:publisher}
 if err:=PrepareImportTask(context.Background(),documents,task,publisher,stage);err==nil||stage.calls!=0{t.Fatal("shell created after publication failure")}
}
