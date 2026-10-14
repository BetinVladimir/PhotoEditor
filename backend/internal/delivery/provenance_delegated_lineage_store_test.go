package delivery

import (
 "context"
 "errors"
 "testing"

 "photoeditor/backend/internal/v2input"
)

func TestDelegatedLineagePublisherRejectsMissingAuthorityBeforeObjectIO(t *testing.T){
 _,err:=PublishDelegatedSigningLineage(context.Background(),
  SQLDelegatedLineageStore{},nil,v2input.Ref{},[]byte("{}"))
 if !errors.Is(err,v2input.ErrInvalid){t.Fatal("missing delegated publisher accepted",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 _,err=PublishDelegatedSigningLineage(ctx,SQLDelegatedLineageStore{},nil,
  v2input.Ref{},[]byte("{}"))
 if !errors.Is(err,context.Canceled){t.Fatal("cancelled delegated publication continued",err)}
}

func TestDelegatedLineageReadyRequiresCurrentAuthority(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 err:=(SQLDelegatedLineageStore{}).VerifyReady(ctx,v2input.Ref{},nil)
 if !errors.Is(err,context.Canceled){t.Fatal("cancelled lineage read continued",err)}
 err=(SQLDelegatedLineageStore{}).VerifyReady(context.Background(),v2input.Ref{},nil)
 if !errors.Is(err,v2input.ErrInvalid){t.Fatal("unbound lineage receipt accepted",err)}
}
