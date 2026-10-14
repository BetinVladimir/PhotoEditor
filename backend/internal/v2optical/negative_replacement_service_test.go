package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)

func TestReplacementWaveServiceRefusesInvalidScopeBeforeSQL(t *testing.T){
 tenant,project:=uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 db:=&negativeDraftStoreDB{}
 service:=&NegativeService{tenant:tenant,project:project,coordinator:NegativeCoordinator{DB:db}}
 for _,tc:=range []struct{tenant,project uuid.UUID;route string;limit int}{
  {uuid.New(),project,"base",1},{tenant,uuid.New(),"base",1},{tenant,project,"skip",1},{tenant,project,"base",0},{tenant,project,"base",101},{tenant,project,"base",1},
 }{
  pass,err:=service.AdvanceReplacementWave(context.Background(),who,tc.tenant,tc.project,NegativeDeclinedDecision{Route:tc.route},tc.limit)
  if err==nil||len(pass.Receipts)!=0||db.begins!=0{t.Fatal("invalid scope/deployment reached SQL",tc,err)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=service.AdvanceReplacementWave(ctx,who,tenant,project,NegativeDeclinedDecision{Route:"base"},1);err!=context.Canceled{t.Fatal("cancellation",err)}
 var absent *NegativeService
 if _,err:=absent.AdvanceReplacementWave(context.Background(),who,tenant,project,NegativeDeclinedDecision{Route:"base"},1);err==nil{t.Fatal("absent service accepted")}
}
