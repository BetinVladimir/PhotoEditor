package workflow

import (
 "context"
 "github.com/google/uuid"
 "testing"
 "photoeditor/backend/internal/delivery"
)

func TestEmptyWatermarkDescriptorCannotBindPrepareChild(t *testing.T){
 var frozen FrozenDeliveryWatermarkTask
 if _,_,err:=frozen.InitialRequest(delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{});err==nil{t.Fatal("empty descriptor accepted")}
 if _,err:=frozen.PublishInputs(context.Background(),nil);err==nil{t.Fatal("empty descriptor published")}
}

func TestEmptyWatermarkDraftNeverReachesSQL(t *testing.T){
 var frozen FrozenDeliveryWatermarkTask
 tx:=&deliveryDraftTx{accepted:true}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=frozen.CreateDraft(ctx,tx,delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{},uuid.New(),1<<30);err!=context.Canceled{t.Fatal("cancel ignored",err)}
 if err:=frozen.CreateDraft(context.Background(),tx,delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{},uuid.New(),1<<30);err==nil{t.Fatal("empty draft accepted")}
 if _,err:=frozen.WithMetadataConfig(ctx,delivery.MetadataInventoryEntryStore{},delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{});err!=context.Canceled{t.Fatal("metadata cancellation ignored",err)}
 if tx.calls!=0{t.Fatal("invalid descriptor queried SQL")}
}

func TestEmptyWatermarkTransportNeverQueries(t *testing.T){
 var frozen FrozenDeliveryWatermarkTask
 tx:=&deliveryDraftTx{accepted:true}
 if _,err:=frozen.TransportRegistered(context.Background(),tx,delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{});err==nil{t.Fatal("empty transport accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=frozen.TransportRegistered(ctx,tx,delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{});err!=context.Canceled{t.Fatal("cancel ignored",err)}
 if tx.calls!=0{t.Fatal("invalid transport queried SQL")}
}

func TestEmptyWatermarkActivationNeverQueries(t *testing.T){
 var frozen FrozenDeliveryWatermarkTask
 tx:=&deliveryDraftTx{accepted:true}
 if err:=frozen.ActivateInTransaction(context.Background(),tx,delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{},uuid.New());err==nil{t.Fatal("empty activation accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=frozen.ActivateInTransaction(ctx,tx,delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{},uuid.New());err!=context.Canceled{t.Fatal("cancel ignored",err)}
 if tx.calls!=0{t.Fatal("invalid activation queried SQL")}
}

func TestEmptyWatermarkPresentationDraftNeverQueries(t *testing.T){
 var frozen FrozenDeliveryWatermarkPrefixTask
 tx:=&deliveryDraftTx{accepted:true}
 if err:=frozen.CreateInTransaction(context.Background(),tx,delivery.ExpansionLease{},delivery.FrozenExportRequest{});err==nil{t.Fatal("empty continuation accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=frozen.CreateInTransaction(ctx,tx,delivery.ExpansionLease{},delivery.FrozenExportRequest{});err!=context.Canceled{t.Fatal("cancel ignored",err)}
 if tx.calls!=0{t.Fatal("invalid continuation queried SQL")}
}

func TestEmptyWatermarkPresentationTransportNeverQueries(t *testing.T){
 var frozen FrozenDeliveryWatermarkPrefixTask
 tx:=&deliveryDraftTx{accepted:true}
 if _,err:=frozen.TransportRegistered(context.Background(),tx,delivery.ExpansionLease{},delivery.FrozenExportRequest{});err==nil{t.Fatal("empty continuation transport accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=frozen.TransportRegistered(ctx,tx,delivery.ExpansionLease{},delivery.FrozenExportRequest{});err!=context.Canceled{t.Fatal("cancel ignored",err)}
 if tx.calls!=0{t.Fatal("invalid transport queried SQL")}
}

func TestEmptyWatermarkPresentationActivationNeverQueries(t *testing.T){
 var frozen FrozenDeliveryWatermarkPrefixTask
 tx:=&deliveryDraftTx{accepted:true}
 if err:=frozen.ActivateInTransaction(context.Background(),tx,delivery.ExpansionLease{},delivery.FrozenExportRequest{});err==nil{t.Fatal("empty presentation activation accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=frozen.ActivationRegistered(ctx,tx,delivery.ExpansionLease{},delivery.FrozenExportRequest{});err!=context.Canceled{t.Fatal("cancel ignored",err)}
 if tx.calls!=0{t.Fatal("invalid activation queried SQL")}
}

func TestEmptyWatermarkPresentationEvidenceCannotOpenBarrier(t *testing.T){
 var verified VerifiedWatermarkPresentationStage
 tx:=&deliveryDraftTx{accepted:true}
 if err:=verified.BeginWatermarkInTransaction(context.Background(),tx,delivery.ExpansionLease{},delivery.FrozenExportRequest{},"photos");err==nil{t.Fatal("empty evidence accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=verified.BeginWatermarkInTransaction(ctx,tx,delivery.ExpansionLease{},delivery.FrozenExportRequest{},"photos");err!=context.Canceled{t.Fatal("cancel ignored",err)}
 if tx.calls!=0{t.Fatal("invalid evidence queried SQL")}
}

func TestEmptyWatermarkInitialDerivativeDraftNeverQueries(t *testing.T){
 var frozen FrozenDeliveryWatermarkPrefixTask
 tx:=&deliveryDraftTx{accepted:true}
 if err:=frozen.CreateDraft(context.Background(),tx,delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{},uuid.New(),1<<30);err==nil{t.Fatal("empty derivative shell accepted")}
 if tx.calls!=0{t.Fatal("invalid derivative shell queried SQL")}
}

func TestEmptyWatermarkInitialDerivativeTransportNeverQueries(t *testing.T){
 var frozen FrozenDeliveryWatermarkPrefixTask
 tx:=&deliveryDraftTx{accepted:true}
 if _,err:=frozen.InitialDerivativeTransportRegistered(context.Background(),tx,delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{});err==nil{t.Fatal("empty initial derivative transport accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=frozen.InitialDerivativeTransportRegistered(ctx,tx,delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{});err!=context.Canceled{t.Fatal("cancel ignored",err)}
 if tx.calls!=0{t.Fatal("invalid transport queried SQL")}
}

func TestEmptyWatermarkInitialDerivativeActivationNeverQueries(t *testing.T){
 var frozen FrozenDeliveryWatermarkPrefixTask
 tx:=&deliveryDraftTx{accepted:true}
 if err:=frozen.InitialDerivativeActivateInTransaction(context.Background(),tx,delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{},uuid.New());err==nil{t.Fatal("empty initial derivative activation accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=frozen.InitialDerivativeActivateInTransaction(ctx,tx,delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{},uuid.New());err!=context.Canceled{t.Fatal("cancel ignored",err)}
 if tx.calls!=0{t.Fatal("invalid activation queried SQL")}
}

func TestEmptyWatermarkResizeEvidenceCannotOpenSharpen(t *testing.T){
 var verified VerifiedWatermarkPresentationStage
 tx:=&deliveryDraftTx{accepted:true}
 if err:=verified.BeginSharpenInTransaction(context.Background(),tx,delivery.ExpansionLease{},delivery.FrozenExportRequest{},"photos");err==nil{t.Fatal("empty resize evidence accepted")}
 if tx.calls!=0{t.Fatal("invalid resize evidence queried SQL")}
}

func TestEmptyWatermarkSharpenDraftNeverQueries(t *testing.T){
 var frozen FrozenDeliveryWatermarkPrefixTask
 tx:=&deliveryDraftTx{accepted:true}
 if err:=frozen.CreateSharpenInTransaction(context.Background(),tx,delivery.ExpansionLease{},delivery.FrozenExportRequest{});err==nil{t.Fatal("empty sharpen continuation accepted")}
 if tx.calls!=0{t.Fatal("invalid sharpen continuation queried SQL")}
}

func TestEmptyWatermarkSharpenTransportNeverQueries(t *testing.T){
 var frozen FrozenDeliveryWatermarkPrefixTask
 tx:=&deliveryDraftTx{accepted:true}
 if _,err:=frozen.SharpenTransportRegistered(context.Background(),tx,delivery.ExpansionLease{},delivery.FrozenExportRequest{});err==nil{t.Fatal("empty sharpen transport accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=frozen.SharpenTransportRegistered(ctx,tx,delivery.ExpansionLease{},delivery.FrozenExportRequest{});err!=context.Canceled{t.Fatal("cancel ignored",err)}
 if tx.calls!=0{t.Fatal("invalid sharpen transport queried SQL")}
}

func TestEmptyWatermarkSharpenActivationNeverQueries(t *testing.T){
 var frozen FrozenDeliveryWatermarkPrefixTask
 tx:=&deliveryDraftTx{accepted:true}
 if err:=frozen.ActivateSharpenInTransaction(context.Background(),tx,delivery.ExpansionLease{},delivery.FrozenExportRequest{});err==nil{t.Fatal("empty sharpen activation accepted")}
 if tx.calls!=0{t.Fatal("invalid sharpen activation queried SQL")}
}

func TestEmptyWatermarkInitialActivationReceiptNeverQueries(t *testing.T){
 tx:=&deliveryDraftTx{accepted:true}
 if _,err:=(FrozenDeliveryWatermarkTask{}).ActivationRegistered(context.Background(),tx,delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{},uuid.New());err==nil{t.Fatal("empty watermark receipt accepted")}
 if _,err:=(FrozenDeliveryWatermarkPrefixTask{}).InitialDerivativeActivationRegistered(context.Background(),tx,delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{},uuid.New());err==nil{t.Fatal("empty derivative receipt accepted")}
 if tx.calls!=0{t.Fatal("invalid receipt queried SQL")}
}

func TestInitialWatermarkCoordinatorRequiresOwnedTaskAndDependencies(t *testing.T){
 if _,err:=admitInitialWatermark(context.Background(),nil,nil,nil,delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{},uuid.New(),1);err==nil{t.Fatal("missing dependencies accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=admitInitialWatermark(ctx,nil,nil,nil,delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{},uuid.New(),1);err!=context.Canceled{t.Fatal("cancel ignored",err)}
}

func TestCancelledInitialWatermarkAdmissionPrecedesDescriptorValidation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(FrozenDeliveryWatermarkTask{}).AdmitInitial(ctx,nil,nil,delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{},uuid.New(),1);err!=context.Canceled{t.Fatal("watermark cancellation ignored",err)}
 if _,err:=(FrozenDeliveryWatermarkPrefixTask{}).AdmitInitialDerivative(ctx,nil,nil,delivery.ExpansionLease{},delivery.PreparePlan{},delivery.ExportChildIntent{},uuid.New(),1);err!=context.Canceled{t.Fatal("derivative cancellation ignored",err)}
}
