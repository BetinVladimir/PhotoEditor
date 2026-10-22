package v2portrait

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

type receiptOnlyProbeDB struct{calls int}
var errReceiptOnlyProbe=errors.New("receipt reader reached")
func (db *receiptOnlyProbeDB) Begin(context.Context)(pgx.Tx,error){db.calls++;return nil,errReceiptOnlyProbe}

func TestReceiptOnlyRoutersRejectMissingReader(t *testing.T){
 if _,err:=NewRelightReceiptOnlyProjects(nil);!errors.Is(err,v2input.ErrInvalid){t.Fatal(err)}
 if _,err:=NewRelightAnalysisReceiptOnlyProjects(nil);!errors.Is(err,v2input.ErrInvalid){t.Fatal(err)}
}
func TestReceiptOnlyRoutersHaveNoPublicationServices(t *testing.T){
 db:=&receiptOnlyProbeDB{}
 relight,err:=NewRelightReceiptOnlyProjects(db);if err!=nil||relight.db!=db||len(relight.services)!=0{t.Fatal(err)}
 analysis,err:=NewRelightAnalysisReceiptOnlyProjects(db);if err!=nil||analysis.db!=db||len(analysis.services)!=0{t.Fatal(err)}
}
func TestReceiptOnlyKeyedRecoveryReadsBeforeMissingProvider(t *testing.T){
 db:=&receiptOnlyProbeDB{};router,_:=NewRelightReceiptOnlyProjects(db)
 params,_,_,_,_:=relightQualificationFixture(t)
 raw,_,err:=params.Compile();if err!=nil{t.Fatal(err)}
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 _,err=router.SubmitProjectKeyed(context.Background(),who,uuid.New(),uuid.New(),"receipt-key",RelightDraftIntent{Snapshot:uuid.New(),SubjectKind:"manual",ParamsRaw:raw})
 if !errors.Is(err,errReceiptOnlyProbe)||db.calls!=1{t.Fatal(db.calls,err)}
}
func TestReceiptOnlyAnalysisRecoveryReadsBeforeMissingProvider(t *testing.T){
 db:=&receiptOnlyProbeDB{};router,_:=NewRelightAnalysisReceiptOnlyProjects(db)
 r:=analysisPlanFixture(t)
 _,err:=router.SubmitProjectAnalysis(context.Background(),auth.Identity{UserID:r.Actor,SessionID:r.Session},r.Tenant,r.Project,r.Key,RelightAnalysisIntent{SchemaVersion:1,Snapshot:r.Snapshot,Base:r.Base,Operation:r.Operation})
 if !errors.Is(err,errReceiptOnlyProbe)||db.calls!=1{t.Fatal(db.calls,err)}
}
func TestReceiptOnlyRoutersRespectCancellationBeforeDatabase(t *testing.T){
 db:=&receiptOnlyProbeDB{};ctx,cancel:=context.WithCancel(context.Background());cancel()
 relight,_:=NewRelightReceiptOnlyProjects(db);analysis,_:=NewRelightAnalysisReceiptOnlyProjects(db)
 if _,err:=relight.SubmitProjectKeyed(ctx,auth.Identity{},uuid.New(),uuid.New(),"key",RelightDraftIntent{});!errors.Is(err,context.Canceled){t.Fatal(err)}
 if _,err:=analysis.SubmitProjectAnalysis(ctx,auth.Identity{},uuid.New(),uuid.New(),"key",RelightAnalysisIntent{});!errors.Is(err,context.Canceled){t.Fatal(err)}
 if db.calls!=0{t.Fatal("cancelled recovery reached database")}
}
