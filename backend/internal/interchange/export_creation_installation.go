package interchange
import (
 "context"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/domain"
)
// Compose only independently installed adapters. Native probes and measured
// resource qualification are mandatory and cannot be synthesized by API config.
func NewSQLExportCreationPreparation(ctx context.Context,proposals ExportFrozenProposalSource,installation SQLExportExecutionInstaller)(ExportCreationPreparation,error){
 var zero ExportCreationPreparation
 if err:=ctx.Err();err!=nil{return zero,err}
 if proposals==nil||installation.API==nil||installation.Scheduler==nil||installation.API==installation.Scheduler||installation.Objects==nil||len(installation.Bucket)<3||len(installation.Bucket)>63||installation.Qualification==nil||installation.MaxInputBytes<1||installation.MaxInputBytes>128<<30{return zero,v2input.ErrInvalid}
 if (installation.Dependencies==nil)==(installation.DependencyFactory==nil)||(installation.Binaries==nil)==(installation.Native==nil){return zero,v2input.ErrInvalid}
 if installation.Native!=nil{
  native:=installation.Native
  if native.Scratch==nil||native.ImageProbe==nil||native.MaskProbe==nil||native.ArrayProbe==nil{return zero,v2input.ErrInvalid}
  if _,err:=domain.ParseDigest(native.ImageRuntimeSHA);err!=nil{return zero,v2input.ErrInvalid}
  if _,err:=domain.ParseDigest(native.ArrayRuntimeSHA);err!=nil{return zero,v2input.ErrInvalid}
  // Own configuration fields; process-owned probes/scratch retain their handles.
  owned:=*native;installation.Native=&owned
 }
 return ExportCreationPreparation{Proposals:proposals,Drafts:Service{DB:installation.API},Installation:installation},nil
}

// Complete SQL proposal path using the same native image authority and scratch
// budget as execution; encoded originals are never accepted by MIME alone.
func NewSQLNativeExportCreationPreparation(ctx context.Context,installation SQLExportExecutionInstaller,limits ExportLimits)(ExportCreationPreparation,error){
 var zero ExportCreationPreparation
 if err:=ctx.Err();err!=nil{return zero,err}
 if installation.Native==nil||installation.Binaries!=nil||limits.ArchiveBytes<1||limits.ArchiveBytes>64<<30||limits.ExpandedBytes<1||limits.ExpandedBytes>128<<30||limits.Files<1||limits.Files>30000||installation.MaxInputBytes<1{return zero,v2input.ErrInvalid}
 native:=*installation.Native
 installation.Native=&native
 originalLimit:=installation.MaxInputBytes
 if originalLimit>64<<30{originalLimit=64<<30}
 images:=NativeExportImageQualifier{Objects:installation.Objects,Scratch:native.Scratch,Probe:native.ImageProbe,RuntimeSHA:native.ImageRuntimeSHA}
 frames:=SQLExportProposalFrameFactory{DB:installation.API,Objects:installation.Objects,Bucket:installation.Bucket,Native:images,MaxBytes:originalLimit}
 inputs:=SQLExportProposalInputs{DB:installation.API,Objects:installation.Objects,Bucket:installation.Bucket,Frames:frames}
 return NewSQLExportCreationPreparation(ctx,ExportProposalSource{Inputs:inputs,Limits:limits},installation)
}
