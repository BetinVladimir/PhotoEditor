package main

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/semanticreview"
)

type sharedSemanticAdmission struct{}
func (*sharedSemanticAdmission) Apply(context.Context,auth.Identity,uuid.UUID,uuid.UUID,string,semanticreview.ApplyDecision)(idempotency.Response,error){panic("selection must not admit")}
func (*sharedSemanticAdmission) Close(context.Context,auth.Identity,uuid.UUID,uuid.UUID,string,semanticreview.ReviewClose)(idempotency.Response,error){panic("selection must not close")}

func TestSemanticCloseSelectionKeepsConfiguredAdmission(t *testing.T){
 t.Setenv("PHOTO_SEMANTIC_REVIEW_CONFIG_FILE","must-not-read")
 t.Setenv("PHOTO_SEMANTIC_REVIEW_CONFIG_SHA256","")
 installed:=&sharedSemanticAdmission{}
 selected,err:=buildSemanticCloseForApply(nil,nil,"",installed)
 if err!=nil || selected!=installed {t.Fatal("configured Apply/Close provider replaced",selected,err)}
}
func TestSemanticCloseSelectionInstallsReplayFallback(t *testing.T){
 t.Setenv("PHOTO_SEMANTIC_REVIEW_CONFIG_FILE","")
 t.Setenv("PHOTO_SEMANTIC_REVIEW_CONFIG_SHA256","")
 pool:=&pgxpool.Pool{}
 selected,err:=buildSemanticCloseForApply(pool,nil,"",&semanticreview.ApplyService{API:pool})
 fallback,ok:=selected.(*semanticreview.CloseService)
 if err!=nil || !ok || fallback.API!=pool || fallback.Prepare!=nil {t.Fatal("invalid replay fallback",selected,err)}
}
func TestSemanticCloseSelectionRefusesBrokenFallback(t *testing.T){
 t.Setenv("PHOTO_SEMANTIC_REVIEW_CONFIG_FILE","unpaired")
 t.Setenv("PHOTO_SEMANTIC_REVIEW_CONFIG_SHA256","")
 if _,err:=buildSemanticCloseForApply(&pgxpool.Pool{},nil,"",&semanticreview.ApplyService{});err==nil {t.Fatal("broken fallback accepted")}
}
