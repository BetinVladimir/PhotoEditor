package interchange
import (
 "context"
 "testing"
 "strings"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/v2input"
)
// Embedded ports deliberately panic if construction attempts object/native IO.
type exportCompositionObjects struct{ExportExecutionObjects}
type exportCompositionValidator struct{ExportTypedDocumentValidator}
type exportCompositionBinary struct{ExportBinaryQualifier}
func TestSQLExportCreationCompositionRequiresIndependentAuthorities(t *testing.T){
 proposals:=&exportCreationAdapters{}
 base:=SQLExportExecutionInstaller{API:new(pgxpool.Pool),Scheduler:new(pgxpool.Pool),Objects:exportCompositionObjects{},Bucket:"photos",Dependencies:exportCompositionValidator{},Binaries:exportCompositionBinary{},Qualification:&exportResourceQualifierStub{},MaxInputBytes:1<<30}
 composed,err:=NewSQLExportCreationPreparation(context.Background(),proposals,base)
 if err!=nil||composed.Proposals!=proposals||composed.Drafts==nil||composed.Installation==nil{t.Fatal("qualified assembly rejected",err)}
 if len(proposals.events)!=0{t.Fatal("constructor performed proposal IO")}
 for _,alter:=range []func(*SQLExportExecutionInstaller){
 func(s *SQLExportExecutionInstaller){s.API=nil},func(s *SQLExportExecutionInstaller){s.Scheduler=s.API},
 func(s *SQLExportExecutionInstaller){s.Objects=nil},func(s *SQLExportExecutionInstaller){s.Dependencies=nil},
 func(s *SQLExportExecutionInstaller){s.Binaries=nil},func(s *SQLExportExecutionInstaller){s.Qualification=nil},
 func(s *SQLExportExecutionInstaller){s.MaxInputBytes=0},func(s *SQLExportExecutionInstaller){s.MaxInputBytes=(128<<30)+1},
 func(s *SQLExportExecutionInstaller){s.Bucket=""},func(s *SQLExportExecutionInstaller){s.Native=&ExportNativeResolverInstallation{}},
 }{bad:=base;alter(&bad);if _,err:=NewSQLExportCreationPreparation(context.Background(),proposals,bad);err!=v2input.ErrInvalid{t.Fatal("incomplete installation accepted",err)}}
 if _,err:=NewSQLExportCreationPreparation(context.Background(),nil,base);err!=v2input.ErrInvalid{t.Fatal("missing proposal source accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=NewSQLExportCreationPreparation(ctx,proposals,base);err!=context.Canceled{t.Fatal("cancelled composition accepted",err)}
}

type exportCompositionProbe struct{ExportNativeProbe}
func TestSQLNativeExportCreationUsesOneQualifiedImageAuthority(t *testing.T){
 scratch:=&ExportArchiveScratch{root:"/process-owned/export-scratch",maximum:8<<30}
 native:=&ExportNativeResolverInstallation{Scratch:scratch,ImageProbe:exportCompositionProbe{},MaskProbe:exportCompositionProbe{},ArrayProbe:exportCompositionProbe{},ImageRuntimeSHA:strings.Repeat("a",64),ArrayRuntimeSHA:strings.Repeat("b",64)}
 installer:=SQLExportExecutionInstaller{API:new(pgxpool.Pool),Scheduler:new(pgxpool.Pool),Objects:exportCompositionObjects{},Bucket:"photos",Dependencies:exportCompositionValidator{},Native:native,Qualification:&exportResourceQualifierStub{},MaxInputBytes:128<<30}
 limits:=ExportLimits{ArchiveBytes:64<<30,ExpandedBytes:128<<30,Files:30000}
 composed,err:=NewSQLNativeExportCreationPreparation(context.Background(),installer,limits);if err!=nil{t.Fatal(err)}
 proposal:=composed.Proposals.(ExportProposalSource);inputs:=proposal.Inputs.(SQLExportProposalInputs);frames:=inputs.Frames.(SQLExportProposalFrameFactory);image:=frames.Native.(NativeExportImageQualifier)
 installed:=composed.Installation.(SQLExportExecutionInstaller)
 if inputs.DB!=installer.API||frames.DB!=installer.API||frames.MaxBytes!=64<<30||image.Scratch!=scratch||image.RuntimeSHA!=native.ImageRuntimeSHA||installed.Native==native{t.Fatal("proposal/execution authorities diverged")}
 native.ImageRuntimeSHA=strings.Repeat("c",64)
 if installed.Native.ImageRuntimeSHA!=image.RuntimeSHA{t.Fatal("caller mutated native installation")}
 for _,bad:=range []ExportLimits{{},{ArchiveBytes:(64<<30)+1,ExpandedBytes:1,Files:1},{ArchiveBytes:1,ExpandedBytes:(128<<30)+1,Files:1},{ArchiveBytes:1,ExpandedBytes:1,Files:30001}}{
  if _,err:=NewSQLNativeExportCreationPreparation(context.Background(),installer,bad);err!=v2input.ErrInvalid{t.Fatal("invalid archive limits accepted")}
 }
 installer.Native=nil
 if _,err:=NewSQLNativeExportCreationPreparation(context.Background(),installer,limits);err!=v2input.ErrInvalid{t.Fatal("missing native installation accepted")}
}
