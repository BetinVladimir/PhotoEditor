package delivery

import (
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
)

// NewDelegatedNegativeSigningClaims assembles the complete session-free
// negative graph-claims path under one immutable Delivery delegation scope.
func NewDelegatedNegativeSigningClaims(tasks SQLDelegatedTaskCatalog,
 objects provenance.SigningObjects, policies *provenance.SigningNegativePolicyRegistry,
 producers provenance.DelegatedImageProducer) (provenance.DelegatedTaskClaims,error) {
 if tasks.DB==nil||objects==nil||policies==nil||producers==nil||
  ValidateSignerDelegationScope(tasks.Lease,tasks.Slot,tasks.Scope)!=nil||
  tasks.Ready.Tenant!=tasks.Scope.TenantID||
  tasks.Ready.Project!=tasks.Scope.ProjectID||
  tasks.Ready.ExportID!=tasks.Scope.ExportID||
  tasks.Ready.Ref.ArtifactID!=tasks.Scope.ExportArtifactID||
  tasks.Ready.Ref.SHA256!=tasks.Scope.ExportSHA||
  len(tasks.Bucket)<3||len(tasks.Bucket)>63{return nil,v2input.ErrInvalid}
 documents:=DelegatedTaskDocumentReader{Tasks:tasks,Objects:objects}
 adopted:=DelegatedAdoptedResultReader{Tasks:tasks,Documents:documents}
 historical:=DelegatedNegativeQualificationCatalog{Tasks:tasks,
  Documents:documents,Adopted:adopted}
 evidence:=provenance.BoundDelegatedNegativeEvidenceReader{
  Adopted:adopted,Documents:documents,
  Counter:provenance.BoundDelegatedNegativeMaskCounter{
   Catalog:DelegatedNegativeMaskCatalog{Tasks:tasks,Documents:documents,Adopted:adopted},
   Objects:objects},
  SourceArrays:provenance.BoundDelegatedNegativeSourceArraysVerifier{
   Catalog:DelegatedNegativeSourceArrayCatalog{Tasks:tasks,Documents:documents,Adopted:adopted},
   Objects:objects},
  Pixels:provenance.BoundDelegatedNegativePixelsVerifier{
   Catalog:DelegatedNegativePixelsCatalog{Tasks:tasks,Documents:documents,Adopted:adopted},
   Objects:objects},
  Qualification:provenance.BoundDelegatedNegativeQualificationContent{
   Historical:historical,Documents:documents,Policies:policies},
  Reviews:DelegatedCandidateReviewReader{Tasks:tasks,Documents:documents},
 }
 causal:=provenance.BoundDelegatedNegativeCausalSource{
  Evidence:evidence,Producers:producers,Documents:documents}
 return provenance.DelegatedNegativeSigningTaskResolver{Causal:causal},nil
}
