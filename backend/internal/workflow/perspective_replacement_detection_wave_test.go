package workflow

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2optical"
)

func TestReplacementDetectionWaveRejectsMissingDependenciesBeforeSQL(t *testing.T){
 service:=ReplacementDetectionWaveService{}
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 pass,err:=service.Advance(context.Background(),who,uuid.New(),uuid.New(),v2optical.NegativeDeclinedDecision{Route:"base"},1)
 if err==nil||pass.Publications!=0||len(pass.Admissions)!=0{t.Fatal("invalid wave admitted",pass,err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=service.Advance(ctx,who,uuid.New(),uuid.New(),v2optical.NegativeDeclinedDecision{Route:"base"},1);err!=context.Canceled{t.Fatal(err)}
}
