package interchange

import (
 "context"
 "photoeditor/backend/internal/objectstore"
)
func(p ParsePlan)Documents()([]CreateDocument,error){
 snapshot,err:=p.CompileSnapshot();if err!=nil{return nil,err}
 return []CreateDocument{{Purpose:"v30_interchange_snapshot",Ref:snapshot.Ref,Body:append([]byte(nil),snapshot.Body...)},{Purpose:"task_params",Ref:p.paramsRef,Body:append([]byte(nil),p.paramsRaw...)}},nil
}
func(p ParsePlan)PublishDocuments(ctx context.Context,publisher CreateStoragePublisher)error{
 if err:=ctx.Err();err!=nil{return err}
 if publisher.Tenant!=p.tenant{return objectstore.ErrInvalid}
 docs,err:=p.Documents();if err!=nil{return err}
 for _,doc:=range docs{ref,err:=publisher.PublishCreateDocument(ctx,p.identity,doc);if err!=nil{return err};if ref!=doc.Ref{return objectstore.ErrIntegrity}}
 return nil
}
