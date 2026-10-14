package interchange

import (
 "context"
 "errors"
 "testing"
 "photoeditor/backend/internal/v2input"
)

type exportGraphFactoryStub struct{resolver ExportClosureNodeResolver;calls int;cause error}
func(s *exportGraphFactoryStub)BindExportGraph(context.Context,CompiledExportPlan)(ExportClosureNodeResolver,error){s.calls++;return s.resolver,s.cause}
func(s *exportGraphFactoryStub)ResolveExportNode(context.Context,v2input.Entry)(ExportClosureNode,error){return ExportClosureNode{},errors.New("unbound factory used")}

func TestExportInventoryBindsFreshGraphPerPass(t *testing.T){
 compiled,resolver:=exportClosureFixture(t);factory:=&exportGraphFactoryStub{resolver:resolver}
 for i:=0;i<2;i++{if _,err:=ResolveExportInputInventory(context.Background(),compiled,factory);err!=nil{t.Fatal(err)}}
 if factory.calls!=2||resolver.calls!=14{t.Fatal("graph scope cached across admission passes")}
 cause:=errors.New("discovery failed");factory.cause=cause
 if _,err:=ResolveExportInputInventory(context.Background(),compiled,factory);!errors.Is(err,cause){t.Fatal("discovery refusal bypassed",err)}
}

func TestNativeResolverInstallationDiscoversAndOwnsGraphScope(t *testing.T){
 compiled,json:=exportClosureFixture(t)
 scratch,err:=NewExportArchiveScratch(t.TempDir(),100);if err!=nil{t.Fatal(err)}
 _,catalog,objects,_:=importReaderFixture()
 installation:=ExportNativeResolverInstallation{Catalog:exportWorkingArrayCatalogStub{ExportDocumentCatalog:exportReaderCatalog{catalog}},Objects:objects,JSON:json,Scratch:scratch,ImageProbe:&exportNativeProbeStub{},MaskProbe:&exportMaskProbeStub{},ArrayProbe:&exportArrayProbeStub{},ImageRuntimeSHA:rawSHA([]byte("image runtime")),ArrayRuntimeSHA:rawSHA([]byte("array runtime")),MaxBytes:1<<20}
 originalCatalog:=installation.Catalog
 installation.Catalog=exportReaderCatalog{catalog}
 if _,err:=installation.BindExportGraph(context.Background(),compiled);err!=v2input.ErrInvalid{t.Fatal("missing working array producer authority accepted")}
 installation.Catalog=originalCatalog
 bound,err:=installation.BindExportGraph(context.Background(),compiled);if err!=nil{t.Fatal(err)}
 router,ok:=bound.(ExportNodeResolverRouter);if !ok{t.Fatal("missing scoped router")}
 binary,ok:=router.Binary.(ExportBinaryNodeResolver);if !ok{t.Fatal("missing physical resolver")}
 qualifiers,ok:=binary.Qualifier.(ExportNativeQualifierRouter);if !ok||qualifiers.Images==nil||qualifiers.Arrays==nil||qualifiers.Masks!=nil{t.Fatal("incorrect discovered installation")}
 // Graph fixture has no MaskRef roots: a mask must not acquire geometry merely
 // because an array with a matching digest exists in the catalogue.
 if _,err:=installation.ResolveExportNode(context.Background(),v2input.Entry{Contract:"WorkingPixels"});err!=v2input.ErrInvalid{t.Fatal("unbound binary resolved")}
 installation.ArrayProbe=nil
 if _,err:=installation.BindExportGraph(context.Background(),compiled);err!=v2input.ErrInvalid{t.Fatal("missing native probe accepted")}
}

type exportWorkingArrayCatalogStub struct{ExportDocumentCatalog}
func(exportWorkingArrayCatalogStub)QualifyExportWorkingArray(context.Context,v2input.Entry,v2input.Entry)error{return nil}

func(exportWorkingArrayCatalogStub)QualifyExportDenseWarpOccurrence(context.Context,v2input.Entry,[]byte,int,v2input.Entry)error{return nil}

func(exportWorkingArrayCatalogStub)QualifyExportMaskArray(context.Context,v2input.Entry,[]byte,v2input.Entry)error{return nil}

func(*exportNodeResolverStub)QualifyExportWorkingArrayOccurrence(context.Context,v2input.Entry,[]byte,v2input.Entry)error{return nil}
