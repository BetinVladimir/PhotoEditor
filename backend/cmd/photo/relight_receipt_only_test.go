package main

import (
 "context"
 "errors"
 "os"
 "testing"
 "time"
)

func TestRelightReceiptStartupAbsentBothConfigAndReader(t *testing.T){
 t.Setenv("PHOTO_V28_RELIGHT_CONFIG","");t.Setenv("PHOTO_V28_RELIGHT_ANALYSIS_CONFIG","");t.Setenv("V28_RELIGHT_SCHEDULER_DATABASE_URL","")
 relight,closeRelight,err:=buildRelightCreate(context.Background(),nil,"");if err!=nil||relight!=nil{t.Fatal(err)};closeRelight()
 analysis,closeAnalysis,err:=buildRelightAnalysis(context.Background(),nil,"");if err!=nil||analysis!=nil{t.Fatal(err)};closeAnalysis()
}
func TestRelightReceiptReaderHonorsCancelledStartupBeforeConnection(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 for _,analysis:=range []bool{false,true}{pool,err:=buildRelightReceiptReader(ctx,"unused",analysis);if pool!=nil||!errors.Is(err,context.Canceled){t.Fatal("cancelled startup opened reader",err)}}
}
func TestRelightConfiguredInvalidFileDoesNotSelectReceiptOnly(t *testing.T){
 t.Setenv("PHOTO_V28_RELIGHT_CONFIG","/nonexistent-relight-config.json");t.Setenv("PHOTO_V28_RELIGHT_ANALYSIS_CONFIG","/nonexistent-analysis-config.json")
 t.Setenv("V28_RELIGHT_SCHEDULER_DATABASE_URL","unused-invalid-dsn")
 if service,closeService,err:=buildRelightCreate(context.Background(),nil,"");err==nil||service!=nil{closeService();t.Fatal("invalid relight file silently enabled recovery")}
 if service,closeService,err:=buildRelightAnalysis(context.Background(),nil,"");err==nil||service!=nil{closeService();t.Fatal("invalid analysis file silently enabled recovery")}
}
func TestIntegrationRelightReceiptOnlyStartup(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated PostgreSQL fixture required")}
 t.Setenv("PHOTO_V28_RELIGHT_CONFIG","");t.Setenv("PHOTO_V28_RELIGHT_ANALYSIS_CONFIG","");t.Setenv("V28_RELIGHT_SCHEDULER_DATABASE_URL",dsn)
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 // Nil object store and empty bucket ensure startup installs no publisher.
 relight,closeRelight,err:=buildRelightCreate(ctx,nil,"");if err!=nil||relight==nil{t.Fatal("receipt-only relight startup",err)};defer closeRelight()
 analysis,closeAnalysis,err:=buildRelightAnalysis(ctx,nil,"");if err!=nil||analysis==nil{t.Fatal("receipt-only analysis startup",err)};defer closeAnalysis()
}
