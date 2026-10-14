package v2optical
import("context";"testing";"github.com/google/uuid";"photoeditor/backend/internal/v2input")
func TestDetectionPreparationRefusesMissingTransactionAndCancellation(t *testing.T){
 var p PreparedNegativeReplacementInputs
 if _,err:=p.LockPerspectiveDetectionPreparation(context.Background(),nil,nil,nil,nil,nil,[]byte(`{}`),uuid.New());err!=v2input.ErrInvalid{t.Fatal("missing transaction accepted",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=p.LockPerspectiveDetectionPreparation(ctx,nil,nil,nil,nil,nil,nil,uuid.Nil);err!=context.Canceled{t.Fatal("cancelled preparation accepted",err)}
}

func TestDetectionDraftSaveRefusesUnprovedBeforePersistence(t *testing.T){
 var p PreparedNegativeReplacementInputs
 if _,err:=p.SavePerspectiveDetectionDraft(context.Background(),nil,nil,nil,nil,nil,[]byte(`{}`),uuid.New());err!=v2input.ErrInvalid{t.Fatal("unproved draft saved",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=p.SavePerspectiveDetectionDraft(ctx,nil,nil,nil,nil,nil,nil,uuid.Nil);err!=context.Canceled{t.Fatal("cancelled draft saved",err)}
}

func TestDetectionDraftReadRefusesUnprovedBeforeSQL(t *testing.T){
 var p PreparedNegativeReplacementInputs
 if _,raw,err:=p.ReadPerspectiveDetectionDraft(context.Background(),nil,nil,nil,nil,nil,[]byte(`{}`),uuid.New());err!=v2input.ErrInvalid||raw!=nil{t.Fatal("unproved recovery reached SQL",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,raw,err:=p.ReadPerspectiveDetectionDraft(ctx,nil,nil,nil,nil,nil,nil,uuid.Nil);err!=context.Canceled||raw!=nil{t.Fatal("cancelled recovery returned draft",err)}
}
