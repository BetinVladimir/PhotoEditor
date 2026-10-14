package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func TestExportScopedValidatorBindsExactFrozenRootDocuments(t *testing.T){
 _,_,intent,compiled,snapshot,raw:=exportLedgerFixture(t)
 frozen,err:=FreezeExportDocuments(context.Background(),intent,compiled,snapshot,raw);if err!=nil{t.Fatal(err)}
 validator,err:=NewExportScopedDocumentValidator(context.Background(),intent,frozen,nil);if err!=nil{t.Fatal(err)}
 for _,doc:=range []struct{entry v2input.Entry;raw []byte;count int}{{v2input.Entry{Ref:compiled.paramsRef,Contract:"Params"},compiled.paramsBody,1},{v2input.Entry{Ref:snapshot,Contract:"InputSnapshot"},raw,2}}{
  children,err:=validator.ExportDocumentChildren(context.Background(),doc.entry,doc.raw)
  if err!=nil||len(children)!=doc.count{t.Fatal("root validation failed",err)}
  bad:=doc.entry;bad.Ref.ArtifactID=uuid.New()
  if _,err:=validator.ExportDocumentChildren(context.Background(),bad,doc.raw);err==nil{t.Fatal("foreign root identity accepted")}
 }
 body:=[]byte(`{}`);unknown:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)},Contract:"WorkingImage"}
 if _,err:=validator.ExportDocumentChildren(context.Background(),unknown,body);err==nil{t.Fatal("missing dependency validator accepted")}
 if _,err:=validator.ExportDocumentChildren(context.Background(),v2input.Entry{Ref:compiled.planRef,Contract:"InterchangePlan"},compiled.planBody);err!=nil{t.Fatal(err)}
}
