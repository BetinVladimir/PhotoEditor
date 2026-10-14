package interchange
import (
 "context"
 "testing"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2draft"
)
func TestExportStyleInstallationRequiresRequestScopeAndBuildsNonrecursiveReader(t *testing.T){
 _,_,intent,frozen,_:=exportExecutionFixture(t)
 scratch,err:=NewExportArchiveScratch(t.TempDir(),1000);if err!=nil{t.Fatal(err)}
 _,catalog,objects,_:=importReaderFixture()
 base:=ExportDocumentValidatorInstallation{ICCSHA:rawSHA([]byte("icc")),ModelSHA:rawSHA([]byte("model")),Semantics:&exportSemanticsStub{},Other:exportReaderValidator{&importValidatorStub{}}}
 installed:=ExportStyleValidatorInstallation{Body:exportBodyFactoryStub{},PersonPolicy:exportStatsPersonPolicyFactoryStub{},PromptPolicy:exportPromptMaskPolicyFactoryStub{},Base:base,Scratch:scratch,Probe:&exportStyleProbeStub{},Models:map[string]string{},RuntimeSHA:rawSHA([]byte("runtime"))}
 if _,err:=installed.BuildExportDocumentValidator(context.Background(),intent,frozen);err!=v2input.ErrInvalid{t.Fatal("unscoped construction accepted",err)}
 validator,err:=installed.BuildScopedExportDocumentValidator(context.Background(),intent,frozen,exportCorrectionReaderCatalog{exportReaderCatalog{catalog}},objects);if err!=nil{t.Fatal(err)}
 composition,ok:=validator.(*ExportFrozenCompositionValidator);if !ok{t.Fatal("composition validator missing")}
 bodySchema,ok:=composition.base.(*NativeExportBodyOriginValidator);if !ok{t.Fatal("body origin gate missing")}
 seriesSchema,ok:=bodySchema.Other.(*NativeExportStyleValidator);if !ok||seriesSchema.contract!="SeriesCorrections"{t.Fatal("series schema missing")}
 seriesProducer:=seriesSchema.authority.(ExportSeriesProducerValidator)
 originSeries,ok:=seriesProducer.Authority.(*NativeExportSeriesOriginQualifier);if !ok{t.Fatal("original series content proof missing")}
 measureNative,ok:=originSeries.Inputs.Measure.(*NativeExportSeriesMeasureQualifier);if !ok{t.Fatal("original measure native proof missing")}
 physicalMasks,ok:=measureNative.Inputs.(NativeExportSeriesMeasureMasks);if !ok||physicalMasks.Source==nil||physicalMasks.Inputs==nil{t.Fatal("physical region masks missing")}
 if _,ok:=physicalMasks.Source.(QualifiedExportSeriesMeasureMaskSource);!ok{t.Fatal("current model-qualified region mask source missing")}
 params,ok:=seriesSchema.authority.(ExportSeriesProducerValidator).Other.(*ExportOperationParamsValidator);if !ok||params.Source==nil||params.native.contract!="OperationParams"{t.Fatal("operation Params authority missing")}
 resolvedOrigin,ok:=params.Other.(*ExportResolvedOriginValidator);if !ok||resolvedOrigin.Origin==nil||resolvedOrigin.native.contract!="ResolvedOrigin"{t.Fatal("original resolve Params authority missing")}
 resolved,ok:=resolvedOrigin.Other.(*NativeExportStyleValidator);if !ok||resolved.contract!="ResolvedStyle"{t.Fatal("resolved schema missing")}
 localScope,ok:=resolved.authority.(ExportImageLocalSourceScope);if !ok||localScope.Images==nil||localScope.Resolved==nil{t.Fatal("aligned image source scope missing")}
 working:=localScope.Other.(ExportDescriptorFaceValidator).Other.(ExportWorkingDocumentValidator)
 correction:=working.Additional.(ExportCorrectionWorkingAuthority)
 auxiliary:=correction.Plans.(ExportJSONNodeResolver)
 auxiliaryBody,ok:=auxiliary.Validator.(*NativeExportBodyOriginValidator);if !ok{t.Fatal("auxiliary body origin gate missing")}
 auxiliarySeries,ok:=auxiliaryBody.Other.(*NativeExportStyleValidator);if !ok||auxiliarySeries.contract!="SeriesCorrections"{t.Fatal("auxiliary series schema missing")}
 auxiliaryParams,ok:=auxiliarySeries.authority.(ExportSeriesProducerValidator).Other.(*ExportOperationParamsValidator);if !ok||auxiliaryParams.Source==nil{t.Fatal("auxiliary operation Params authority missing")}
 if _,ok:=auxiliaryParams.Other.(*ExportResolvedOriginValidator);!ok{t.Fatal("auxiliary original Params proof missing")}
 render:=working.Dependencies.(ExportFrozenDocumentValidator)
 portable:=render.Dependencies.(ExportRecipeDocumentValidator)
 dependencies:=portable.Dependencies.(ExportDependencyDocumentValidator)
 maskSemantics,ok:=dependencies.Semantics.(*ExportCanonicalMaskSemantics);if !ok{t.Fatal("canonical mask semantics missing")}
 sourceSemantics,ok:=maskSemantics.fallback.(*ExportSourceSemantics);if !ok{t.Fatal("source semantics missing")}
 stats,ok:=sourceSemantics.fallback.(ExportStatsSemantics);if !ok||stats.Authority==nil||stats.Other==nil{t.Fatal("stats producer authority missing")}
 origin,ok:=stats.Authority.(ExportStatsOriginQualifier);if !ok{t.Fatal("original statistics Params authority missing")}
 person,ok:=origin.Person.(NativeExportStatsPersonQualifier);if !ok{t.Fatal("physical person authority missing")}
 if _,ok:=person.Source.(QualifiedExportStatsPersonSource);!ok{t.Fatal("qualified person source missing")}
 overrideSchema,ok:=dependencies.Other.(*NativeExportStyleValidator);if !ok||overrideSchema.contract!="Override"{t.Fatal("override schema missing")}
 overrides,ok:=overrideSchema.authority.(*ExportFrozenOverrideScope);if !ok{t.Fatal("override source scope missing")}
 native,ok:=overrides.policy.(*NativeExportStyleValidator);if !ok||native.contract!="StyleV2"{t.Fatal("style probe missing")}
 authority,ok:=native.authority.(*ExportFrozenStyleScope);if !ok{t.Fatal("style scope missing")}
 parentReader,ok:=authority.reader.(ExportJSONNodeResolver);if !ok{t.Fatal("physical parent reader missing")}
 parentValidator,ok:=parentReader.Validator.(ExportFrozenDocumentValidator);if !ok||parentValidator.Dependencies!=nil{t.Fatal("parent reader can recurse into style")}
 if catalog.calls!=0||objects.opens!=0{t.Fatal("constructor performed IO")}
}

func TestExportStyleInstallationRejectsIncompleteAdapters(t *testing.T){
 _,_,intent,frozen,_:=exportExecutionFixture(t)
 scratch,err:=NewExportArchiveScratch(t.TempDir(),1000);if err!=nil{t.Fatal(err)}
 _,catalog,objects,_:=importReaderFixture()
 for _,mode:=range []string{"catalog","objects","policy","probe","scratch","runtime","correction_source","person_policy","prompt_policy","body","cancelled"}{t.Run(mode,func(t *testing.T){
  installation:=ExportStyleValidatorInstallation{Body:exportBodyFactoryStub{},PersonPolicy:exportStatsPersonPolicyFactoryStub{},PromptPolicy:exportPromptMaskPolicyFactoryStub{},Base:ExportDocumentValidatorInstallation{ICCSHA:rawSHA([]byte("icc")),ModelSHA:rawSHA([]byte("model")),Semantics:&exportSemanticsStub{},Other:exportReaderValidator{&importValidatorStub{}}},Scratch:scratch,Probe:&exportStyleProbeStub{},Models:map[string]string{},RuntimeSHA:rawSHA([]byte("runtime"))}
  var c ExportDocumentCatalog=exportCorrectionReaderCatalog{exportReaderCatalog{catalog}};var o ImportDocumentObjects=objects;ctx:=context.Background()
  switch mode{case "body":installation.Body=nil;case "person_policy":installation.PersonPolicy=nil;case "prompt_policy":installation.PromptPolicy=nil;case "correction_source":c=exportReaderCatalog{catalog};case "catalog":c=nil;case "objects":o=nil;case "policy":installation.Base.Other=nil;case "probe":installation.Probe=nil;case "scratch":installation.Scratch=nil;case "runtime":installation.RuntimeSHA="bad";case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  if _,err:=installation.BuildScopedExportDocumentValidator(ctx,intent,frozen,c,o);err==nil{t.Fatal("incomplete scoped style installed")}
 })}
}

type exportCorrectionReaderCatalog struct{ExportDocumentCatalog}
func(exportCorrectionReaderCatalog)LookupExportCorrectionEvidence(context.Context,v2input.Entry,[]byte)(ExportCorrectionProducerEvidence,error){return ExportCorrectionProducerEvidence{},v2input.ErrInvalid}

func(exportCorrectionReaderCatalog)QualifyExportLearningOccurrence(context.Context,v2draft.LayerRef,[]byte)error{return v2input.ErrInvalid}

func(exportCorrectionReaderCatalog)QualifyExportCorrectionMember(context.Context,v2input.SessionVersionBinding,v2input.Ref,[]byte)error{return v2input.ErrInvalid}

func(exportCorrectionReaderCatalog)QualifyExportRenderFrame(context.Context,v2draft.LayerRef,[]byte)error{return v2input.ErrInvalid}

func(exportCorrectionReaderCatalog)QualifyExportRenderFrameArray(context.Context,v2draft.LayerRef)error{return v2input.ErrInvalid}

func(exportCorrectionReaderCatalog)QualifyExportStats(context.Context,v2input.Entry,[]byte)error{return v2input.ErrInvalid}

type exportStatsPersonPolicyFactoryStub struct{}
func(exportStatsPersonPolicyFactoryStub)BuildExportStatsPersonPolicy(context.Context,CreateIntent,FrozenExportDocuments,ExportDocumentCatalog,ImportDocumentObjects)(ExportStatsPersonPolicyAuthority,error){return &statsPersonPolicyDenied{},nil}
func(exportCorrectionReaderCatalog)LookupExportStatsOrigin(context.Context,v2input.Entry,[]byte)(ExportStatsOriginEvidence,error){return ExportStatsOriginEvidence{},v2input.ErrInvalid}
func(exportCorrectionReaderCatalog)LookupExportStatsPersonOrigin(context.Context,v2input.Entry,v2input.Ref,[]byte,review.WorkingImage)(ExportStatsPersonOrigin,error){return ExportStatsPersonOrigin{},v2input.ErrInvalid}
func(exportCorrectionReaderCatalog)LookupExportStatsPersonDocument(context.Context,ExportStatsPersonOrigin,v2input.Entry)(objectstore.Object,error){return objectstore.Object{},v2input.ErrInvalid}

func(exportCorrectionReaderCatalog)LookupExportResolvedOrigin(context.Context,v2input.Entry,[]byte)(ExportResolvedOriginEvidence,error){return ExportResolvedOriginEvidence{},v2input.ErrInvalid}

func(exportCorrectionReaderCatalog)QualifyExportDenseWarpOccurrence(context.Context,v2input.Entry,[]byte,int,v2input.Entry)error{return v2input.ErrInvalid}

func(exportCorrectionReaderCatalog)LookupExportParamsOperation(context.Context,v2input.Entry,[]byte)(ExportParamsOperationEvidence,error){return ExportParamsOperationEvidence{},v2input.ErrInvalid}

func(exportCorrectionReaderCatalog)QualifyExportSeries(context.Context,v2input.Entry,[]byte)error{return v2input.ErrInvalid}

func(exportCorrectionReaderCatalog)LookupExportSeriesOrigin(context.Context,v2input.Entry,[]byte)(ExportSeriesOriginEvidence,error){return ExportSeriesOriginEvidence{},v2input.ErrInvalid}
func(exportCorrectionReaderCatalog)LookupExportSeriesOriginDocument(context.Context,ExportSeriesOriginEvidence,v2input.Entry)(objectstore.Object,error){return objectstore.Object{},v2input.ErrInvalid}

func(exportCorrectionReaderCatalog)QualifyExportSeriesStatImage(context.Context,ExportSeriesOriginEvidence,v2input.Entry,[]byte)error{return v2input.ErrInvalid}

func(exportCorrectionReaderCatalog)LookupExportSeriesMeasureOrigin(context.Context,ExportSeriesOriginEvidence,v2input.Entry,[]byte)(ExportSeriesMeasureOrigin,error){return ExportSeriesMeasureOrigin{},v2input.ErrInvalid}

func(exportCorrectionReaderCatalog)QualifyExportSeriesMeasureInputs(context.Context,ExportSeriesMeasureOrigin,v2input.Entry,[]byte,ExportClosureNode)error{return v2input.ErrInvalid}

func(exportCorrectionReaderCatalog)LookupExportSeriesMeasureMask(context.Context,ExportSeriesMeasureOrigin,v2input.Entry,[]byte,ExportClosureNode,v2input.Ref)(objectstore.Object,error){return objectstore.Object{},v2input.ErrInvalid}

func(exportCorrectionReaderCatalog)LookupExportSeriesMaskOrigin(context.Context,ExportSeriesMeasureOrigin,v2input.Entry,[]byte,ExportClosureNode,v2input.Ref)(ExportSeriesMaskOrigin,error){return ExportSeriesMaskOrigin{},v2input.ErrInvalid}

type exportPromptMaskPolicyFactoryStub struct{}
func(exportPromptMaskPolicyFactoryStub)BuildExportPromptMaskPolicy(context.Context,CreateIntent,FrozenExportDocuments,ExportDocumentCatalog,ImportDocumentObjects)(ExportSeriesMaskPolicyAuthority,error){return &seriesMaskPolicyStub{},nil}

// Construction-only stub. It supplies no native runtime validation evidence.
type exportBodyFactoryStub struct{}
func(exportBodyFactoryStub)BuildExportBodyOriginValidator(_ context.Context,_ CreateIntent,_ FrozenExportDocuments,_ ExportDocumentCatalog,_ ImportDocumentObjects,_ ExportClosureNodeResolver,_ ExportClosureNodeResolver,_ ExportDenseWarpOccurrenceAuthority,_ ExportSeriesMaskPolicyAuthority,other ExportTypedDocumentValidator)(*NativeExportBodyOriginValidator,error){return &NativeExportBodyOriginValidator{Other:other},nil}
