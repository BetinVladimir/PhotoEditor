import {collectionApplyRequest,readCollectionApplyAccepted} from './collection-apply-contract';
import {readRAWCapabilities} from '@photoeditor/api-client';
import {freezeLearningCorrection,readLearningCorrectionReceipt,type LearningCorrectionCreate} from './learning-correction';
import {readCaptureAlertPage} from './capture-alert-response';
import {archiveUUID,archiveUploadID,archiveRequestSHA,freezeArchiveUpload,readArchiveUploadReceipt,archivePartNumbers,archiveCompletionParts,readArchivePartURLs,readArchiveAccepted,readArchiveAbortAccepted,readArchiveStatus,type ArchiveUploadRequest,type ArchiveUploadReceipt,type ArchiveUploadedPart} from './interchange-archive-upload';
import {freezeInterchangeSnapshot,readInterchangeSnapshotReceipt,type InterchangeSnapshotRequest} from './interchange-export-snapshot';
import {freezeInterchangeCreate,readInterchangeCreateTicket,type InterchangeCreateRequest} from './interchange-create';
import {readMultiReplacementPage} from './multi-replacement-page';
import {readMultiReplacementScope} from './multi-replacement-result';
import {readPerspectiveReplacementPage} from './perspective-replacement-page';
import {readPerspectiveReplacementScope} from './perspective-replacement-result';
import {readNegativeResultSelection,readRenderResultSelection,readRestorationResultSelection} from './negative-result-selection';
import {ownRenderReviewBinding} from './review-binding';
import {readInterchangeDownload} from './interchange-download';
import {assertInterchangeSnapshot,assertInterchangeScope,readInterchangeJob,readInterchangeInspection,interchangeCloseRequest,readInterchangeCloseResult,interchangeDecisionRequest,readInterchangeDecisionResult} from './interchange-job';
import {freezeImportPersonMaskDecision,readImportPersonMaskDecision,type ImportPersonMaskDecision} from './interchange-person-mask-decision';
import {readImportPersonMaskCandidates,readImportPersonMaskEntries} from './interchange-person-mask-candidates';
import {readImportPersonMaskStatuses} from './interchange-person-mask-status';
import {readPreparedReviews,readPreparedReviewReceipt,readSavedPreparedReviews,type PreparedReviewDecision} from './interchange-prepared-review';
import {readDeliveryInspection} from './delivery-inspection';
import {deliveryDownloadTarget,readDeliveryDownloadIntent,readDeliveryDownloadLink} from './delivery-download';
import {readDeliveryPage,type DeliverySummary} from './delivery-list';
import {deliverySigningMIMEs,readDeliverySigningPolicies} from './delivery-signing-policies';
import type {DeliveryItem} from './delivery-items';
import {collectDeliveryItems} from './delivery-items';
import type {DeliveryPackage} from './delivery-package';
import {assertDeliveryScope,freezeDeliveryPublish,readDeliveryPackage,readPublishedDelivery,type DeliveryPublishIntent} from './delivery-package';
import {prepareMultiApply,readMultiApplied} from './multi-apply';
import type {MultiQuality} from './multi-quality';
import {readMultiQuality} from './multi-quality';
import {readMultiPreview,type MultiPreview} from './multi-preview';
import type {MultiProposal} from './multi-proposal';
import {freezeMultiProposalScope,readMultiProposal} from './multi-proposal';
import {freezeMultiIntent,readMultiCapabilities,multiUUID,type MultiIntent,type MultiOperation} from './multi-intent';
import {freezePerspectiveLinesSubmission,readPerspectiveLinesSubmissionReceipt,type PerspectiveLinesSubmissionIntent} from './perspective-lines-submission';
import {readPerspectiveDetectedLines} from './perspective-detected-lines';
import {freezeCollectionVersion,readCollectionVersionSelection,reconcileCollectionVersion,type CollectionVersionSelection} from './collection-version';
import {preparePerspectiveApply,readPerspectiveApplied} from './perspective-apply';
import type {PerspectiveQuality} from './perspective-quality';
import {readPerspectiveQuality} from './perspective-quality';
import {readPerspectivePreview,type PerspectivePreview} from './perspective-preview';
import type {PerspectiveProposal} from './perspective-proposal';
import {perspectiveProposalScope,readPerspectiveProposal} from './perspective-proposal';
import {freezePerspectiveLinesIntent,readPerspectiveLinesReceipt,type PerspectiveLinesIntent} from './perspective-lines';
import {freezePerspectiveIntent,type PerspectiveIntent} from './perspective-intent';
import {freezeNegativeIntent,type NegativeIntent} from './negative-intent';
import {readNegativeCapabilities} from './negative-capabilities';
import {freezeRelightAnalysisIntent,type RelightAnalysisIntent} from './relight-analysis';
import {freezeRelightPreparation,readPreparedRelight,type RelightPreparationRequest} from './relight-preparation';
import {freezeRelightCandidateQuery,readRelightCandidatePage,type RelightCandidateQuery} from './relight-candidates';
import {freezeRelightIntent,type RelightIntent} from './relight-intent';
import {freezeGlareDonorIntent,freezeGlareDonorPreparation,readPreparedGlareDonorIntent,readGlareDonorTicket,type GlareDonorIntent,type GlareDonorPreparation} from './glare-donor-intent';
import {freezeOpenEyesIntent,freezeOpenEyesSelection,readPreparedOpenEyesIntent,readOpenEyesTicket,type OpenEyesIntent,type OpenEyesSelection} from './open-eyes-intent';
import {freezeReflectionIntent,freezeReflectionPreparation,readPreparedReflectionIntent,readReflectionTicket,type ReflectionIntent,type ReflectionPreparation} from './reflection-intent';
import {freezeReflectionMaskPairQuery,readReflectionMaskPairPage,type ReflectionMaskPairQuery} from './reflection-mask-pairs';
import {freezeLocalPortraitSelection,readLocalPortraitPrepared,readLocalPortraitTicket,type LocalPortraitSelection} from './local-portrait-intent';
import {freezeGlareDonorCandidateQuery,freezeGlareSourceCandidateQuery,freezeGlareMaskPairQuery,readGlareDonorCandidatePage,readGlareSourceCandidatePage,readGlareMaskPairPage,type GlareDonorCandidateQuery,type GlareSourceCandidateQuery,type GlareMaskPairQuery} from './glare-donor-candidates';
import {freezeSceneApplyDecision,type SceneApplyDecision} from './scene-apply';
import {readRenderPolicy} from './render-policy';
import {renderInputSnapshotRequest,renderInputSnapshotDigest,renderInputSnapshotResult,type RenderInputSnapshotRequest} from './render-input-snapshot';
import {renderBatchSubmission,renderBatchTicket,type RenderBatchSubmission} from './render-batch';
import {renderFreezeRequest,renderFreezeResult,type RenderFreezeRequest} from './render-freeze';
import {readRegionDescriptor} from './region-preview-descriptor';
import {readLayerDocument,readLayerStack,type EditorLayerStack} from './editor-layers';
import {readSessionAnalyses} from './session-analysis-list';
import type {SceneAnalysisRequest} from './scene-analysis-request';
import {readSceneBoardDocument} from './scene-board';
import {v2ResourceData} from './v2-resource';
import {freezeRegionRequest,regionJobTicket} from './region-preview';
import type {RegionPreviewRequest} from './region-preview';
import type {CapturePerson,ShootSession,ShootSessionCreate,ShootSessionUpdate,CaptureAlert,CaptureFrameConfirmation,CaptureShotListView,CaptureShotRequirement} from "./capture-types";
import { ApiClient, signExport, signingPolicies, type SignCreate, type SemanticEditIntent } from "@photoeditor/api-client";
import type {ArtifactRef,ApplyAccepted,Capabilities,Collection,CollectionItem,DownloadJob,EditorDraft,ExportPreset,MaskRef,MediaLink,Member,OverrideRecord,Page,Review,RunAccepted,RunOutput,RunStatus,StyleProfile,StyleVersion,TenantSettings,TrainingPair,TrainingSet,UserProfile,V2DocumentResource,V2DraftResource,V2EditingRecipe,V2VersionBinding} from "./catalog-types";
import {freezeDeliveryCreate,readDeliveryCreateTicket,type DeliveryCreateRequest} from './delivery-create';
export class OwnerAPI {
  constructor(private client: ApiClient, readonly tenant: string) {}
  async recordCorrection(request:LearningCorrectionCreate,key:string,signal?:AbortSignal){
    const body=freezeLearningCorrection(request);
    if(!key||!key.trim())throw new Error('IDEMPOTENCY_KEY_REQUIRED');
    signal?.throwIfAborted();
    const result=await this.client.request<unknown>(
      this.path('/api/v1/tenants/{t}/corrections'),
      {method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();
    return readLearningCorrectionReceipt(result,body);
  }
  signExport(exportID:string,request:SignCreate,key:string,signal?:AbortSignal){return signExport(this.client,this.tenant,exportID,request,key,signal);}
  signingPolicies(exportID:string,signal?:AbortSignal){return signingPolicies(this.client,this.tenant,exportID,signal);}
  async deliverySigningPolicies(project:string,formats:ReadonlyArray<'jpeg'|'png'|'tiff'>,signal?:AbortSignal){
    assertDeliveryScope(this.tenant,project,project);
    const mimes=deliverySigningMIMEs(formats);
    signal?.throwIfAborted();
    const query=new URLSearchParams();for(const mime of mimes)query.append('mime',mime);
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/delivery-signing-policies?${query}`),{method:'GET',signal});
    signal?.throwIfAborted();
    return readDeliverySigningPolicies(value);
  }
  private path(value:string){return value.replace("{t}",encodeURIComponent(this.tenant));}
  async rawCapabilities(project:string,signal?:AbortSignal){
    archiveUUID(this.tenant);archiveUUID(project);signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/raw-capabilities`),{method:'GET',signal});
    signal?.throwIfAborted();return readRAWCapabilities(value,project);
  }

  async interchangeJob(project:string,id:string,signal?:AbortSignal){
    assertInterchangeScope(this.tenant,project,id);signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/interchange-jobs/${encodeURIComponent(id)}`),{method:'GET',signal});
    signal?.throwIfAborted();return readInterchangeJob(value,this.tenant,project,id);
  }
  async inspectInterchange(project:string,id:string,signal?:AbortSignal){
    assertInterchangeScope(this.tenant,project,id);signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/interchange-jobs/${encodeURIComponent(id)}/inspection`),{method:'GET',signal});
    signal?.throwIfAborted();return readInterchangeInspection(value,this.tenant,project,id);
  }
  async decideImportPersonMask(project:string,workflow:string,request:ImportPersonMaskDecision,signal?:AbortSignal){
    assertInterchangeScope(this.tenant,project,workflow);
    const body=freezeImportPersonMaskDecision(request);signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-jobs/${encodeURIComponent(workflow)}/person-mask-confirmations`),{method:'POST',body,signal});
    signal?.throwIfAborted();return readImportPersonMaskDecision(value,workflow,body);
  }
  async preparedImportReviews(project:string,workflow:string,signal?:AbortSignal){
    assertInterchangeScope(this.tenant,project,workflow);signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-jobs/${encodeURIComponent(workflow)}/prepared-reviews`),{method:'GET',signal});
    signal?.throwIfAborted();return readPreparedReviews(value,workflow);
  }
  async savedPreparedImportReviews(project:string,workflow:string,signal?:AbortSignal){
    assertInterchangeScope(this.tenant,project,workflow);signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-jobs/${encodeURIComponent(workflow)}/prepared-reviews/saved`),{method:'GET',signal});
    signal?.throwIfAborted();return readSavedPreparedReviews(value,workflow);
  }
  async decidePreparedImportReview(project:string,workflow:string,body:PreparedReviewDecision,key:string,signal?:AbortSignal){
    assertInterchangeScope(this.tenant,project,workflow);if(!archiveUUID(key))throw new Error('INTERCHANGE_PREPARED_REVIEW_INVALID');signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-jobs/${encodeURIComponent(workflow)}/prepared-reviews`),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();return readPreparedReviewReceipt(value,workflow,body);
  }
  async importPersonMaskStatuses(project:string,workflow:string,signal?:AbortSignal){
    assertInterchangeScope(this.tenant,project,workflow);signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-jobs/${encodeURIComponent(workflow)}/person-mask-confirmations`),{method:'GET',signal});
    signal?.throwIfAborted();return readImportPersonMaskStatuses(value,workflow);
  }
  async importPersonMaskCandidates(project:string,workflow:string,entry:string,signal?:AbortSignal){
    assertInterchangeScope(this.tenant,project,workflow);if(!archiveUUID(entry))throw new Error('INTERCHANGE_MASK_CANDIDATES_INVALID');signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-jobs/${encodeURIComponent(workflow)}/person-mask-candidates/${encodeURIComponent(entry)}`),{method:'GET',signal});
    signal?.throwIfAborted();return readImportPersonMaskCandidates(value,workflow,entry);
  }
  async importPersonMaskEntries(project:string,workflow:string,signal?:AbortSignal){
    assertInterchangeScope(this.tenant,project,workflow);signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-jobs/${encodeURIComponent(workflow)}/person-mask-candidates`),{method:'GET',signal});
    signal?.throwIfAborted();return readImportPersonMaskEntries(value,workflow);
  }
  async importPersonMaskPreview(project:string,workflow:string,entry:string,override:string,mask:string,signal?:AbortSignal){
    assertInterchangeScope(this.tenant,project,workflow);
    if(!archiveUUID(entry)||!archiveUUID(override)||!archiveUUID(mask))throw new Error('INTERCHANGE_MASK_PREVIEW_INVALID');
    signal?.throwIfAborted();
    const bytes=await this.client.requestBytes(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-jobs/${encodeURIComponent(workflow)}/person-mask-candidates/${encodeURIComponent(entry)}/previews/${encodeURIComponent(override)}/${encodeURIComponent(mask)}`),'image/png',8<<20,signal);
    signal?.throwIfAborted();
    if(bytes.length<24||bytes[0]!==137||bytes[1]!==80||bytes[2]!==78||bytes[3]!==71||bytes[4]!==13||bytes[5]!==10||bytes[6]!==26||bytes[7]!==10)throw new Error('INTERCHANGE_MASK_PREVIEW_INVALID');
    return new Uint8Array(bytes);
  }
  async interchangeSnapshot(project:string,id:string,signal?:AbortSignal){
    // Both projections use separate transactions; retry a revision race before displaying them.
    for(let attempt=0;attempt<3;attempt++){
      signal?.throwIfAborted();
      const [job,inspection]=await Promise.all([this.interchangeJob(project,id,signal),this.inspectInterchange(project,id,signal)]);
      signal?.throwIfAborted();
      if(job.revision===inspection.revision){assertInterchangeSnapshot(job,inspection);return {job,inspection};}
    }
    throw new Error('INTERCHANGE_REVISION_CHANGED');
  }
  async interchangeDownload(project:string,id:string,signal?:AbortSignal){
    const first=await this.interchangeSnapshot(project,id,signal);
    if(first.job.direction!=='export'||!first.job.bundle_artifact_id||first.inspection.status!=='completed')throw new Error('INTERCHANGE_DOWNLOAD_UNAVAILABLE');
    const outputs=await this.runOutputs(first.inspection.run_id,signal);
    signal?.throwIfAborted();
    const current=await this.interchangeSnapshot(project,id,signal);
    if(first.job.revision!==current.job.revision||first.inspection.run_id!==current.inspection.run_id||first.job.bundle_artifact_id!==current.job.bundle_artifact_id)throw new Error('INTERCHANGE_REVISION_CHANGED');
    signal?.throwIfAborted();return readInterchangeDownload(outputs,current.job,current.inspection);
  }
  async decideInterchangeReview(project:string,inspection:import('./interchange-job').InterchangeInspection,decisions:{entry_id:string;decision:'approve'|'reject'}[],key:string,signal?:AbortSignal){
    const owned=readInterchangeInspection(structuredClone(inspection),this.tenant,project,inspection.workflow_id);
    const body=interchangeDecisionRequest(owned,decisions,key);signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/interchange-jobs/${encodeURIComponent(owned.workflow_id)}/decisions`),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();return readInterchangeDecisionResult(value,owned,body.decisions.length);
  }
  async closeInterchangeReview(project:string,inspection:import('./interchange-job').InterchangeInspection,key:string,signal?:AbortSignal){
    const owned=readInterchangeInspection(structuredClone(inspection),this.tenant,project,inspection.workflow_id);
    const body=interchangeCloseRequest(owned,key);
    signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/interchange-jobs/${encodeURIComponent(owned.workflow_id)}/close-review`),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();
    return readInterchangeCloseResult(value,owned);
  }
  async beginInterchangeArchive(project:string,request:ArchiveUploadRequest,key:string,signal?:AbortSignal){
    const body=freezeArchiveUpload(request,key);
    if(!archiveUUID(this.tenant)||!archiveUUID(project))throw new Error('INTERCHANGE_ARCHIVE_UPLOAD_INVALID');
    signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-archive-uploads`),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();return readArchiveUploadReceipt(value,project,body);
  }
  async interchangeArchiveAttemptStatus(user:string,project:string,request:ArchiveUploadRequest,key:string,signal?:AbortSignal){
    const body=freezeArchiveUpload(request,key),upload=archiveUploadID(this.tenant,user,key);
    if(!archiveUUID(project))throw new Error('INTERCHANGE_ARCHIVE_UPLOAD_INVALID');signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-archive-uploads/attempt-status`),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();return readArchiveStatus(value,{upload_id:upload,state:'uploading',part_bytes:16777216,parts_total:Math.ceil(body.bytes/16777216),request_sha256:archiveRequestSHA(project,body)},body);
  }
  async interchangeArchiveParts(project:string,request:ArchiveUploadRequest,receipt:ArchiveUploadReceipt,parts:number[],signal?:AbortSignal){
    const pin=readArchiveUploadReceipt(receipt,project,request),owned=archivePartNumbers(pin,parts);
    if(!archiveUUID(this.tenant))throw new Error('INTERCHANGE_ARCHIVE_UPLOAD_INVALID');signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-archive-uploads/${pin.upload_id}/parts`),{method:'POST',body:{request_sha256:pin.request_sha256,parts:owned},signal});
    signal?.throwIfAborted();return readArchivePartURLs(value,pin,owned);
  }
  async completeInterchangeArchive(project:string,request:ArchiveUploadRequest,receipt:ArchiveUploadReceipt,parts:ArchiveUploadedPart[],signal?:AbortSignal){
    const pin=readArchiveUploadReceipt(receipt,project,request),owned=archiveCompletionParts(pin,parts);
    if(!archiveUUID(this.tenant))throw new Error('INTERCHANGE_ARCHIVE_UPLOAD_INVALID');signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-archive-uploads/${pin.upload_id}/complete`),{method:'POST',body:{request_sha256:pin.request_sha256,parts:owned},signal});
    signal?.throwIfAborted();return readArchiveAccepted(value,pin);
  }
  async interchangeArchiveStatus(project:string,request:ArchiveUploadRequest,receipt:ArchiveUploadReceipt,signal?:AbortSignal){
    const pin=readArchiveUploadReceipt(receipt,project,request);
    if(!archiveUUID(this.tenant))throw new Error('INTERCHANGE_ARCHIVE_UPLOAD_INVALID');signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-archive-uploads/${pin.upload_id}?request_sha256=${pin.request_sha256}`),{signal});
    signal?.throwIfAborted();return readArchiveStatus(value,pin,request);
  }
  async abortInterchangeArchive(project:string,request:ArchiveUploadRequest,receipt:ArchiveUploadReceipt,signal?:AbortSignal){
    const pin=readArchiveUploadReceipt(receipt,project,request);
    if(!archiveUUID(this.tenant))throw new Error('INTERCHANGE_ARCHIVE_UPLOAD_INVALID');signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-archive-uploads/${pin.upload_id}/abort`),{method:'POST',body:{request_sha256:pin.request_sha256},signal});
    signal?.throwIfAborted();return readArchiveAbortAccepted(value,pin);
  }
  async prepareInterchangeSnapshot(project:string,request:InterchangeSnapshotRequest,key:string,signal?:AbortSignal){
    const body=freezeInterchangeSnapshot(request,key);
    assertInterchangeScope(this.tenant,project,body.items[0].binding.asset_version_id);signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-export-snapshots`),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();return readInterchangeSnapshotReceipt(value,body);
  }
  async createInterchange(request:InterchangeCreateRequest,key:string,signal?:AbortSignal){
    const body=freezeInterchangeCreate(request,key);
    assertInterchangeScope(this.tenant,body.project_id,body.direction==='import'?body.bundle.artifact_id:body.snapshot_id);
    signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path('/api/v1/tenants/{t}/interchange-jobs'),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();
    return readInterchangeCreateTicket(value);
  }
  async createDelivery(request:DeliveryCreateRequest,key:string,signal?:AbortSignal){
    const body=freezeDeliveryCreate(request,key);
    assertDeliveryScope(this.tenant,body.project_id,body.snapshot_id);
    signal?.throwIfAborted();
    const result=await this.client.request<unknown>(this.path('/api/v1/tenants/{t}/delivery-packages'),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();
    return readDeliveryCreateTicket(result);
  }
  async inspectDelivery(pkg:DeliveryPackage,items:readonly DeliveryItem[],signal?:AbortSignal){
    const owned=structuredClone(pkg),selection=structuredClone(items);
    assertDeliveryScope(this.tenant,owned.project_id,owned.id);
    if(owned.tenant_id!==this.tenant||!['review','ready'].includes(owned.state)||owned.published_at!==null||owned.document===null)throw new Error('DELIVERY_INSPECTION_INVALID');
    if(signal?.aborted)throw new Error('DELIVERY_ABORTED');
    const query=new URLSearchParams({revision:owned.revision});
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/delivery-packages/${encodeURIComponent(owned.id)}/inspection?${query}`),{method:'GET',signal});
    if(signal?.aborted)throw new Error('DELIVERY_ABORTED');
    return readDeliveryInspection(value,owned,selection);
  }
  async deliveryItems(pkg:DeliveryPackage,signal?:AbortSignal){
    const owned=structuredClone(pkg);
    assertDeliveryScope(this.tenant,owned.project_id,owned.id);
    if(owned.tenant_id!==this.tenant)throw new Error('DELIVERY_PACKAGE_INVALID');
    return collectDeliveryItems(owned,after=>{
      const query=new URLSearchParams({revision:owned.revision,limit:'200'});
      if(after){query.set('after_ordinal',String(after.ordinal));query.set('after_preset',after.preset_id)}
      return this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/delivery-packages/${encodeURIComponent(owned.id)}/items?${query}`),{method:'GET',signal});
    },signal);
  }
  async deliveryPackage(project:string,id:string,signal?:AbortSignal){
    assertDeliveryScope(this.tenant,project,id);
    if(signal?.aborted)throw new Error('DELIVERY_ABORTED');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/delivery-packages/${encodeURIComponent(id)}`),{method:'GET',signal});
    if(signal?.aborted)throw new Error('DELIVERY_ABORTED');
    return readDeliveryPackage(value,this.tenant,project,id);
  }
  async downloadDelivery(pkg:DeliveryPackage,item:DeliveryItem|undefined,key:string,signal?:AbortSignal){
    const owned=structuredClone(pkg),selected=item?structuredClone(item):undefined;
    assertDeliveryScope(this.tenant,owned.project_id,owned.id);
    if(owned.tenant_id!==this.tenant||typeof key!=='string'||!/^[\x21-\x7e]{1,128}$/.test(key))throw new Error('DELIVERY_DOWNLOAD_INVALID');
    const body=deliveryDownloadTarget(owned,selected);
    signal?.throwIfAborted();
    const issued=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/delivery-packages/${encodeURIComponent(owned.id)}/download-intent`),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();
    const token=readDeliveryDownloadIntent(issued);
    const redeemed=await this.client.request<unknown>('/api/v1/downloads/redeem',{method:'POST',body:{token},signal});
    signal?.throwIfAborted();return readDeliveryDownloadLink(redeemed,body.kind==='manifest'?owned.document!.sha256:undefined);
  }
  async deliveryPackageForWorkflow(project:string,workflow:string,signal?:AbortSignal){
    assertDeliveryScope(this.tenant,project,workflow);
    if(signal?.aborted)throw new Error('DELIVERY_ABORTED');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/delivery-packages/by-workflow/${encodeURIComponent(workflow)}`),{method:'GET',signal});
    if(signal?.aborted)throw new Error('DELIVERY_ABORTED');
    if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('DELIVERY_PACKAGE_INVALID');
    const id=(value as Record<string,unknown>).id;
    if(typeof id!=='string')throw new Error('DELIVERY_PACKAGE_INVALID');
    const pkg=readDeliveryPackage(value,this.tenant,project,id);
    if(pkg.workflow_id!==workflow)throw new Error('DELIVERY_PACKAGE_INVALID');
    return pkg;
  }
  async deliveryPackageForRun(run:string,signal?:AbortSignal){
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(run))throw new Error('DELIVERY_RUN_INVALID');
    if(signal?.aborted)throw new Error('DELIVERY_ABORTED');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/delivery-packages/by-run/${encodeURIComponent(run)}`),{method:'GET',signal});
    if(signal?.aborted)throw new Error('DELIVERY_ABORTED');
    if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('DELIVERY_PACKAGE_INVALID');
    const wire=value as Record<string,unknown>,data=wire.data;
    if(typeof wire.id!=='string'||!data||typeof data!=='object'||Array.isArray(data)||typeof (data as Record<string,unknown>).project_id!=='string')throw new Error('DELIVERY_PACKAGE_INVALID');
    return readDeliveryPackage(value,this.tenant,(data as Record<string,string>).project_id,wire.id);
  }
  async deliveryPackages(project:string,cursor?:string,seen?:ReadonlySet<string>,signal?:AbortSignal,after?:Pick<DeliverySummary,'id'|'created_at'>){
    assertDeliveryScope(this.tenant,project,project);
    if(signal?.aborted)throw new Error('DELIVERY_ABORTED');
    const query=new URLSearchParams({project_id:project,limit:'50'});
    if(cursor)query.set('cursor',cursor);
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/delivery-packages?${query}`),{method:'GET',signal});
    if(signal?.aborted)throw new Error('DELIVERY_ABORTED');
    return readDeliveryPage(value,project,seen,after);
  }
  async publishDelivery(intent:DeliveryPublishIntent,key:string,signal?:AbortSignal){
    const owned=freezeDeliveryPublish(intent,key);
    assertDeliveryScope(this.tenant,owned.project_id,owned.id);
    if(signal?.aborted)throw new Error('DELIVERY_ABORTED');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/delivery-packages/${encodeURIComponent(owned.id)}/publish`),{method:'POST',body:{expected_revision:owned.expected_revision},idempotencyKey:key,signal});
    if(signal?.aborted)throw new Error('DELIVERY_ABORTED');
    return readPublishedDelivery(value,this.tenant,owned);
  }

  async reconcileCollectionVersion(request:CollectionVersionSelection,signal?:AbortSignal){
    const owned=freezeCollectionVersion(request);
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/collections/${encodeURIComponent(owned.collection)}/items/${encodeURIComponent(owned.item)}/version`),{method:'GET',signal});
    if(signal?.aborted)throw new Error('COLLECTION_VERSION_ABORTED');
    return reconcileCollectionVersion(value,owned);
  }
  async replaceCollectionVersion(request:CollectionVersionSelection,signal?:AbortSignal){
    const owned=freezeCollectionVersion(request);
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/collections/${encodeURIComponent(owned.collection)}/items/${encodeURIComponent(owned.item)}/version`),{method:'PUT',body:{expected_revision:owned.revision,expected_asset_version_id:owned.expectedVersion,asset_version_id:owned.version},headers:{'If-Match':`"${owned.revision}"`},signal});
    if(signal?.aborted)throw new Error('COLLECTION_VERSION_ABORTED');
    return readCollectionVersionSelection(value,owned);
  }
  async applyPerspective(review:PerspectiveQuality,confirmGeometryReset:boolean,acceptQualityWarnings:boolean,signal?:AbortSignal){
    const owned=structuredClone(review);
    readPerspectiveProposal(owned.proposal,this.tenant,owned.proposal.project_id,owned.proposal.producer_task_id,owned.proposal.base);
    const body=preparePerspectiveApply(owned,confirmGeometryReset,acceptQualityWarnings),p=owned.proposal;
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(p.project_id)}/versions/${encodeURIComponent(p.base.asset_version_id)}/perspective-proposals/${encodeURIComponent(p.producer_task_id)}/apply`),{method:'POST',body,signal});
    if(signal?.aborted)throw new Error('PERSPECTIVE_APPLY_ABORTED');
    return readPerspectiveApplied(value,owned);
  }
  async perspectiveQuality(proposal:PerspectiveProposal,signal?:AbortSignal){
    const owned=readPerspectiveProposal(proposal,this.tenant,proposal.project_id,proposal.producer_task_id,proposal.base);
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(owned.project_id)}/versions/${encodeURIComponent(owned.base.asset_version_id)}/perspective-proposals/${encodeURIComponent(owned.producer_task_id)}/quality`),{method:'GET',signal});
    if(signal?.aborted)throw new Error('PERSPECTIVE_QUALITY_ABORTED');
    return readPerspectiveQuality(value,owned);
  }
  async perspectivePreview(proposal:PerspectiveProposal,variant:PerspectivePreview['variant']='preview1600',signal?:AbortSignal){
    const owned=readPerspectiveProposal(proposal,this.tenant,proposal.project_id,proposal.producer_task_id,proposal.base);
    if(!['preview1600','preview2560'].includes(variant))throw new Error('PERSPECTIVE_PREVIEW_INVALID');
    if(signal?.aborted)throw new Error('PERSPECTIVE_PREVIEW_ABORTED');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(owned.project_id)}/versions/${encodeURIComponent(owned.base.asset_version_id)}/perspective-proposals/${encodeURIComponent(owned.producer_task_id)}/previews/${variant}`),{method:'GET',signal});
    if(signal?.aborted)throw new Error('PERSPECTIVE_PREVIEW_ABORTED');
    return readPerspectivePreview(value,owned,variant);
  }
  async submitPerspectiveLineDetection(project:string,request:PerspectiveLinesSubmissionIntent,key:string,signal?:AbortSignal){
    const body=freezePerspectiveLinesSubmission(this.tenant,project,request);
    if(!/^[A-Za-z0-9._:-]{1,128}$/.test(key))throw new Error('IDEMPOTENCY_KEY_REQUIRED');
    if(signal?.aborted)throw new Error('PERSPECTIVE_DETECTION_ABORTED');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/perspective-line-detections`),{method:'POST',body,idempotencyKey:key,signal});
    if(signal?.aborted)throw new Error('PERSPECTIVE_DETECTION_ABORTED');
    return readPerspectiveLinesSubmissionReceipt(value);
  }
  async perspectiveDetectedLines(project:string,producer:string,base:V2VersionBinding,signal?:AbortSignal){
    const scope=perspectiveProposalScope(this.tenant,project,producer,base);
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(scope.project)}/versions/${encodeURIComponent(scope.base.asset_version_id)}/perspective-line-detections/${encodeURIComponent(scope.producer)}`),{method:'GET',signal});
    return readPerspectiveDetectedLines(value,scope.tenant,scope.project,scope.producer,scope.base);
  }
  async multiReplacementPage(review:string,decisionHash:string,after:string|null,signal?:AbortSignal){
    const valid=(v:string)=>/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
    if(!valid(review)||!/^[0-9a-f]{64}$/.test(decisionHash)||(after!==null&&!valid(after)))throw new Error('MULTI_REPLACEMENT_PAGE_INVALID');
    const value=await this.get<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(review)}/multi-replacements?decision_hash=${encodeURIComponent(decisionHash)}${after===null?'':`&after=${encodeURIComponent(after)}`}`,signal);
    if(signal?.aborted)throw new Error('MULTI_RESULT_CANCELLED');
    return readMultiReplacementPage(value,this.tenant,review,decisionHash,after);
  }
  async perspectiveReplacementPage(review:string,decisionHash:string,after:string|null,signal?:AbortSignal){
    const valid=(v:string)=>/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
    if(!valid(review)||!/^[0-9a-f]{64}$/.test(decisionHash)||(after!==null&&!valid(after)))throw new Error('PERSPECTIVE_REPLACEMENT_PAGE_INVALID');
    const value=await this.get<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(review)}/perspective-replacements?decision_hash=${encodeURIComponent(decisionHash)}${after===null?'':`&after=${encodeURIComponent(after)}`}`,signal);
    if(signal?.aborted)throw new Error('PERSPECTIVE_RESULT_CANCELLED');
    return readPerspectiveReplacementPage(value,this.tenant,review,decisionHash,after);
  }
  async multiReplacementScope(operation:MultiOperation,project:string,review:string,originalTask:string,decisionHash:string,signal?:AbortSignal){
    const valid=(v:string)=>/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
    if(![this.tenant,project,review,originalTask].every(valid)||!/^[0-9a-f]{64}$/.test(decisionHash)||(operation!=='optical.panorama'&&operation!=='optical.focus_stack'))throw new Error('MULTI_REPLACEMENT_SCOPE_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/reviews/${encodeURIComponent(review)}/multi-replacements/${encodeURIComponent(originalTask)}?decision_hash=${encodeURIComponent(decisionHash)}&operation=${encodeURIComponent(operation)}`),{method:'GET',signal});
    if(signal?.aborted)throw new Error('MULTI_RESULT_CANCELLED');
    return readMultiReplacementScope(value,this.tenant,project,review,originalTask,decisionHash,operation);
  }
  async perspectiveReplacementScope(project:string,review:string,originalTask:string,decisionHash:string,signal?:AbortSignal){
    const valid=(v:string)=>/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
    if(![this.tenant,project,review,originalTask].every(valid)||!/^[0-9a-f]{64}$/.test(decisionHash))throw new Error('PERSPECTIVE_REPLACEMENT_SCOPE_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/reviews/${encodeURIComponent(review)}/perspective-replacements/${encodeURIComponent(originalTask)}?decision_hash=${encodeURIComponent(decisionHash)}`),{method:'GET',signal});
    if(signal?.aborted)throw new Error('PERSPECTIVE_RESULT_CANCELLED');
    return readPerspectiveReplacementScope(value,this.tenant,project,review,originalTask,decisionHash);
  }
  async perspectiveProposal(project:string,producer:string,base:V2VersionBinding,signal?:AbortSignal){
    const scope=perspectiveProposalScope(this.tenant,project,producer,base);
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(scope.project)}/versions/${encodeURIComponent(scope.base.asset_version_id)}/perspective-proposals/${encodeURIComponent(scope.producer)}`),{method:'GET',signal});
    return readPerspectiveProposal(value,scope.tenant,scope.project,scope.producer,scope.base);
  }
  async publishPerspectiveLines(project:string,request:PerspectiveLinesIntent,key:string,signal?:AbortSignal){
    const body=freezePerspectiveLinesIntent(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('PERSPECTIVE_LINES_SUBMISSION_INVALID');
    const receipt=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/perspective-lines`),{method:'POST',body,idempotencyKey:key,signal});
    return readPerspectiveLinesReceipt(receipt,this.tenant,project,body);
  }
  async applyMulti(review:MultiQuality,intent:MultiIntent,confirmGeometryReset:boolean,acceptQualityWarnings:boolean,signal?:AbortSignal){
    const scope=freezeMultiProposalScope(this.tenant,review.proposal.project_id,review.proposal.producer_task_id,review.proposal.operation,intent);
    const owned=readMultiQuality(review,review.proposal,scope.intent);
    const body=prepareMultiApply(owned,confirmGeometryReset,acceptQualityWarnings,scope.intent),p=owned.proposal;
    if(signal?.aborted)throw new Error('MULTI_APPLY_ABORTED');
    const route=p.operation==='optical.panorama'?'panorama-proposals':'focus-stack-proposals';
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(p.project_id)}/${route}/${encodeURIComponent(p.producer_task_id)}/apply`),{method:'POST',body,signal});
    if(signal?.aborted)throw new Error('MULTI_APPLY_ABORTED');
    return readMultiApplied(value,owned);
  }
  async multiQuality(proposal:MultiProposal,intent:MultiIntent,signal?:AbortSignal){
    const ownedIntent=freezeMultiProposalScope(this.tenant,proposal.project_id,proposal.producer_task_id,proposal.operation,intent).intent;
    const owned=readMultiProposal(proposal,this.tenant,proposal.project_id,proposal.producer_task_id,proposal.operation,ownedIntent);
    if(signal?.aborted)throw new Error('MULTI_QUALITY_ABORTED');
    const route=owned.operation==='optical.panorama'?'panorama-proposals':'focus-stack-proposals';
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(owned.project_id)}/${route}/${encodeURIComponent(owned.producer_task_id)}/quality`),{method:'GET',signal});
    if(signal?.aborted)throw new Error('MULTI_QUALITY_ABORTED');
    return readMultiQuality(value,owned,ownedIntent);
  }
  async multiPreview(proposal:MultiProposal,intent:MultiIntent,variant:MultiPreview['variant']='preview1600',signal?:AbortSignal){
    const owned=readMultiProposal(proposal,this.tenant,proposal.project_id,proposal.producer_task_id,proposal.operation,intent);
    if(!['preview1600','preview2560'].includes(variant))throw new Error('MULTI_PREVIEW_INVALID');
    if(signal?.aborted)throw new Error('MULTI_PREVIEW_ABORTED');
    const route=owned.operation==='optical.panorama'?'panorama-proposals':'focus-stack-proposals';
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(owned.project_id)}/${route}/${encodeURIComponent(owned.producer_task_id)}/previews/${variant}`),{method:'GET',signal});
    if(signal?.aborted)throw new Error('MULTI_PREVIEW_ABORTED');
    return readMultiPreview(value,owned,variant);
  }
  async multiProposal(operation:MultiOperation,project:string,producer:string,intent:MultiIntent,signal?:AbortSignal){
    const scope=freezeMultiProposalScope(this.tenant,project,producer,operation,intent);
    const route=operation==='optical.panorama'?'panorama-proposals':'focus-stack-proposals';
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(scope.project)}/${route}/${encodeURIComponent(scope.producer)}`),{method:'GET',signal});
    if(signal?.aborted)throw new Error('MULTI_PROPOSAL_CANCELLED');
    return readMultiProposal(value,scope.tenant,scope.project,scope.producer,scope.operation,scope.intent);
  }
  async multiCapabilities(operation:MultiOperation,project:string,signal?:AbortSignal){
    if(!multiUUID(project)||(operation!=='optical.panorama'&&operation!=='optical.focus_stack'))throw new Error('MULTI_CAPABILITIES_INVALID');
    const route=operation==='optical.panorama'?'panorama-capabilities':'focus-stack-capabilities';
    return readMultiCapabilities(await this.get(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/${route}`,signal),this.tenant,project,operation);
  }
  async submitMulti(operation:MultiOperation,project:string,request:MultiIntent,key:string,signal?:AbortSignal){
    const body=freezeMultiIntent(operation,request);
    if(!multiUUID(project)||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('MULTI_SUBMISSION_INVALID');
    const route=operation==='optical.panorama'?'panorama-intents':'focus-stack-intents';
    return regionJobTicket(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/${route}`),{method:'POST',body,idempotencyKey:key,signal}));
  }
  async submitPerspective(project:string,request:PerspectiveIntent,key:string,signal?:AbortSignal){
    const body=freezePerspectiveIntent(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('PERSPECTIVE_SUBMISSION_INVALID');
    return regionJobTicket(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/perspective-intents`),{method:'POST',body,idempotencyKey:key,signal}));
  }
  async negativeCapabilities(project:string,signal?:AbortSignal){
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('NEGATIVE_PROJECT_INVALID');
    return readNegativeCapabilities(await this.get(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/negative-capabilities`,signal),this.tenant,project);
  }
  async submitNegative(project:string,request:NegativeIntent,key:string,signal?:AbortSignal){
    const body=freezeNegativeIntent(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('NEGATIVE_SUBMISSION_INVALID');
    return regionJobTicket(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/negative-intents`),{method:'POST',body,idempotencyKey:key,signal}));
  }
  async submitRelightAnalysis(project:string,request:RelightAnalysisIntent,key:string,signal?:AbortSignal){
    const body=freezeRelightAnalysisIntent(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('RELIGHT_PROJECT_INVALID');
    if(!/^[A-Za-z0-9._:-]{1,128}$/.test(key))throw new Error('RELIGHT_IDEMPOTENCY_INVALID');
    return regionJobTicket(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/relight-analyses`),{method:'POST',body,idempotencyKey:key,signal}));
  }
  async prepareRelight(project:string,request:RelightPreparationRequest,signal?:AbortSignal){
    const body=freezeRelightPreparation(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('RELIGHT_PROJECT_INVALID');
    return readPreparedRelight(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/relight-preparation`),{method:'POST',body,signal}),body);
  }
  async relightCandidates(project:string,request:RelightCandidateQuery,signal?:AbortSignal){
    const body=freezeRelightCandidateQuery(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('RELIGHT_PROJECT_INVALID');
    return readRelightCandidatePage(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/relight-candidates`),{method:'POST',body,signal}),body);
  }
  async submitRelight(project:string,request:RelightIntent,key:string,signal?:AbortSignal){
    const body=freezeRelightIntent(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('RELIGHT_SUBMISSION_INVALID');
    return regionJobTicket(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/relight-intents`),{method:'POST',body,idempotencyKey:key,signal}));
  }
  async submitGlareDonor(project:string,request:GlareDonorIntent,key:string,signal?:AbortSignal){
    const body=freezeGlareDonorIntent(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('GLARE_DONOR_SUBMISSION_INVALID');
    return readGlareDonorTicket(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/glare-donor-intents`),{method:'POST',body,idempotencyKey:key,signal}));
  }
  async submitOpenEyes(project:string,request:OpenEyesIntent,key:string,signal?:AbortSignal){
    const body=freezeOpenEyesIntent(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('OPEN_EYES_SUBMISSION_INVALID');
    return readOpenEyesTicket(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/open-eyes-intents`),{method:'POST',body,idempotencyKey:key,signal}));
  }
  async prepareOpenEyes(project:string,request:OpenEyesSelection,key:string,signal?:AbortSignal){
    const body=freezeOpenEyesSelection(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('OPEN_EYES_PREPARATION_INVALID');
    return readPreparedOpenEyesIntent(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/open-eyes-preparations`),{method:'POST',body,idempotencyKey:key,signal}),body);
  }
  async openEyesSourceCandidates(project:string,query:GlareSourceCandidateQuery,signal?:AbortSignal){
    const body=freezeGlareSourceCandidateQuery(query);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('OPEN_EYES_SOURCE_QUERY_INVALID');
    return readGlareSourceCandidatePage(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/open-eyes-source-candidates`),{method:'POST',body,signal}),body);
  }
  async openEyesDonorCandidates(project:string,query:GlareDonorCandidateQuery,signal?:AbortSignal){
    const body=freezeGlareDonorCandidateQuery(query);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('OPEN_EYES_DONOR_QUERY_INVALID');
    return readGlareDonorCandidatePage(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/open-eyes-donor-candidates`),{method:'POST',body,signal}),body);
  }
  async openEyesMaskPairs(project:string,query:GlareMaskPairQuery,signal?:AbortSignal){
    const body=freezeGlareMaskPairQuery(query);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('OPEN_EYES_MASK_QUERY_INVALID');
    return readGlareMaskPairPage(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/open-eyes-mask-pairs`),{method:'POST',body,signal}),body);
  }
  async submitReflection(project:string,request:ReflectionIntent,key:string,signal?:AbortSignal){
    const body=freezeReflectionIntent(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('REFLECTION_SUBMISSION_INVALID');
    return readReflectionTicket(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/reflection-intents`),{method:'POST',body,idempotencyKey:key,signal}));
  }
  async prepareReflection(project:string,request:ReflectionPreparation,key:string,signal?:AbortSignal){
    const body=freezeReflectionPreparation(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('REFLECTION_PREPARATION_INVALID');
    return readPreparedReflectionIntent(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/reflection-preparations`),{method:'POST',body,idempotencyKey:key,signal}),body);
  }
  async prepareGlareDonor(project:string,request:GlareDonorPreparation,key:string,signal?:AbortSignal){
    const body=freezeGlareDonorPreparation(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('GLARE_DONOR_PREPARATION_INVALID');
    return readPreparedGlareDonorIntent(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/glare-donor-preparations`),{method:'POST',body,idempotencyKey:key,signal}),body);
  }
  async glareDonorCandidates(project:string,query:GlareDonorCandidateQuery,signal?:AbortSignal){
    const body=freezeGlareDonorCandidateQuery(query);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('GLARE_DONOR_CANDIDATE_QUERY_INVALID');
    return readGlareDonorCandidatePage(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/glare-donor-candidates`),{method:'POST',body,signal}),body);
  }
  async glareSourceCandidates(project:string,query:GlareSourceCandidateQuery,signal?:AbortSignal){
    const body=freezeGlareSourceCandidateQuery(query);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('GLARE_SOURCE_CANDIDATE_QUERY_INVALID');
    return readGlareSourceCandidatePage(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/glare-source-candidates`),{method:'POST',body,signal}),body);
  }
  async glareMaskPairs(project:string,query:GlareMaskPairQuery,signal?:AbortSignal){
    const body=freezeGlareMaskPairQuery(query);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('GLARE_MASK_PAIR_QUERY_INVALID');
    return readGlareMaskPairPage(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/glare-mask-pairs`),{method:'POST',body,signal}),body);
  }
  async reflectionMaskPairs(project:string,query:ReflectionMaskPairQuery,signal?:AbortSignal){
    const body=freezeReflectionMaskPairQuery(query);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('REFLECTION_MASK_PAIR_QUERY_INVALID');
    return readReflectionMaskPairPage(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/reflection-mask-pairs`),{method:'POST',body,signal}),body);
  }
  async prepareLocalPortrait(project:string,request:LocalPortraitSelection,key:string,signal?:AbortSignal){
    const body=freezeLocalPortraitSelection(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('LOCAL_PORTRAIT_PREPARATION_INVALID');
    return readLocalPortraitPrepared(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/local-portrait-preparations`),{method:'POST',body,idempotencyKey:key,signal}),body);
  }
  async localPortraitMaskPairs(project:string,query:ReflectionMaskPairQuery,signal?:AbortSignal){
    const body=freezeReflectionMaskPairQuery(query);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('LOCAL_PORTRAIT_MASK_PAIR_QUERY_INVALID');
    return readReflectionMaskPairPage(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/local-portrait-mask-pairs`),{method:'POST',body,signal}),body);
  }
  async submitLocalPortrait(project:string,request:LocalPortraitSelection,key:string,signal?:AbortSignal){
    const body=freezeLocalPortraitSelection(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('LOCAL_PORTRAIT_SUBMISSION_INVALID');
    return readLocalPortraitTicket(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/local-portrait-intents`),{method:'POST',body,idempotencyKey:key,signal}));
  }
  async renderPolicy(project:string,signal?:AbortSignal){return readRenderPolicy(await this.get(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/render-policy`,signal));}
  async createRenderInputSnapshot(project:string,request:RenderInputSnapshotRequest,key:string,signal?:AbortSignal){const body=renderInputSnapshotRequest(request),digest=renderInputSnapshotDigest(body),count=body.sources.length;if(!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('IDEMPOTENCY_KEY_REQUIRED');return renderInputSnapshotResult(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/render-input-snapshots`),{method:'POST',body,idempotencyKey:key,signal}),digest,count);}
  async queueRenderBatch(project:string,request:RenderBatchSubmission,key:string,signal?:AbortSignal){const body=renderBatchSubmission(request);if(!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('IDEMPOTENCY_KEY_REQUIRED');return renderBatchTicket(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/render-batches`),{method:'POST',body,idempotencyKey:key,signal}));}
  async freezeRenderDraft(asset:string,request:RenderFreezeRequest,key:string,signal?:AbortSignal){const body=renderFreezeRequest(request);if(!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('IDEMPOTENCY_KEY_REQUIRED');return renderFreezeResult(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/assets/${encodeURIComponent(asset)}/v2-draft/freeze`),{method:'POST',body,idempotencyKey:key,signal}),body);}
  async regionDescriptor(asset:string,run:string,request:RegionPreviewRequest,source:ArtifactRef,signal?:AbortSignal){const expected=freezeRegionRequest(request),pinned=structuredClone(source);return readRegionDescriptor(await this.get(`/api/v1/tenants/{t}/assets/${encodeURIComponent(asset)}/region-previews/${encodeURIComponent(run)}/descriptor`,signal),asset,expected,pinned);}
  async regionPreview(asset:string,request:RegionPreviewRequest,key:string,signal?:AbortSignal){const body=freezeRegionRequest(request);return regionJobTicket(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/assets/${encodeURIComponent(asset)}/region-previews`),{method:'POST',body,idempotencyKey:key,signal}));}
  async analyzeSession(id:string,request:SceneAnalysisRequest,key:string,signal?:AbortSignal){return regionJobTicket(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(id)}/analyses`),{method:'POST',body:structuredClone(request),idempotencyKey:key,signal}));}
  async sessionAnalyses(sessionId:string,cursor?:string,signal?:AbortSignal){return readSessionAnalyses(await this.get(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(sessionId)}/analyses?limit=100${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`,signal),sessionId);}
  async applySceneAnalysis(analysisId:string,request:SceneApplyDecision,key:string,signal?:AbortSignal){
    const body=freezeSceneApplyDecision(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(analysisId)||analysisId==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('SCENE_APPLY_INVALID');
    return regionJobTicket(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/session-analyses/${encodeURIComponent(analysisId)}/apply`),{method:'POST',body,idempotencyKey:key,signal}));
  }
  async sceneBoard(analysisId:string,sessionId:string,signal?:AbortSignal){return readSceneBoardDocument(await this.get(`/api/v1/tenants/{t}/session-analyses/${encodeURIComponent(analysisId)}/scene-board`,signal),analysisId,sessionId);}
  get<T>(path:string,signal?:AbortSignal){return this.client.request<T>(this.path(path),{signal});}
  mutate<T>(path:string,body:unknown,key:string=crypto.randomUUID(),method="POST",headers?:HeadersInit,signal?:AbortSignal){return this.client.request<T>(this.path(path),{method,body,idempotencyKey:key,headers,signal});}
  async createShootSession(request:ShootSessionCreate,key:string=crypto.randomUUID()){return v2ResourceData<ShootSession>(await this.mutate('/api/v1/tenants/{t}/shoot-sessions',request,key),'shoot_sessions');}
  async updateShootSession(id:string,request:ShootSessionUpdate,key:string=crypto.randomUUID(),signal?:AbortSignal){return v2ResourceData<ShootSession>(await this.mutate(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(id)}`,structuredClone(request),key,'PATCH',undefined,signal),'shoot_sessions',id);}
  async shootSession(id:string,signal?:AbortSignal){return v2ResourceData<ShootSession>(await this.get(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(id)}`,signal),'shoot_sessions',id);}
  async shootSessions(project:string,cursor?:string,signal?:AbortSignal){const page=await this.get<Page<unknown>>(`/api/v1/tenants/{t}/shoot-sessions?project_id=${encodeURIComponent(project)}&limit=100${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`,signal);return {...page,items:page.items.map(item=>v2ResourceData<ShootSession>(item,'shoot_sessions'))};}
  async capturePeople(project:string,cursor?:string,signal?:AbortSignal){const page=await this.get<Page<unknown>>(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/persons?limit=100${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`,signal);return {...page,items:page.items.map(item=>v2ResourceData<CapturePerson>(item,'project_persons'))};}
  captureShotList(sessionId:string,signal?:AbortSignal){return this.get<CaptureShotListView>(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(sessionId)}/shot-list`,signal);}
  replaceCaptureShotList(sessionId:string,revision:string,requirements:CaptureShotRequirement[],key:string=crypto.randomUUID()){return this.mutate<CaptureShotListView>(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(sessionId)}/shot-list`,{expected_revision:revision,requirements},key,"PUT");}
  async captureAlerts(sessionId:string,cursor?:string,signal?:AbortSignal){const page=await this.get<unknown>(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(sessionId)}/alerts${cursor?`?cursor=${encodeURIComponent(cursor)}`:""}`,signal);signal?.throwIfAborted();return readCaptureAlertPage(page,sessionId);}
  acknowledgeCaptureAlert(alert:CaptureAlert,key:string=crypto.randomUUID()){return this.mutate<CaptureAlert>(`/api/v1/tenants/{t}/capture-alerts/${encodeURIComponent(alert.id)}/ack`,{expected_revision:alert.revision},key);}
  confirmCaptureFrame(receiptId:string,alertId:string,key:string=crypto.randomUUID()){return this.mutate<CaptureFrameConfirmation>(`/api/v1/tenants/{t}/capture-receipts/${encodeURIComponent(receiptId)}/confirm`,{alert_id:alertId},key);}
  projects(cursor?:string){return this.get(`/api/v1/tenants/{t}/projects${cursor?`?cursor=${encodeURIComponent(cursor)}`:""}`);}
  collection(id:string,signal?:AbortSignal){return this.get<Collection>(`/api/v1/tenants/{t}/collections/${encodeURIComponent(id)}`,signal);}
  items(id:string,cursor?:string){return this.get<Page<CollectionItem>>(`/api/v1/tenants/{t}/collections/${encodeURIComponent(id)}/items${cursor?`?cursor=${encodeURIComponent(cursor)}`:""}`);}
  setItemColorLabel(collection:Collection,item:CollectionItem,label:CollectionItem["color_label"]|null,signal?:AbortSignal){
    return this.client.request<CollectionItem>(this.path(`/api/v1/tenants/{t}/collections/${encodeURIComponent(collection.id)}/items/${encodeURIComponent(item.id)}`),{
      method:"PATCH",body:{color_label:label},headers:{"If-Match":`"${collection.revision}"`},signal,
    });
  }
  moderateApplyItem(collection:Collection,item:CollectionItem,decision:"accept"|"reject"){return this.mutate<CollectionItem>(`/api/v1/tenants/{t}/collections/${encodeURIComponent(collection.id)}/items/${encodeURIComponent(item.id)}/moderation`,{expected_revision:Number(collection.revision),decision});}
  run(id:string,signal?:AbortSignal){return this.get<RunStatus>(`/api/v1/tenants/{t}/runs/${encodeURIComponent(id)}`,signal);}
  semanticRunIntent(run:string,signal?:AbortSignal){return this.client.semanticRunIntent(this.tenant,run,signal);}
  cancelRun(id:string,signal?:AbortSignal){return this.client.request<RunStatus>(this.path(`/api/v1/tenants/{t}/runs/${encodeURIComponent(id)}/cancel`),{method:'POST',signal});}
  runOutputs(id:string,signal?:AbortSignal){return this.get<{items:RunOutput[]}>(`/api/v1/tenants/{t}/runs/${encodeURIComponent(id)}/outputs`,signal);}
  styles(projectId?:string,cursor?:string,signal?:AbortSignal){const q=new URLSearchParams();if(projectId)q.set("project_id",projectId);if(cursor)q.set("cursor",cursor);return this.get<Page<StyleProfile>>(`/api/v1/tenants/{t}/styles?${q}`,signal);}
  createStyle(projectId:string,name:string,description=""){return this.mutate<StyleProfile>(`/api/v1/tenants/{t}/styles`,{project_id:projectId,name,description});}
  style(id:string){return this.get<StyleProfile>(`/api/v1/tenants/{t}/styles/${encodeURIComponent(id)}`);}
  styleVersions(id:string,cursor?:string,signal?:AbortSignal){return this.get<Page<StyleVersion>>(`/api/v1/tenants/{t}/styles/${encodeURIComponent(id)}/versions${cursor?`?cursor=${encodeURIComponent(cursor)}`:""}`,signal);}
  createTrainingSet(styleId:string){return this.mutate<TrainingSet>(`/api/v1/tenants/{t}/styles/${encodeURIComponent(styleId)}/training-sets`,{});}
  trainingSet(id:string){return this.get<TrainingSet>(`/api/v1/tenants/{t}/training-sets/${encodeURIComponent(id)}`);}
  addPair(id:string,pair:{before_asset_version_id:string;after_asset_version_id:string;session_id:string;confirmed:boolean;included:boolean}){return this.mutate<TrainingPair>(`/api/v1/tenants/{t}/training-sets/${encodeURIComponent(id)}/pairs`,pair);}
  analyze(styleId:string,set:TrainingSet){return this.mutate<{run_id:string;status:string}>(`/api/v1/tenants/{t}/styles/${encodeURIComponent(styleId)}/analyses`,{training_set_id:set.id,training_set_revision:Number(set.revision),analysis_options:{alignment_mode:"auto",geometry_enabled:true,seed:0}});}
  async apply(collection:Pick<Collection,"id"|"revision">,styleVersionId:string,strength:number,outputName:string,key:string=crypto.randomUUID(),signal?:AbortSignal){const body=collectionApplyRequest(collection,styleVersionId,strength,outputName,key);signal?.throwIfAborted();const result=readCollectionApplyAccepted(await this.mutate<unknown>(`/api/v1/tenants/{t}/collections/${encodeURIComponent(collection.id)}/apply-profile`,body,key,"POST",undefined,signal));signal?.throwIfAborted();return result;}
  reviews(state="pending"){return this.get<Page<Review>>(`/api/v1/tenants/{t}/reviews?state=${encodeURIComponent(state)}`);}
  reviewsForRun(run:string,cursor?:string,signal?:AbortSignal){
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(run))throw new Error('REVIEW_RUN_INVALID');
    const query=new URLSearchParams({run_id:run});
    if(cursor)query.set('cursor',cursor);
    return this.get<Page<Review>>(`/api/v1/tenants/{t}/reviews?${query}`,signal);
  }
  reviewPreview(id:string,side:'before'|'after',signal:AbortSignal){if(!['before','after'].includes(side))throw new Error('REVIEW_PREVIEW_INVALID');return this.get<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(id)}/previews/${side}/preview1600`,signal);}
  async negativeResultSelection(review:Review,signal?:AbortSignal){const owned=ownRenderReviewBinding(review);const value=await this.get<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(owned.id)}/result-selection`,signal);return readNegativeResultSelection(value,owned);}
  async renderResultSelection(review:Review,signal?:AbortSignal){const owned=ownRenderReviewBinding(review);if(owned.kind!=='render.compose'||owned.state!=='decided')throw new Error('RENDER_RESULT_SELECTION_INVALID');const value=await this.get<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(owned.id)}/render-result-selection`,signal);return readRenderResultSelection(value,owned);}
  renderSelectedPreview(review:Review,signal:AbortSignal){const owned=ownRenderReviewBinding(review);if(owned.kind!=='render.compose'||owned.state!=='decided')throw new Error('RENDER_RESULT_SELECTION_INVALID');return this.get<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(owned.id)}/selected-preview/preview1600`,signal);}
  async restorationResultSelection(review:Review,signal?:AbortSignal){const owned=ownRenderReviewBinding(review);if((owned.kind!=='portrait.glare'&&owned.kind!=='portrait.open_eyes'&&owned.kind!=='image.reflection')||owned.state!=='decided')throw new Error('RESTORATION_RESULT_SELECTION_INVALID');const value=await this.get<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(owned.id)}/restoration-result-selection`,signal);return readRestorationResultSelection(value,owned);}
  restorationSelectedPreview(review:Review,signal:AbortSignal){const owned=ownRenderReviewBinding(review);if((owned.kind!=='portrait.glare'&&owned.kind!=='portrait.open_eyes'&&owned.kind!=='image.reflection')||owned.state!=='decided')throw new Error('RESTORATION_RESULT_SELECTION_INVALID');return this.get<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(owned.id)}/restoration-selected-preview/preview1600`,signal);}
  async review(id:string,signal?:AbortSignal){return ownRenderReviewBinding(await this.get<Review>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(id)}`,signal));}
  decide(review:Review,decision:"accept"|"reject"|"skip",accepted:string[]=[],rejected:string[]=[]){return this.mutate<Review>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(review.id)}/decision`,{expected_revision:review.revision,candidate_hash:review.candidate_hash,decision,accepted_candidate_ids:accepted,rejected_candidate_ids:rejected});}
  presets(cursor?:string,signal?:AbortSignal){const query=cursor?'?cursor='+encodeURIComponent(cursor):'';return this.client.request<Page<ExportPreset>>(this.path(`/api/v1/tenants/{t}/export-presets${query}`),{method:'GET',signal});}
  changePresetMetadataPolicy(preset:ExportPreset,metadata_policy:ExportPreset["config"]["metadata_policy"],key:string=crypto.randomUUID(),signal?:AbortSignal){return this.mutate<ExportPreset>(`/api/v1/tenants/{t}/export-presets/${encodeURIComponent(preset.id)}/metadata-policy`,{revision:preset.revision,metadata_policy},key,'PATCH',undefined,signal);}
  createPreset(name:string,config:ExportPreset["config"],key:string=crypto.randomUUID()){return this.mutate<ExportPreset>(`/api/v1/tenants/{t}/export-presets`,{name,config},key);}
  startDownload(selectionSnapshotId:string,presetId:string,key:string=crypto.randomUUID()){return this.mutate<{download_job_id:string;status:string}>(`/api/v1/tenants/{t}/download-jobs`,{selection_snapshot_id:selectionSnapshotId,preset_id:presetId},key);}
  selectionSnapshot(collection:Collection,key?:string,signal?:AbortSignal){return this.mutate<{id:string;digest:string;item_count:number;expires_at:string}>(`/api/v1/tenants/{t}/collections/${encodeURIComponent(collection.id)}/selection-snapshots`,{collection_revision:collection.revision,scope:"all"},key,"POST",undefined,signal);}
  proposeSemanticIntent(input:SemanticEditIntent,key:string,signal?:AbortSignal){return this.client.proposeSemanticIntent(this.tenant,input,key,signal);}
  contactSheet(selectionSnapshotId:string,locale:"ru"|"bg"|"en"="en",columns=4,rows=5){return this.mutate<RunAccepted>(`/api/v1/tenants/{t}/contact-sheets`,{selection_snapshot_id:selectionSnapshotId,locale,columns,rows});}
  download(id:string){return this.get<DownloadJob>(`/api/v1/tenants/{t}/download-jobs/${encodeURIComponent(id)}`);}
  draft(assetId:string,signal?:AbortSignal){return this.get<EditorDraft>(`/api/v1/tenants/{t}/assets/${encodeURIComponent(assetId)}/editor-draft`,signal);}
  saveDraft(assetId:string,draft:EditorDraft){return this.mutate<EditorDraft>(`/api/v1/tenants/{t}/assets/${encodeURIComponent(assetId)}/editor-draft`,{expected_revision:Number(draft.revision),asset_version_id:draft.asset_version_id,params:draft.params,mask_refs:draft.mask_refs},undefined,"PUT",{"If-Match":`"${draft.revision}"`});}
  preview(draft:EditorDraft,scale:"proxy"|"full"="proxy"){return this.mutate<RunAccepted>(`/api/v1/tenants/{t}/previews`,{asset_version_id:draft.asset_version_id,draft_revision:Number(draft.revision),params:draft.params,scale,generation:false});}
  promptMask(draft:EditorDraft,points:Array<{x:number;y:number;label:"positive"|"negative"}>){return this.mutate<RunAccepted>(`/api/v1/tenants/{t}/masks`,{asset_version_id:draft.asset_version_id,input_sha256:draft.source_sha256,selector:"generation",points});}
  manualMask(draft:EditorDraft,strokes:Array<{kind:"stroke";sequence:number;mode:"add"|"subtract"|"protect";points:Array<{x:number;y:number}>;radius:number;hardness:number}>,polygons:Array<{kind:"polygon";sequence:number;mode:"add"|"subtract"|"protect";points:Array<{x:number;y:number}>}>=[]){return this.mutate<RunAccepted>(`/api/v1/tenants/{t}/masks`,{asset_version_id:draft.asset_version_id,input_sha256:draft.source_sha256,selector:"manual",strokes,polygons});}
  async v2Layers(assetId:string,ref:ArtifactRef,binding:V2VersionBinding,signal?:AbortSignal){const pinned=structuredClone(ref),source=structuredClone(binding);return readLayerDocument(await this.get(`/api/v1/tenants/{t}/assets/${encodeURIComponent(assetId)}/v2-layers/${encodeURIComponent(pinned.artifact_id)}?sha256=${encodeURIComponent(pinned.sha256)}`,signal),pinned,source);}
  v2Draft(assetId:string,signal?:AbortSignal){return this.get<V2DraftResource>(`/api/v1/tenants/{t}/assets/${encodeURIComponent(assetId)}/v2-draft`,signal);}
  v2Binding(assetId:string,signal?:AbortSignal){return this.get<V2VersionBinding>(`/api/v1/tenants/{t}/assets/${encodeURIComponent(assetId)}/v2-binding`,signal);}
  v2DraftHistory(assetId:string){return this.get<Page<V2DocumentResource>>(`/api/v1/tenants/{t}/assets/${encodeURIComponent(assetId)}/v2-draft/history?limit=1`);}
  v2Document(documentId:string,signal?:AbortSignal){return this.get<V2EditingRecipe>(`/api/v1/tenants/{t}/v2-documents/${encodeURIComponent(documentId)}`,signal);}
  saveV2Draft(assetId:string,revision:string,recipe:V2EditingRecipe,layers:EditorLayerStack|null=null,key:string=crypto.randomUUID(),signal?:AbortSignal){return this.mutate<V2DraftResource>(`/api/v1/tenants/{t}/assets/${encodeURIComponent(assetId)}/v2-draft`,{expected_revision:revision,recipe:structuredClone(recipe),layers:layers===null?null:readLayerStack(layers,recipe.source)},key,"PUT",undefined,signal);}
  private protectedMask(output:RunOutput){
    if(output.role!=="mask_protected_ref"||output.media_type!=="application/json"||!output.artifact_id||!/^[0-9a-f]{64}$/.test(output.sha256))throw new Error("Invalid protected mask output");
    return {artifact_id:output.artifact_id,sha256:output.sha256};
  }
  async createV2Draft(assetId:string,version:StyleVersion,protection?:RunOutput){
    if(version.approval_status!=="accepted"||!version.export_artifact_id||!/^[0-9a-f]{64}$/.test(version.config_hash))throw new Error("No accepted StyleV2 artifact");
    const mask=protection?this.protectedMask(protection):undefined;
    const binding=await this.v2Binding(assetId);
    const recipe:V2EditingRecipe={schema_version:3,source:binding,base_style:{artifact_id:version.export_artifact_id,sha256:version.config_hash},session_corrections:null,learning_version_id:null,overrides:[],layer_stack:null,...(mask?{protected_mask:mask}:{}),renderer_version:"v2.2",parameter_delta:{ev:0,log_r:0,log_g:0,log_b:0,saturation_delta:0,retouch_delta:0}};
    return this.saveV2Draft(assetId,"0",recipe);
  }
  async bindProtectionToV2Draft(assetId:string,output:RunOutput){
    const mask=this.protectedMask(output);
    const draft=await this.v2Draft(assetId),history=await this.v2DraftHistory(assetId),current=history.items[0];
    if(!current||current.document.artifact_id!==draft.document.artifact_id||current.document.sha256!==draft.document.sha256)throw new Error("V2 draft changed");
    const recipe=await this.v2Document(current.id);
    if(recipe.schema_version!==3)throw new Error("Unsupported V2 draft");
    if(recipe.protected_mask&& (recipe.protected_mask.artifact_id!==mask.artifact_id||recipe.protected_mask.sha256!==mask.sha256))throw new Error("Protection is already pinned");
    if(recipe.protected_mask)return draft;
    const layers=recipe.layer_stack===null?null:await this.v2Layers(assetId,recipe.layer_stack,recipe.source);
    return this.saveV2Draft(assetId,draft.revision,{...recipe,protected_mask:mask},layers);
  }
  generate(draft:EditorDraft,mask:MaskRef,prompt:string,seed:number,variantCount=1){const protectedRefs=(draft.mask_refs as MaskRef[]).filter(value=>value?.protected===true);return this.mutate<RunAccepted>(`/api/v1/tenants/{t}/generations`,{asset_version_id:draft.asset_version_id,draft_revision:Number(draft.revision),mask_ref:mask,protected_refs:protectedRefs,params:{prompt,negative_prompt:"",seed,steps:25,guidance_scale:7.5,feather_pixels:8},variant_count:variantCount});}
  createOverride(draft:EditorDraft){const params={...draft.params,input_sha256:draft.source_sha256,revision:1};return this.mutate<OverrideRecord>(`/api/v1/tenants/{t}/overrides`,{asset_version_id:draft.asset_version_id,input_sha256:draft.source_sha256,params});}
  saveEdits(collection:Collection,draft:EditorDraft,overrideId:string,itemId:string,styleVersionId?:string){return this.mutate<ApplyAccepted>(`/api/v1/tenants/{t}/collections/${encodeURIComponent(collection.id)}/save-edits`,{source_revision:Number(collection.revision),scope:"selected",selected_item_ids:[itemId],overrides_id:overrideId,style_version_id:styleVersionId,output_name:`${collection.name} — edited`});}
  me(signal?:AbortSignal){return this.get<UserProfile>("/api/v1/me",signal);}
  updateMe(profile:UserProfile,patch:Partial<Pick<UserProfile,"locale"|"theme"|"timezone">>){return this.mutate<UserProfile>("/api/v1/me",patch,undefined,"PATCH",{"If-Match":`"${profile.revision}"`});}
  tenantSettings(){return this.get<TenantSettings>("/api/v1/tenants/{t}");}
  updateTenant(value:TenantSettings,patch:Partial<Pick<TenantSettings,"name"|"quotas"|"retention">>){return this.mutate<TenantSettings>("/api/v1/tenants/{t}",patch,undefined,"PATCH",{"If-Match":`"${value.revision}"`});}
  members(){return this.get<Page<Member>>("/api/v1/tenants/{t}/members");}
  invite(email:string,role:"admin"|"editor"|"viewer",locale:"en"|"ru"|"bg"="en"){return this.mutate(`/api/v1/tenants/{t}/invites`,{email,role,locale});}
  updateMember(member:Member,role:Member["role"],status:string){return this.mutate<Member>(`/api/v1/tenants/{t}/members/${encodeURIComponent(member.user_id)}`,{role,status},undefined,"PATCH",{"If-Match":`"${member.revision}"`});}
  capabilities(){return this.get<Capabilities>("/api/v1/tenants/{t}/capabilities");}
  mediaLinks(assetId:string,assetVersionId:string,variants:MediaLink["variant"][],signal?:AbortSignal){return this.mutate<{items:MediaLink[]}>(`/api/v1/tenants/{t}/assets/${encodeURIComponent(assetId)}/media-links`,{asset_version_id:assetVersionId,variants},undefined,"POST",undefined,signal);}
  streamEvents(cursor:string|undefined,onEvent:(event:{cursor:string;type:string;payload:unknown})=>void,signal:AbortSignal){return this.client.eventStream(this.path('/api/v1/tenants/{t}/events'),cursor,onEvent,signal);}
  events(cursor?:string,signal?:AbortSignal){return this.get<{items:Array<{cursor:string;type:string;payload:unknown}>;next_cursor?:string}>(`/api/v1/tenants/{t}/events${cursor?`?after=${encodeURIComponent(cursor)}`:""}`,signal);}
}
