package workflow

import (
 "context"
 "testing"
)
func TestAutomaticReplacementStartupRequiresDatabase(t *testing.T){
 if err:=verifyAutomaticReplacementInstallation(context.Background(),nil);err==nil{t.Fatal("missing SQL installation accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=verifyAutomaticReplacementInstallation(ctx,nil);err!=context.Canceled{t.Fatal(err)}
}

func TestReplacementExecutionStartupRequiresDatabase(t *testing.T) {
 if err:=verifyPerspectiveReplacementExecutionInstallation(context.Background(),nil);err==nil { t.Fatal("missing execution SQL accepted") }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=verifyPerspectiveReplacementExecutionInstallation(ctx,nil);err!=context.Canceled { t.Fatal(err) }
}
