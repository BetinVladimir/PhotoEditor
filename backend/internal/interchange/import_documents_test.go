package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type importDocumentPublisherStub struct{calls []ImportDocument;fail bool;cancel context.CancelFunc}
func(s *importDocumentPublisherStub)PublishImportDocument(_ context.Context,_ ImportDocumentIdentity,d ImportDocument)(v2input.Ref,error){
 s.calls=append(s.calls,d)
 if s.cancel!=nil{s.cancel()}
 if s.fail{return v2input.Ref{},objectstore.ErrUnavailable};return d.Ref,nil
}
func TestImportDocumentPairOwnsBytesAndPublishesDependencyFirst(t *testing.T){
 identity,doc:=importPublicationFixture();parsed:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))}
 frozen,err:=FreezeImportDocuments(identity,parsed,doc.Body);if err!=nil{t.Fatal(err)}
 original:=frozen.Documents();doc.Body[0]='x';mutated:=frozen.Documents();mutated[0].Body[0]='x'
 if string(frozen.Documents()[0].Body)!=string(original[0].Body){t.Fatal("caller mutation changed frozen document")}
 publisher:=&importDocumentPublisherStub{}
 ref,err:=frozen.Publish(context.Background(),publisher)
 if err!=nil||len(publisher.calls)!=2||publisher.calls[0].Purpose!="v30_interchange_import_plan"||publisher.calls[1].Purpose!="task_params"||ref!=original[1].Ref{t.Fatal("publication order",err)}
 replay,err:=FreezeImportDocuments(identity,parsed,original[0].Body)
 if err!=nil||replay.Documents()[1].Ref!=ref{t.Fatal("replay identity changed",err)}
}
func TestImportDocumentPairStopsOnPlanFailureOrCancellation(t *testing.T){
 identity,doc:=importPublicationFixture();frozen,err:=FreezeImportDocuments(identity,v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))},doc.Body);if err!=nil{t.Fatal(err)}
 failed:=&importDocumentPublisherStub{fail:true}
 if _,err=frozen.Publish(context.Background(),failed);!errors.Is(err,objectstore.ErrUnavailable)||len(failed.calls)!=1{t.Fatal("Params published after Plan failure",err)}
 ctx,cancel:=context.WithCancel(context.Background());defer cancel();cancelled:=&importDocumentPublisherStub{cancel:cancel}
 if _,err=frozen.Publish(ctx,cancelled);!errors.Is(err,context.Canceled)||len(cancelled.calls)!=1{t.Fatal("Params published after cancellation",err)}
}
