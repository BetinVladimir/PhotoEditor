package delivery

import (
 "context"
 "errors"
 "testing"

 "photoeditor/backend/internal/v2input"
)

func TestDelegatedLineageCoordinatorRequiresScopedGraphReader(t *testing.T){
 _,_,err:=CompileAndPublishDelegatedLineage(context.Background(),
  SQLDelegatedLineageStore{},nil,nil)
 if !errors.Is(err,v2input.ErrInvalid){t.Fatal("unscoped graph accepted",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 _,_,err=CompileAndPublishDelegatedLineage(ctx,SQLDelegatedLineageStore{},nil,nil)
 if !errors.Is(err,context.Canceled){t.Fatal("cancelled graph read continued",err)}
}
