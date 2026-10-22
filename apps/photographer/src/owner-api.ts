import {freezeContactSheetAdmission,readContactSheetAccepted} from './contact-sheet-response';
import {freezeSaveEditsBody,readSaveEditsResolution,type SaveEditsBody} from './editor-save-edits-attempt';
import {eventCursor,readTenantEvent,readTenantEventPage,freezeTenantInvitation,readInvitationReceipt} from './tenant-admin-response';
import {freezeExportBatchRequest,readExportBatchTicket} from './export-batch-attempt-store';
import {sessionAnalysisRequestSHA,readSessionAnalysisCapabilities} from './session-analysis-capabilities';
import {capturePolicySHA,readCaptureCapabilities} from './capture-capabilities';
import {readEditorCapabilities} from './editor-capabilities';
import {readExportPresetResponse,readPresetMetadataReceipt,freezeExportPresetCreate,readCreatedExportPreset,readExportPresetPage} from "./export-preset-response";
import {readAnalysisCapabilities} from './analysis-capabilities';
import {readApplyCapabilities} from './collection-apply-capabilities';
import {readLegacyRunAccepted,readLegacyMaskAccepted,readGenerationAbandonment,readLegacyOverride} from './legacy-producer-dto';
import {readV2DraftResource,readV2DraftHistory} from './v2-draft-dto';
import {readEditingRecipe} from './editing-recipe-response';
import {readLegacyEditorDraft} from './legacy-editor-dto';
import {legacyJSONRevision} from './legacy-json-revision';
import {readMember,readMemberPage} from './member-contract';
import {readTenantSettings,readTenantSettingsUpdate} from './tenant-settings-response';
import {readUserProfile,freezeUserPreferences,readUserProfileUpdate} from './profile-response';
import {readMediaLinks} from './media-link-response';
import {readReviewView,readReviewDetail,readReviewPage,readReviewDecision} from './review-response';
import {readCollectionItemResponse} from './collection-item-response';
import {readStyleVersionPage} from './style-version-response';
import {freezeCaptureShots,readCaptureShots,readCaptureShotsReplacement,readCapturePeople} from './capture-shot-response';
import {readRunStatusResponse} from './run-status-response';
import {readRunOutputs} from './run-output-response';
import {readProjectPage,readStylePage,readStyleProfile} from './catalog-read-response';
import {readShootSession,readShootSessionPage,readShootSessionUpdate} from './capture-session-response';
import {readDeadline} from './read-deadline';
import {readNegativeAppliedVersion} from './negative-applied-version';
import {readReflectionAppliedVersion} from './reflection-applied-version';
import {readLocalPortraitAppliedVersion} from './local-portrait-applied-version';
import {readLocalPortraitResult,readLocalPortraitAutoAppliedVersion,type LocalPortraitResult} from './local-portrait-result';
import {readLocalPortraitResultSelection} from './negative-result-selection';
import {readOpenEyesAppliedVersion} from './open-eyes-applied-version';
import {readGlareAppliedVersion} from './glare-applied-version';
import {readDownloadJob,readDownloadAccepted} from "./download-job";
import {editorEffectsRequest,readEditorEffectsAccepted,readEditorEffectsStatus} from './editor-effects-contract';
import type {RenderFreezeResult} from './render-freeze';
import {readRelightCollectionTargets} from './relight-collection-targets';
import {readRelightAppliedVersion} from './relight-applied-version';
import {readRelightViewedReceipt} from './relight-viewed-receipt';
import {readCapabilities} from "./capabilities-response";
import {collectionApplyRequest,readCollectionApplyAccepted} from './collection-apply-contract';
import {readRAWCapabilities} from '@photoeditor/api-client';
import {freezeLearningCorrection,readLearningCorrectionReceipt,type LearningCorrectionCreate} from './learning-correction';
import {readCaptureAlertPage} from './capture-alert-response';
import {captureActionKey,captureConfirmationScope,freezeCaptureAcknowledgement,readCaptureAcknowledgement,readCaptureConfirmation} from './capture-action-response';
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
import {readPreparedReviewDecision,preparedReviewCursor,readPreparedReviewPage,readPreparedReviewReceipt,readSavedPreparedReviewPage,savedPreparedReviewCursor,type PreparedReviewDecision,type PreparedReviewItem,type SavedPreparedReview} from './interchange-prepared-review';
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
import {readMultiRunReview} from './multi-run-review';
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
import {freezeGlareSourceIntent,freezeGlareSourcePreparation,readPreparedGlareSourceIntent,readGlareSourceTicket,type GlareSourceIntent,type GlareSourcePreparation} from './glare-source-intent';
import {freezeOpenEyesIntent,freezeOpenEyesSelection,readPreparedOpenEyesIntent,readOpenEyesTicket,type OpenEyesIntent,type OpenEyesSelection} from './open-eyes-intent';
import {freezeReflectionIntent,freezeReflectionPreparation,readPreparedReflectionIntent,readReflectionTicket,type ReflectionIntent,type ReflectionPreparation} from './reflection-intent';
import {freezeReflectionMaskPairQuery,readReflectionMaskPairPage,type ReflectionMaskPairQuery} from './reflection-mask-pairs';
import {freezeLocalPortraitSelection,readLocalPortraitPrepared,readLocalPortraitTicket,type LocalPortraitSelection} from './local-portrait-intent';
import {freezeGlareDonorCandidateQuery,freezeGlareSourceCandidateQuery,freezeGlareMaskPairQuery,readGlareDonorCandidatePage,readGlareSourceCandidatePage,readGlareMaskPairPage,type GlareDonorCandidateQuery,type GlareSourceCandidateQuery,type GlareMaskPairQuery} from './glare-donor-candidates';
import {freezeSceneApplyDecision,type SceneApplyDecision} from './scene-apply';
import {readRenderPolicy} from './render-policy';
import {renderInputSnapshotRequest,renderInputSnapshotDigest,renderInputSnapshotResult,type RenderInputSnapshotRequest} from './render-input-snapshot';
import {isRenderSourceBinding,renderBatchSubmission,renderBatchTicket,type RenderBatchSubmission} from './render-batch';
import {renderFreezeRequest,renderFreezeResult,type RenderFreezeRequest} from './render-freeze';
import {readRegionDescriptor} from './region-preview-descriptor';
import {readLayerDocument,readLayerStack,type EditorLayerStack} from './editor-layers';
import {readSessionAnalyses} from './session-analysis-list';
import type {SceneAnalysisRequest} from './scene-analysis-request';
import {readSceneBoardDocument} from './scene-board';
import {v2ResourceData} from './v2-resource';
import {freezeRegionRequest,regionJobTicket} from './region-preview';
import type {RegionPreviewRequest} from './region-preview';
import type {ShootSession,ShootSessionCreate,ShootSessionUpdate,CaptureAlert,CaptureShotRequirement} from "./capture-types";
import { ApiClient, signExport, signingPolicies, type SignCreate, type SemanticEditIntent } from "@photoeditor/api-client";
import type {ArtifactRef,ApplyAccepted,Collection,CollectionItem,DownloadJob,EditorDraft,ExportPreset,MaskRef,MediaLink,Member,OverrideRecord,Page,Review,RunAccepted,RunOutput,StyleProfile,StyleVersion,TenantSettings,TrainingPair,TrainingSet,UserProfile,V2DocumentResource,V2DraftResource,V2EditingRecipe,V2VersionBinding} from "./catalog-types";
import {freezeDeliveryCreate,readDeliveryCreateTicket,type DeliveryCreateRequest} from './delivery-create';
function requireArchiveUUID(value:unknown):string{if(!archiveUUID(value))throw new Error('SCOPE_INVALID');return value;}

export class OwnerAPI {
  constructor(private client: ApiClient, readonly tenant: string) {}
  async recordCorrection(request:LearningCorrectionCreate,key:string,signal?:AbortSignal){
    const body=freezeLearningCorrection(request);
    if(!key||!key.trim())throw new Error('IDEMPOTENCY_KEY_REQUIRED');
    signal?.throwIfAborted();
    const result=await this.client.request<unknown>(
      this.path('/api/v1/tenants/{t}/corrections'),
      {method:'POST',body:structuredClone(body),idempotencyKey:key,signal});
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
  private path(value:string){if(value.includes("{t}"))requireArchiveUUID(this.tenant);return value.replace("{t}",encodeURIComponent(this.tenant));}
  async rawCapabilities(project:string,signal?:AbortSignal){
    requireArchiveUUID(this.tenant);requireArchiveUUID(project);signal?.throwIfAborted();
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
    const items:PreparedReviewItem[]=[],seen=new Set<string>();let after='';
    for(let pageNumber=0;pageNumber<250;pageNumber++){
      const page=await this.preparedImportReviewPage(project,workflow,after,signal);
      for(const row of page.items){const cursor=preparedReviewCursor(row);if(seen.has(cursor)||after&&cursor<=after)throw new Error('INTERCHANGE_PREPARED_REVIEW_INVALID');seen.add(cursor);items.push(row);}
      if(!page.next_after)return items;
      if(page.next_after===after)throw new Error('INTERCHANGE_PREPARED_REVIEW_INVALID');after=page.next_after;
    }
    throw new Error('INTERCHANGE_PREPARED_REVIEW_INVALID');
  }
  async preparedImportReviewPage(project:string,workflow:string,after='',signal?:AbortSignal){
    assertInterchangeScope(this.tenant,project,workflow);signal?.throwIfAborted();
    const path=`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-jobs/${encodeURIComponent(workflow)}/prepared-reviews${after?`?after=${encodeURIComponent(after)}`:''}`;
    const value=await this.get<unknown>(path,signal);signal?.throwIfAborted();
    const page=readPreparedReviewPage(value,workflow);
    if(after&&page.items.some(row=>preparedReviewCursor(row)<=after))throw new Error('INTERCHANGE_PREPARED_REVIEW_INVALID');
    return page;
  }
  async savedPreparedImportReviews(project:string,workflow:string,signal?:AbortSignal){
    assertInterchangeScope(this.tenant,project,workflow);signal?.throwIfAborted();
    const items:SavedPreparedReview[]=[],seen=new Set<string>();let after='';
    for(let pageNumber=0;pageNumber<50;pageNumber++){
      const page=await this.savedPreparedImportReviewPage(project,workflow,after,signal);
      for(const row of page.items){const cursor=savedPreparedReviewCursor(row);if(seen.has(cursor)||after&&cursor<=after)throw new Error('INTERCHANGE_PREPARED_REVIEW_INVALID');seen.add(cursor);items.push(row);}
      if(!page.next_after)return items;
      if(page.next_after===after)throw new Error('INTERCHANGE_PREPARED_REVIEW_INVALID');after=page.next_after;
    }
    throw new Error('INTERCHANGE_PREPARED_REVIEW_INVALID');
  }
  async savedPreparedImportReviewPage(project:string,workflow:string,after='',signal?:AbortSignal){
    assertInterchangeScope(this.tenant,project,workflow);signal?.throwIfAborted();
    const path=`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-jobs/${encodeURIComponent(workflow)}/prepared-reviews/saved${after?`?after=${encodeURIComponent(after)}`:''}`;
    const value=await this.get<unknown>(path,signal);signal?.throwIfAborted();
    const page=readSavedPreparedReviewPage(value,workflow);
    if(after&&page.items.some(row=>savedPreparedReviewCursor(row)<=after))throw new Error('INTERCHANGE_PREPARED_REVIEW_INVALID');
    return page;
  }
  async decidePreparedImportReview(project:string,workflow:string,body:PreparedReviewDecision,key:string,signal?:AbortSignal){
    assertInterchangeScope(this.tenant,project,workflow);if(!archiveUUID(key))throw new Error('INTERCHANGE_PREPARED_REVIEW_INVALID');signal?.throwIfAborted();
    const request=readPreparedReviewDecision(body);
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-jobs/${encodeURIComponent(workflow)}/prepared-reviews`),{method:'POST',body:request,idempotencyKey:key,signal});
    signal?.throwIfAborted();return readPreparedReviewReceipt(value,workflow,request);
  }
  async importPersonMaskStatuses(project:string,workflow:string,signal?:AbortSignal){
    assertInterchangeScope(this.tenant,project,workflow);signal?.throwIfAborted();
    const value=await this.get<unknown>(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-jobs/${encodeURIComponent(workflow)}/person-mask-confirmations`,signal);
    signal?.throwIfAborted();return readImportPersonMaskStatuses(value,workflow);
  }
  async importPersonMaskCandidates(project:string,workflow:string,entry:string,signal?:AbortSignal){
    assertInterchangeScope(this.tenant,project,workflow);if(!archiveUUID(entry))throw new Error('INTERCHANGE_MASK_CANDIDATES_INVALID');signal?.throwIfAborted();
    const value=await this.get<unknown>(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-jobs/${encodeURIComponent(workflow)}/person-mask-candidates/${encodeURIComponent(entry)}`,signal);
    signal?.throwIfAborted();return readImportPersonMaskCandidates(value,workflow,entry);
  }
  async importPersonMaskEntries(project:string,workflow:string,signal?:AbortSignal){
    assertInterchangeScope(this.tenant,project,workflow);signal?.throwIfAborted();
    const value=await this.get<unknown>(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-jobs/${encodeURIComponent(workflow)}/person-mask-candidates`,signal);
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
    const frozen=structuredClone(request),pin=readArchiveUploadReceipt(receipt,project,frozen);
    if(!archiveUUID(this.tenant))throw new Error('INTERCHANGE_ARCHIVE_UPLOAD_INVALID');signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/interchange-archive-uploads/${pin.upload_id}?request_sha256=${pin.request_sha256}`),{signal});
    signal?.throwIfAborted();return readArchiveStatus(value,pin,frozen);
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
    signal?.throwIfAborted();
    const owned=freezeCollectionVersion(request);
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/collections/${encodeURIComponent(owned.collection)}/items/${encodeURIComponent(owned.item)}/version`),{method:'GET',signal});
    if(signal?.aborted)throw new Error('COLLECTION_VERSION_ABORTED');
    return reconcileCollectionVersion(value,owned);
  }
  async replaceCollectionVersion(request:CollectionVersionSelection,signal?:AbortSignal){
    signal?.throwIfAborted();
    const owned=freezeCollectionVersion(request);
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/collections/${encodeURIComponent(owned.collection)}/items/${encodeURIComponent(owned.item)}/version`),{method:'PUT',body:{expected_revision:owned.revision,expected_asset_version_id:owned.expectedVersion,asset_version_id:owned.version},headers:{'If-Match':`"${owned.revision}"`},signal});
    if(signal?.aborted)throw new Error('COLLECTION_VERSION_ABORTED');
    return readCollectionVersionSelection(value,owned);
  }
  async applyPerspective(review:PerspectiveQuality,confirmGeometryReset:boolean,acceptQualityWarnings:boolean,signal?:AbortSignal){
    const owned=structuredClone(review);
    readPerspectiveProposal(owned.proposal,this.tenant,owned.proposal.project_id,owned.proposal.producer_task_id,owned.proposal.base);
    const body=preparePerspectiveApply(owned,confirmGeometryReset,acceptQualityWarnings),p=owned.proposal;
    signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(p.project_id)}/versions/${encodeURIComponent(p.base.asset_version_id)}/perspective-proposals/${encodeURIComponent(p.producer_task_id)}/apply`),{method:'POST',body,signal});
    if(signal?.aborted)throw new Error('PERSPECTIVE_APPLY_ABORTED');
    return readPerspectiveApplied(value,owned);
  }
  async perspectiveQuality(proposal:PerspectiveProposal,signal?:AbortSignal){
    const owned=readPerspectiveProposal(proposal,this.tenant,proposal.project_id,proposal.producer_task_id,proposal.base);
    signal?.throwIfAborted();
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
    signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(scope.project)}/versions/${encodeURIComponent(scope.base.asset_version_id)}/perspective-line-detections/${encodeURIComponent(scope.producer)}`),{method:'GET',signal});
    signal?.throwIfAborted();
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
    signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/reviews/${encodeURIComponent(review)}/multi-replacements/${encodeURIComponent(originalTask)}?decision_hash=${encodeURIComponent(decisionHash)}&operation=${encodeURIComponent(operation)}`),{method:'GET',signal});
    if(signal?.aborted)throw new Error('MULTI_RESULT_CANCELLED');
    return readMultiReplacementScope(value,this.tenant,project,review,originalTask,decisionHash,operation);
  }
  async perspectiveReplacementScope(project:string,review:string,originalTask:string,decisionHash:string,signal?:AbortSignal){
    const valid=(v:string)=>/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
    if(![this.tenant,project,review,originalTask].every(valid)||!/^[0-9a-f]{64}$/.test(decisionHash))throw new Error('PERSPECTIVE_REPLACEMENT_SCOPE_INVALID');
    signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/reviews/${encodeURIComponent(review)}/perspective-replacements/${encodeURIComponent(originalTask)}?decision_hash=${encodeURIComponent(decisionHash)}`),{method:'GET',signal});
    if(signal?.aborted)throw new Error('PERSPECTIVE_RESULT_CANCELLED');
    return readPerspectiveReplacementScope(value,this.tenant,project,review,originalTask,decisionHash);
  }
  async perspectiveProposal(project:string,producer:string,base:V2VersionBinding,signal?:AbortSignal){
    const scope=perspectiveProposalScope(this.tenant,project,producer,base);
    signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(scope.project)}/versions/${encodeURIComponent(scope.base.asset_version_id)}/perspective-proposals/${encodeURIComponent(scope.producer)}`),{method:'GET',signal});
    signal?.throwIfAborted();
    return readPerspectiveProposal(value,scope.tenant,scope.project,scope.producer,scope.base);
  }
  async publishPerspectiveLines(project:string,request:PerspectiveLinesIntent,key:string,signal?:AbortSignal){
    const body=freezePerspectiveLinesIntent(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('PERSPECTIVE_LINES_SUBMISSION_INVALID');
    signal?.throwIfAborted();
    const receipt=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/perspective-lines`),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();
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
    signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(scope.project)}/${route}/${encodeURIComponent(scope.producer)}`),{method:'GET',signal});
    if(signal?.aborted)throw new Error('MULTI_PROPOSAL_CANCELLED');
    return readMultiProposal(value,scope.tenant,scope.project,scope.producer,scope.operation,scope.intent);
  }
  async multiRunReview(run:string,signal?:AbortSignal){if(!multiUUID(run))throw new Error('MULTI_RUN_SCOPE_INVALID');return readMultiRunReview(await this.get<unknown>(`/api/v1/tenants/{t}/runs/${encodeURIComponent(run)}/multi-review`,signal),this.tenant,run);}
  async multiCapabilities(operation:MultiOperation,project:string,signal?:AbortSignal){
    if(!multiUUID(project)||(operation!=='optical.panorama'&&operation!=='optical.focus_stack'))throw new Error('MULTI_CAPABILITIES_INVALID');
    const route=operation==='optical.panorama'?'panorama-capabilities':'focus-stack-capabilities';
    return readMultiCapabilities(await this.get(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/${route}`,signal),this.tenant,project,operation);
  }
  async submitMulti(operation:MultiOperation,project:string,request:MultiIntent,key:string,signal?:AbortSignal){
    const body=freezeMultiIntent(operation,request);
    if(!multiUUID(project)||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('MULTI_SUBMISSION_INVALID');
    const route=operation==='optical.panorama'?'panorama-intents':'focus-stack-intents';
    signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/${route}`),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();
    return regionJobTicket(value);
  }
  async submitPerspective(project:string,request:PerspectiveIntent,key:string,signal?:AbortSignal){
    const body=freezePerspectiveIntent(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('PERSPECTIVE_SUBMISSION_INVALID');
    signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/perspective-intents`),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();
    return regionJobTicket(value);
  }
  async negativeCapabilities(project:string,signal?:AbortSignal){
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('NEGATIVE_PROJECT_INVALID');
    return readNegativeCapabilities(await this.get(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/negative-capabilities`,signal),this.tenant,project);
  }
  async submitNegative(project:string,request:NegativeIntent,key:string,signal?:AbortSignal){
    const body=freezeNegativeIntent(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('NEGATIVE_SUBMISSION_INVALID');
    signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/negative-intents`),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();
    return regionJobTicket(value);
  }
  async submitRelightAnalysis(project:string,request:RelightAnalysisIntent,key:string,signal?:AbortSignal,replayOnly=false){
    signal?.throwIfAborted();
    const body=freezeRelightAnalysisIntent(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('RELIGHT_PROJECT_INVALID');
    if(!/^[A-Za-z0-9._:-]{1,128}$/.test(key))throw new Error('RELIGHT_IDEMPOTENCY_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/relight-analyses`),{method:'POST',body,idempotencyKey:key,signal,...(replayOnly?{headers:{'Relight-Replay-Only':'true'}}:{})});
    signal?.throwIfAborted();
    return regionJobTicket(value);
  }
  async prepareRelight(project:string,request:RelightPreparationRequest,signal?:AbortSignal){
    signal?.throwIfAborted();
    const body=freezeRelightPreparation(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('RELIGHT_PROJECT_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/relight-preparation`),{method:'POST',body,signal});
    signal?.throwIfAborted();
    return readPreparedRelight(value,body);
  }
  async relightCandidates(project:string,request:RelightCandidateQuery,signal?:AbortSignal){
    signal?.throwIfAborted();
    const body=freezeRelightCandidateQuery(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('RELIGHT_PROJECT_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/relight-candidates`),{method:'POST',body,signal});
    signal?.throwIfAborted();
    return readRelightCandidatePage(value,body);
  }
  async submitRelight(project:string,request:RelightIntent,key:string,signal?:AbortSignal,replayOnly=false){
    signal?.throwIfAborted();
    const body=freezeRelightIntent(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('RELIGHT_SUBMISSION_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/relight-intents`),{method:'POST',body,idempotencyKey:key,signal,...(replayOnly?{headers:{'Relight-Replay-Only':'true'}}:{})});
    signal?.throwIfAborted();
    return regionJobTicket(value);
  }
  async submitGlareDonor(project:string,request:GlareDonorIntent,key:string,signal?:AbortSignal){
    signal?.throwIfAborted();
    const body=freezeGlareDonorIntent(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('GLARE_DONOR_SUBMISSION_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/glare-donor-intents`),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();
    return readGlareDonorTicket(value);
  }
  async prepareGlareSource(project:string,request:GlareSourcePreparation,key:string,signal?:AbortSignal){
    signal?.throwIfAborted();
    const body=freezeGlareSourcePreparation(request);
    requireArchiveUUID(project);
    if(!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('GLARE_SOURCE_PREPARATION_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/glare-generative-prepare`),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();
    return readPreparedGlareSourceIntent(value,body);
  }
  async glareSourceAvailable(project:string,signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(project);const result=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/glare-generative-capability`),{signal});signal?.throwIfAborted();if(!result||typeof result!=='object'||Object.keys(result).join(',')!=='available'||typeof (result as {available:unknown}).available!=='boolean')throw new Error('GLARE_SOURCE_CAPABILITY_INVALID');return (result as {available:boolean}).available;}
  async submitGlareSource(project:string,request:GlareSourceIntent,key:string,signal?:AbortSignal){
    signal?.throwIfAborted();
    const body=freezeGlareSourceIntent(request);
    requireArchiveUUID(project);
    if(!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('GLARE_SOURCE_SUBMISSION_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/glare-generative-submit`),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();
    return readGlareSourceTicket(value);
  }
  async submitOpenEyes(project:string,request:OpenEyesIntent,key:string,signal?:AbortSignal){
    signal?.throwIfAborted();
    const body=freezeOpenEyesIntent(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('OPEN_EYES_SUBMISSION_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/open-eyes-intents`),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();
    return readOpenEyesTicket(value);
  }
  async prepareOpenEyes(project:string,request:OpenEyesSelection,key:string,signal?:AbortSignal){
    signal?.throwIfAborted();
    const body=freezeOpenEyesSelection(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('OPEN_EYES_PREPARATION_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/open-eyes-preparations`),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();
    return readPreparedOpenEyesIntent(value,body);
  }
  async openEyesSourceCandidates(project:string,query:GlareSourceCandidateQuery,signal?:AbortSignal){
    signal?.throwIfAborted();
    const body=freezeGlareSourceCandidateQuery(query);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('OPEN_EYES_SOURCE_QUERY_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/open-eyes-source-candidates`),{method:'POST',body,signal});
    signal?.throwIfAborted();
    return readGlareSourceCandidatePage(value,body);
  }
  async openEyesDonorCandidates(project:string,query:GlareDonorCandidateQuery,signal?:AbortSignal){
    signal?.throwIfAborted();
    const body=freezeGlareDonorCandidateQuery(query);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('OPEN_EYES_DONOR_QUERY_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/open-eyes-donor-candidates`),{method:'POST',body,signal});
    signal?.throwIfAborted();
    return readGlareDonorCandidatePage(value,body);
  }
  async openEyesMaskPairs(project:string,query:GlareMaskPairQuery,signal?:AbortSignal){
    signal?.throwIfAborted();
    const body=freezeGlareMaskPairQuery(query);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('OPEN_EYES_MASK_QUERY_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/open-eyes-mask-pairs`),{method:'POST',body,signal});
    signal?.throwIfAborted();
    return readGlareMaskPairPage(value,body);
  }
  async submitReflection(project:string,request:ReflectionIntent,key:string,signal?:AbortSignal){
    signal?.throwIfAborted();
    const body=freezeReflectionIntent(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('REFLECTION_SUBMISSION_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/reflection-intents`),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();
    return readReflectionTicket(value);
  }
  async prepareReflection(project:string,request:ReflectionPreparation,key:string,signal?:AbortSignal){
    signal?.throwIfAborted();
    const body=freezeReflectionPreparation(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('REFLECTION_PREPARATION_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/reflection-preparations`),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();
    return readPreparedReflectionIntent(value,body);
  }
  async prepareGlareDonor(project:string,request:GlareDonorPreparation,key:string,signal?:AbortSignal){
    signal?.throwIfAborted();
    const body=freezeGlareDonorPreparation(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('GLARE_DONOR_PREPARATION_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/glare-donor-preparations`),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();
    return readPreparedGlareDonorIntent(value,body);
  }
  async glareDonorCandidates(project:string,query:GlareDonorCandidateQuery,signal?:AbortSignal){
    signal?.throwIfAborted();
    const body=freezeGlareDonorCandidateQuery(query);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('GLARE_DONOR_CANDIDATE_QUERY_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/glare-donor-candidates`),{method:'POST',body,signal});
    signal?.throwIfAborted();
    return readGlareDonorCandidatePage(value,body);
  }
  async glareSourceCandidates(project:string,query:GlareSourceCandidateQuery,signal?:AbortSignal){
    signal?.throwIfAborted();
    const body=freezeGlareSourceCandidateQuery(query);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('GLARE_SOURCE_CANDIDATE_QUERY_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/glare-source-candidates`),{method:'POST',body,signal});
    signal?.throwIfAborted();
    return readGlareSourceCandidatePage(value,body);
  }
  async glareMaskPairs(project:string,query:GlareMaskPairQuery,signal?:AbortSignal){
    signal?.throwIfAborted();
    const body=freezeGlareMaskPairQuery(query);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('GLARE_MASK_PAIR_QUERY_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/glare-mask-pairs`),{method:'POST',body,signal});
    signal?.throwIfAborted();
    return readGlareMaskPairPage(value,body);
  }
  async reflectionMaskPairs(project:string,query:ReflectionMaskPairQuery,signal?:AbortSignal){
    signal?.throwIfAborted();
    const body=freezeReflectionMaskPairQuery(query);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('REFLECTION_MASK_PAIR_QUERY_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/reflection-mask-pairs`),{method:'POST',body,signal});
    signal?.throwIfAborted();
    return readReflectionMaskPairPage(value,body);
  }
  async prepareLocalPortrait(project:string,request:LocalPortraitSelection,key:string,signal?:AbortSignal){
    signal?.throwIfAborted();
    const body=freezeLocalPortraitSelection(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('LOCAL_PORTRAIT_PREPARATION_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/local-portrait-preparations`),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();
    return readLocalPortraitPrepared(value,body);
  }
  async localPortraitMaskPairs(project:string,query:ReflectionMaskPairQuery,signal?:AbortSignal){
    signal?.throwIfAborted();
    const body=freezeReflectionMaskPairQuery(query);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(project)||project==='00000000-0000-0000-0000-000000000000')throw new Error('LOCAL_PORTRAIT_MASK_PAIR_QUERY_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/local-portrait-mask-pairs`),{method:'POST',body,signal});
    signal?.throwIfAborted();
    return readReflectionMaskPairPage(value,body);
  }
  async submitLocalPortrait(project:string,request:LocalPortraitSelection,key:string,signal?:AbortSignal){
    signal?.throwIfAborted();
    const body=freezeLocalPortraitSelection(request);
    requireArchiveUUID(project);
    if(!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('LOCAL_PORTRAIT_SUBMISSION_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/local-portrait-intents`),{method:'POST',body,idempotencyKey:key,signal});
    signal?.throwIfAborted();
    return readLocalPortraitTicket(value);
  }
  async renderPolicy(project:string,signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(project);const value=await this.get(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/render-policy`,signal);signal?.throwIfAborted();return readRenderPolicy(value);}
  async createRenderInputSnapshot(project:string,request:RenderInputSnapshotRequest,key:string,signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(project);const body=renderInputSnapshotRequest(request),digest=renderInputSnapshotDigest(body),count=body.sources.length;if(!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('IDEMPOTENCY_KEY_REQUIRED');const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/render-input-snapshots`),{method:'POST',body,idempotencyKey:key,signal});signal?.throwIfAborted();return renderInputSnapshotResult(value,digest,count);}
  async editorEffectsStatus(project:string,build:string,signal?:AbortSignal){
    signal?.throwIfAborted();requireArchiveUUID(project);requireArchiveUUID(build);const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/editor-effects/${encodeURIComponent(build)}`),{signal});signal?.throwIfAborted();return readEditorEffectsStatus(value,build);
  }
  async queueEditorEffects(project:string,frozen:RenderFreezeResult,key:string,signal?:AbortSignal){
    signal?.throwIfAborted();requireArchiveUUID(project);const body=editorEffectsRequest(frozen);if(!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('IDEMPOTENCY_KEY_REQUIRED');signal?.throwIfAborted();
    const result=readEditorEffectsAccepted(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/editor-effects`),{method:'POST',body,idempotencyKey:key,signal}));signal?.throwIfAborted();return result;
  }
  async queueRenderBatch(project:string,request:RenderBatchSubmission,key:string,signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(project);const body=renderBatchSubmission(request);if(!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('IDEMPOTENCY_KEY_REQUIRED');const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/render-batches`),{method:'POST',body,idempotencyKey:key,signal});signal?.throwIfAborted();return renderBatchTicket(value);}
  async editorCapabilities(asset:string,request:RenderFreezeRequest,binding:V2VersionBinding,signal?:AbortSignal){
    const id=requireArchiveUUID(asset),body=renderFreezeRequest(request),source=structuredClone(binding);signal?.throwIfAborted();
    const query=new URLSearchParams({document_id:body.document_id,expected_revision:body.expected_revision});if(body.assume_profile!==null)query.set('assume_profile',body.assume_profile);
    const value=await this.get<unknown>(`/api/v1/tenants/{t}/assets/${encodeURIComponent(id)}/editor-capabilities?${query}`,signal);signal?.throwIfAborted();
    return readEditorCapabilities(value,{tenant:this.tenant,asset:id,document:body.document_id,revision:body.expected_revision,source});
  }
  async freezeRenderDraft(asset:string,request:RenderFreezeRequest,key:string,signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(asset);const body=renderFreezeRequest(request);if(!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('IDEMPOTENCY_KEY_REQUIRED');const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/assets/${encodeURIComponent(asset)}/v2-draft/freeze`),{method:'POST',body,idempotencyKey:key,signal});signal?.throwIfAborted();return renderFreezeResult(value,body);}
  async regionDescriptor(asset:string,run:string,request:RegionPreviewRequest,source:ArtifactRef,signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(asset);requireArchiveUUID(run);const expected=freezeRegionRequest(request),pinned=structuredClone(source);const raw=await this.get<unknown>(`/api/v1/tenants/{t}/assets/${encodeURIComponent(asset)}/region-previews/${encodeURIComponent(run)}/descriptor`,signal);signal?.throwIfAborted();return readRegionDescriptor(raw,asset,expected,pinned);}
  async regionPreview(asset:string,request:RegionPreviewRequest,key:string,signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(asset);const body=freezeRegionRequest(request);if(!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('IDEMPOTENCY_KEY_REQUIRED');const raw=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/assets/${encodeURIComponent(asset)}/region-previews`),{method:'POST',body,idempotencyKey:key,signal});signal?.throwIfAborted();return regionJobTicket(raw);}
  async sessionAnalysisCapabilities(id:string,request:SceneAnalysisRequest,signal?:AbortSignal){
    const session=requireArchiveUUID(id),body=structuredClone(request),scope={tenant:this.tenant,session,requestSHA:sessionAnalysisRequestSHA(body)};signal?.throwIfAborted();
    const value=await readDeadline(signal,child=>this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(session)}/analysis-capabilities`),{method:'POST',body,signal:child}));signal?.throwIfAborted();return readSessionAnalysisCapabilities(value,scope);
  }
  async analyzeSession(id:string,request:SceneAnalysisRequest,key:string,signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(id);if(!key||key.length>128||!/^[!-~]+$/.test(key))throw Error('SCENE_ANALYSIS_INVALID');const body=structuredClone(request),value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(id)}/analyses`),{method:'POST',body,idempotencyKey:key,signal});signal?.throwIfAborted();return regionJobTicket(value);}
  async sessionAnalyses(sessionId:string,cursor?:string,signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(sessionId);if(cursor!==undefined&&(!cursor||cursor.length>2048))throw Error('SESSION_ANALYSES_INVALID');const tenant=this.tenant,value=await this.get<unknown>(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(sessionId)}/analyses?limit=100${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`,signal);signal?.throwIfAborted();return readSessionAnalyses(value,sessionId,cursor,tenant);}
  async applySceneAnalysis(analysisId:string,request:SceneApplyDecision,key:string,signal?:AbortSignal){
    signal?.throwIfAborted();
    const body=freezeSceneApplyDecision(request);
    if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(analysisId)||analysisId==='00000000-0000-0000-0000-000000000000'||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error('SCENE_APPLY_INVALID');
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/session-analyses/${encodeURIComponent(analysisId)}/apply`),{method:'POST',body,idempotencyKey:key,signal});signal?.throwIfAborted();return regionJobTicket(value);
  }
  async sceneBoard(analysisId:string,sessionId:string,signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(analysisId);requireArchiveUUID(sessionId);const value=await this.get<unknown>(`/api/v1/tenants/{t}/session-analyses/${encodeURIComponent(analysisId)}/scene-board`,signal);signal?.throwIfAborted();return readSceneBoardDocument(value,analysisId,sessionId);}
  get<T>(path:string,signal?:AbortSignal){return readDeadline(signal,child=>this.client.request<T>(this.path(path),{signal:child}));}
  mutate<T>(path:string,body:unknown,key:string=crypto.randomUUID(),method="POST",headers?:HeadersInit,signal?:AbortSignal){return this.client.request<T>(this.path(path),{method,body,idempotencyKey:key,headers,signal});}
  async captureCapabilities(source:ShootSession,signal?:AbortSignal){
    const scope={tenant:this.tenant,project:requireArchiveUUID(source.project_id),template:requireArchiveUUID(source.id),revision:source.revision,policySHA:capturePolicySHA(source)};signal?.throwIfAborted();
    const value=await this.get<unknown>(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(scope.template)}/capture-capabilities?source_revision=${encodeURIComponent(scope.revision)}`,signal);signal?.throwIfAborted();return readCaptureCapabilities(value,scope);
  }
  async createShootSession(request:ShootSessionCreate,key:string=crypto.randomUUID(),signal?:AbortSignal){const body=structuredClone(request),tenant=this.tenant,scope={project:body.project_id,collection:body.collection_id};signal?.throwIfAborted();const value=await this.mutate('/api/v1/tenants/{t}/shoot-sessions',body,key,'POST',undefined,signal);signal?.throwIfAborted();return readShootSession(value,tenant,scope);}
  async updateShootSession(id:string,request:ShootSessionUpdate,key:string=crypto.randomUUID(),signal?:AbortSignal){signal?.throwIfAborted();const tenant=this.tenant,frozen=structuredClone(request),value=await this.mutate(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(id)}`,structuredClone(frozen),key,'PATCH',undefined,signal);signal?.throwIfAborted();return readShootSessionUpdate(value,tenant,id,frozen);}
  async shootSession(id:string,signal?:AbortSignal){signal?.throwIfAborted();const tenant=this.tenant,value=await this.get<unknown>(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(id)}`,signal);signal?.throwIfAborted();return readShootSession(value,tenant,{id});}
  async shootSessions(project:string,cursor?:string,signal?:AbortSignal){signal?.throwIfAborted();const tenant=this.tenant,page=await this.get<unknown>(`/api/v1/tenants/{t}/shoot-sessions?project_id=${encodeURIComponent(project)}&limit=100${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`,signal);signal?.throwIfAborted();return readShootSessionPage(page,tenant,project,cursor);}
  async capturePeople(project:string,cursor?:string,signal?:AbortSignal){requireArchiveUUID(project);if(cursor!==undefined&&(!cursor||cursor.length>2048))throw Error('CAPTURE_PEOPLE_INVALID');signal?.throwIfAborted();const page=await this.get<unknown>(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/persons?limit=100${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`,signal);signal?.throwIfAborted();return readCapturePeople(page,this.tenant,project,cursor);}
  async captureShotList(sessionId:string,signal?:AbortSignal){requireArchiveUUID(sessionId);signal?.throwIfAborted();const raw=await this.get<unknown>(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(sessionId)}/shot-list`,signal);signal?.throwIfAborted();return readCaptureShots(raw,sessionId);}
  async replaceCaptureShotList(sessionId:string,revision:string,requirements:CaptureShotRequirement[],key:string=crypto.randomUUID(),signal?:AbortSignal){requireArchiveUUID(sessionId);if(!/^[1-9][0-9]{0,18}$/.test(revision)||BigInt(revision)>=9223372036854775807n)throw Error('CAPTURE_SHOT_REVISION_INVALID');const frozen=freezeCaptureShots(requirements);signal?.throwIfAborted();const raw=await this.mutate<unknown>(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(sessionId)}/shot-list`,{expected_revision:revision,requirements:structuredClone(frozen)},key,"PUT",undefined,signal);signal?.throwIfAborted();return readCaptureShotsReplacement(raw,sessionId,revision,frozen);}
  async captureAlerts(sessionId:string,cursor?:string,signal?:AbortSignal){const page=await this.get<unknown>(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(sessionId)}/alerts${cursor?`?cursor=${encodeURIComponent(cursor)}`:""}`,signal);signal?.throwIfAborted();return readCaptureAlertPage(page,sessionId);}
  async acknowledgeCaptureAlert(alert:CaptureAlert,key:string=crypto.randomUUID(),signal?:AbortSignal){signal?.throwIfAborted();captureActionKey(key);const frozen=freezeCaptureAcknowledgement(alert);const value=await this.mutate<unknown>(`/api/v1/tenants/{t}/capture-alerts/${encodeURIComponent(frozen.id)}/ack`,{expected_revision:frozen.revision},key,"POST",undefined,signal);signal?.throwIfAborted();return readCaptureAcknowledgement(value,this.tenant,frozen);}
  async confirmCaptureFrame(receiptId:string,alertId:string,key:string=crypto.randomUUID(),signal?:AbortSignal){signal?.throwIfAborted();captureActionKey(key);captureConfirmationScope(receiptId,alertId);const value=await this.mutate<unknown>(`/api/v1/tenants/{t}/capture-receipts/${encodeURIComponent(receiptId)}/confirm`,{alert_id:alertId},key,"POST",undefined,signal);signal?.throwIfAborted();return readCaptureConfirmation(value,receiptId,alertId);}
  async assetProject(id:string,signal?:AbortSignal):Promise<string>{
    requireArchiveUUID(id);const value=await this.get<{id?:unknown;tenant_id?:unknown;project_id?:unknown}>(`/api/v1/tenants/{t}/assets/${encodeURIComponent(id)}`,signal);signal?.throwIfAborted();
    if(value.id!==id||value.tenant_id!==this.tenant||typeof value.project_id!=='string')throw new Error('ASSET_PROJECT_INVALID');
    requireArchiveUUID(value.project_id);return value.project_id;
  }
  async projectAccess(id:string,signal?:AbortSignal):Promise<{id:string;can_edit:boolean}>{
    requireArchiveUUID(id);signal?.throwIfAborted();
    const value=await this.get<{id?:unknown;tenant_id?:unknown;can_edit?:unknown}>(`/api/v1/tenants/{t}/projects/${encodeURIComponent(id)}`,signal);
    signal?.throwIfAborted();
    if(value.id!==id||value.tenant_id!==this.tenant||typeof value.can_edit!=='boolean')throw new Error("PROJECT_ACCESS_INVALID");
    return {id,can_edit:value.can_edit};
  }
  async projects(cursor?:string,signal?:AbortSignal){signal?.throwIfAborted();const tenant=this.tenant,raw=await this.get<unknown>(`/api/v1/tenants/{t}/projects${cursor?`?cursor=${encodeURIComponent(cursor)}`:""}`,signal);signal?.throwIfAborted();return readProjectPage(raw,tenant,cursor);}
  collection(id:string,signal?:AbortSignal){return this.get<Collection>(`/api/v1/tenants/{t}/collections/${encodeURIComponent(id)}`,signal);}
  items(id:string,cursor?:string){return this.get<Page<CollectionItem>>(`/api/v1/tenants/{t}/collections/${encodeURIComponent(id)}/items${cursor?`?cursor=${encodeURIComponent(cursor)}`:""}`);}
  setItemColorLabel(collection:Collection,item:CollectionItem,label:CollectionItem["color_label"]|null,signal?:AbortSignal){
    return this.client.request<CollectionItem>(this.path(`/api/v1/tenants/{t}/collections/${encodeURIComponent(collection.id)}/items/${encodeURIComponent(item.id)}`),{
      method:"PATCH",body:{color_label:label},headers:{"If-Match":`"${collection.revision}"`},signal,
    });
  }
  async moderateApplyItem(collection:Collection,item:CollectionItem,decision:"accept"|"reject",signal?:AbortSignal){
    signal?.throwIfAborted();
    if(collection.tenant_id!==this.tenant||!['accept','reject'].includes(decision))throw Error('COLLECTION_MODERATION_INVALID');
    const owned=readCollectionItemResponse(item,{collection:collection.id});
    const revision=legacyJSONRevision(collection.revision),expected={collection:owned.collection_id,item:owned.id,asset:owned.asset_id,version:owned.asset_version_id,moderation:decision==='accept'?'accepted':'rejected'};
    const raw=await this.mutate<unknown>(`/api/v1/tenants/{t}/collections/${encodeURIComponent(expected.collection)}/items/${encodeURIComponent(expected.item)}/moderation`,{expected_revision:revision,decision},undefined,"POST",undefined,signal);
    signal?.throwIfAborted();return readCollectionItemResponse(raw,expected);
  }
  async run(id:string,signal?:AbortSignal){requireArchiveUUID(id);signal?.throwIfAborted();const value=await this.get<unknown>(`/api/v1/tenants/{t}/runs/${encodeURIComponent(id)}`,signal);signal?.throwIfAborted();return readRunStatusResponse(value,id);}
  semanticRunIntent(run:string,signal?:AbortSignal){return this.client.semanticRunIntent(this.tenant,run,signal);}
  async cancelRun(id:string,signal?:AbortSignal,key:string=crypto.randomUUID()){requireArchiveUUID(id);captureActionKey(key);signal?.throwIfAborted();const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/runs/${encodeURIComponent(id)}/cancel`),{method:'POST',idempotencyKey:key,signal});signal?.throwIfAborted();return readRunStatusResponse(value,id);}
  async runOutputs(id:string,signal?:AbortSignal){requireArchiveUUID(id);signal?.throwIfAborted();const value=await this.get<unknown>(`/api/v1/tenants/{t}/runs/${encodeURIComponent(id)}/outputs`,signal);signal?.throwIfAborted();return readRunOutputs(value);}
  async styles(projectId?:string,cursor?:string,signal?:AbortSignal){signal?.throwIfAborted();const tenant=this.tenant,q=new URLSearchParams();if(projectId)q.set("project_id",projectId);if(cursor)q.set("cursor",cursor);const raw=await this.get<unknown>(`/api/v1/tenants/{t}/styles?${q}`,signal);signal?.throwIfAborted();return readStylePage(raw,tenant,projectId,cursor);}
  async createStyle(projectId:string,name:string,description="",key?:string,signal?:AbortSignal){signal?.throwIfAborted();const tenant=this.tenant,request={project_id:projectId,name,description},value=await this.mutate<unknown>(`/api/v1/tenants/{t}/styles`,request,key,"POST",undefined,signal);signal?.throwIfAborted();const out=readStyleProfile(value,tenant,{project:projectId});if(out.name!==name.trim()||out.description!==description||out.revision!=='1'||out.archived)throw Error('STYLE_CREATE_RESPONSE_INVALID');return out;}
  async style(id:string,signal?:AbortSignal){signal?.throwIfAborted();const tenant=this.tenant,raw=await this.get<unknown>(`/api/v1/tenants/{t}/styles/${encodeURIComponent(id)}`,signal);signal?.throwIfAborted();return readStyleProfile(raw,tenant,{id});}
  async styleVersions(id:string,cursor?:string,signal?:AbortSignal){signal?.throwIfAborted();const value=await this.get<unknown>(`/api/v1/tenants/{t}/styles/${encodeURIComponent(id)}/versions${cursor?`?cursor=${encodeURIComponent(cursor)}`:""}`,signal);signal?.throwIfAborted();return readStyleVersionPage(value,id,cursor);}
  createTrainingSet(styleId:string){return this.mutate<TrainingSet>(`/api/v1/tenants/{t}/styles/${encodeURIComponent(styleId)}/training-sets`,{});}
  async analysisCapabilities(style:string,geometry:boolean,signal?:AbortSignal){
    const id=requireArchiveUUID(style),scope={tenant:this.tenant,style:id,geometry};signal?.throwIfAborted();
    const value=await this.get<unknown>(`/api/v1/tenants/{t}/styles/${encodeURIComponent(id)}/analysis-capabilities?geometry_enabled=${geometry}`,signal);
    signal?.throwIfAborted();return readAnalysisCapabilities(value,scope);
  }
  trainingSet(id:string,cursor?:string,signal?:AbortSignal){return this.get<TrainingSet>(`/api/v1/tenants/{t}/training-sets/${encodeURIComponent(id)}?limit=100${cursor?`&cursor=${encodeURIComponent(cursor)}`:""}`,signal);}
  addPair(id:string,pair:{before_asset_version_id:string;after_asset_version_id:string;session_id:string;confirmed:boolean;included:boolean}){return this.mutate<TrainingPair>(`/api/v1/tenants/{t}/training-sets/${encodeURIComponent(id)}/pairs`,pair);}
  analyze(styleId:string,set:TrainingSet){return this.mutate<{run_id:string;status:string}>(`/api/v1/tenants/{t}/styles/${encodeURIComponent(styleId)}/analyses`,{training_set_id:set.id,training_set_revision:legacyJSONRevision(set.revision),analysis_options:{alignment_mode:"crop_aware",geometry_enabled:true,seed:0}});}
  async apply(collection:Pick<Collection,"id"|"revision">,styleVersionId:string,strength:number,outputName:string,key:string=crypto.randomUUID(),signal?:AbortSignal){const body=collectionApplyRequest(collection,styleVersionId,strength,outputName,key);signal?.throwIfAborted();const result=readCollectionApplyAccepted(await this.mutate<unknown>(`/api/v1/tenants/{t}/collections/${encodeURIComponent(collection.id)}/apply-profile`,body,key,"POST",undefined,signal));signal?.throwIfAborted();return result;}
  async reviews(state="pending",cursor?:string,signal?:AbortSignal){signal?.throwIfAborted();if(!['','pending','decided'].includes(state)||cursor!==undefined&&(typeof cursor!=='string'||!cursor||cursor.length>2048))throw Error('REVIEW_QUERY_INVALID');const raw=await this.get<unknown>(`/api/v1/tenants/{t}/reviews?state=${encodeURIComponent(state)}${cursor?`&cursor=${encodeURIComponent(cursor)}`:""}`,signal);signal?.throwIfAborted();return readReviewPage(raw,{state:state||undefined});}
  async reviewsForRun(run:string,cursor?:string,signal?:AbortSignal){
    signal?.throwIfAborted();requireArchiveUUID(run);if(cursor!==undefined&&(typeof cursor!=='string'||!cursor||cursor.length>2048))throw Error('REVIEW_QUERY_INVALID');
    const query=new URLSearchParams({run_id:run});
    if(cursor)query.set('cursor',cursor);
    const raw=await this.get<unknown>(`/api/v1/tenants/{t}/reviews?${query}`,signal);signal?.throwIfAborted();return readReviewPage(raw,{run});
  }
  async reviewPreview(id:string,side:'before'|'after',signal:AbortSignal){signal.throwIfAborted();requireArchiveUUID(id);if(!['before','after'].includes(side))throw new Error('REVIEW_PREVIEW_INVALID');const value=await this.get<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(id)}/previews/${side}/preview1600`,signal);signal.throwIfAborted();return value;}
  async negativeResultSelection(review:Review,signal?:AbortSignal){signal?.throwIfAborted();const owned=ownRenderReviewBinding(review);const value=await this.get<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(owned.id)}/result-selection`,signal);signal?.throwIfAborted();return readNegativeResultSelection(value,owned);}
  async renderResultSelection(review:Review,signal?:AbortSignal){signal?.throwIfAborted();const owned=ownRenderReviewBinding(review);if(owned.kind!=='render.compose'||owned.state!=='decided')throw new Error('RENDER_RESULT_SELECTION_INVALID');const value=await this.get<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(owned.id)}/render-result-selection`,signal);signal?.throwIfAborted();return readRenderResultSelection(value,owned);}
  async renderSelectedPreview(review:Review,signal:AbortSignal){signal.throwIfAborted();const owned=ownRenderReviewBinding(review);if(owned.kind!=='render.compose'||owned.state!=='decided')throw new Error('RENDER_RESULT_SELECTION_INVALID');const value=await this.get<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(owned.id)}/selected-preview/preview1600`,signal);signal.throwIfAborted();return value;}
  async restorationResultSelection(review:Review,signal?:AbortSignal){signal?.throwIfAborted();const owned=ownRenderReviewBinding(review);if((owned.kind!=='portrait.glare'&&owned.kind!=='portrait.open_eyes'&&owned.kind!=='image.reflection'&&owned.kind!=='image.relight'&&owned.kind!=='portrait.hair'&&owned.kind!=='portrait.clothing')||owned.state!=='decided')throw new Error('RESTORATION_RESULT_SELECTION_INVALID');const value=await this.get<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(owned.id)}/restoration-result-selection`,signal);signal?.throwIfAborted();return owned.kind==='portrait.hair'||owned.kind==='portrait.clothing'?readLocalPortraitResultSelection(value,owned):readRestorationResultSelection(value,owned);}
  async restorationSelectedPreview(review:Review,signal:AbortSignal){signal.throwIfAborted();const owned=ownRenderReviewBinding(review);if((owned.kind!=='portrait.glare'&&owned.kind!=='portrait.open_eyes'&&owned.kind!=='image.reflection'&&owned.kind!=='image.relight'&&owned.kind!=='portrait.hair'&&owned.kind!=='portrait.clothing')||owned.state!=='decided')throw new Error('RESTORATION_RESULT_SELECTION_INVALID');const value=await this.get<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(owned.id)}/restoration-selected-preview/preview1600`,signal);signal.throwIfAborted();return value;}
  async cullingItems(review:Review,after:string|null,signal:AbortSignal){
   signal.throwIfAborted();const owned=readReviewView(review);
   if(owned.kind!=="culling"||after!==null&&(typeof after!=='string'||!after||after.length>2048))throw new Error("CULLING_REVIEW_INVALID");
   const query=after?"?after="+encodeURIComponent(after):"";
   const value=await this.get<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(owned.id)}/culling-items${query}`,signal);signal.throwIfAborted();return value;
  }
  async decideCulling(review:string,request:import('./culling-selection').CullingDecisionRequest,key:string,signal:AbortSignal){signal.throwIfAborted();requireArchiveUUID(review);const frozen=structuredClone(request),value=await this.mutate<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(review)}/culling-decision`,frozen,key,"POST",undefined,signal);signal.throwIfAborted();return value;}
  async review(id:string,signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(id);const raw=await this.get<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(id)}`,signal);signal?.throwIfAborted();return readReviewDetail(raw,id);}
  async relightCollectionTargets(review:Review,after:string|null,signal?:AbortSignal){
   const owned=ownRenderReviewBinding(review);
   if(owned.kind!=='image.relight'||owned.state!=='decided'||owned.decision!=='accept'||!owned.decision_hash)throw new Error('RELIGHT_APPLY_INVALID');
   if(after!==null)requireArchiveUUID(after);
   const query=new URLSearchParams({decision_hash:owned.decision_hash});if(after)query.set('after',after);
   return readRelightCollectionTargets(await this.get<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(owned.id)}/relight-collection-targets?${query}`,signal));
  }
  async applyRelightSelected(review:Review,signal?:AbortSignal){
   signal?.throwIfAborted();
   const owned=ownRenderReviewBinding(review);
   if(owned.kind!=='image.relight'||owned.state!=='decided'||owned.revision!==2)throw new Error('RELIGHT_APPLY_INVALID');
   const selected=await this.restorationResultSelection(owned,signal);
   if(!selected.image||selected.route==='skip')throw new Error('RELIGHT_APPLY_INVALID');
   signal?.throwIfAborted();
   const response=await this.mutate<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(owned.id)}/relight-apply`,{decision_hash:selected.decision_hash},undefined,'POST',undefined,signal);
   signal?.throwIfAborted();
   return readRelightAppliedVersion(response,selected);
  }
  async applyReflectionSelected(review:Review,signal?:AbortSignal){
   signal?.throwIfAborted();
   const owned=ownRenderReviewBinding(review);
   if(owned.kind!=='image.reflection'||owned.state!=='decided'||owned.revision!==2)throw new Error('REFLECTION_APPLY_INVALID');
   const selected=await this.restorationResultSelection(owned,signal);
   if(!selected.image||selected.route==='skip')throw new Error('REFLECTION_APPLY_INVALID');
   signal?.throwIfAborted();
   const response=await this.mutate<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(owned.id)}/reflection-apply`,{decision_hash:selected.decision_hash},undefined,'POST',undefined,signal);
   signal?.throwIfAborted();
   return readReflectionAppliedVersion(response,selected);
  }
  async applyNegativeSelected(review:Review,signal?:AbortSignal){
   const owned=ownRenderReviewBinding(review);
   if(owned.kind!=='optical.negative'||owned.state!=='decided'||owned.revision!==2)throw new Error('NEGATIVE_APPLY_INVALID');
   const selected=await this.negativeResultSelection(owned,signal);
   if(!selected.image||selected.route==='skip')throw new Error('NEGATIVE_APPLY_INVALID');
   signal?.throwIfAborted();
   const response=await this.mutate<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(owned.id)}/negative-apply`,{decision_hash:selected.decision_hash},undefined,'POST',undefined,signal);
   signal?.throwIfAborted();
   return readNegativeAppliedVersion(response,selected);
  }
  async localPortraitResultSelection(review:Review,signal?:AbortSignal){
   const owned=ownRenderReviewBinding(review);
   if((owned.kind!=='portrait.hair'&&owned.kind!=='portrait.clothing')||owned.state!=='decided')throw new Error('LOCAL_PORTRAIT_RESULT_SELECTION_INVALID');
   return readLocalPortraitResultSelection(await this.get<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(owned.id)}/restoration-result-selection`,signal),owned);
  }
  async applyLocalPortraitSelected(review:Review,signal?:AbortSignal){
   signal?.throwIfAborted();
   const owned=ownRenderReviewBinding(review);
   const selected=await this.localPortraitResultSelection(owned,signal);
   if(!selected.image||selected.route==='skip')throw new Error('LOCAL_PORTRAIT_APPLY_INVALID');
   signal?.throwIfAborted();
   const response=await this.mutate<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(owned.id)}/local-portrait-apply`,{decision_hash:selected.decision_hash},undefined,'POST',undefined,signal);
   signal?.throwIfAborted();
   return readLocalPortraitAppliedVersion(response,selected);
  }
  async discoverLocalPortraitResult(run:string,signal?:AbortSignal){
    signal?.throwIfAborted();requireArchiveUUID(run);const {readLocalPortraitDiscovery}=await import('./local-portrait-result');
    const response=await this.get<unknown>(`/api/v1/tenants/{t}/runs/${encodeURIComponent(run)}/local-portrait-result`,signal);signal?.throwIfAborted();return readLocalPortraitDiscovery(response,run);
  }
  async localPortraitResult(workflow:string,run:string,signal?:AbortSignal){
   signal?.throwIfAborted();requireArchiveUUID(workflow);requireArchiveUUID(run);
   const response=await this.get<unknown>(`/api/v1/tenants/{t}/v2-workflows/${encodeURIComponent(workflow)}/local-portrait-result`,signal);
   return readLocalPortraitResult(response,run);
  }
  async applyLocalPortraitResult(workflow:string,result:LocalPortraitResult,signal?:AbortSignal){
   signal?.throwIfAborted();
   requireArchiveUUID(workflow);
   const selected=readLocalPortraitResult(result,result.run_id);
   signal?.throwIfAborted();
   const response=await this.mutate<unknown>(`/api/v1/tenants/{t}/v2-workflows/${encodeURIComponent(workflow)}/local-portrait-apply`,{result_id:selected.result_id,candidate_sha:selected.candidate.sha256},undefined,'POST',undefined,signal);
   signal?.throwIfAborted();
   return readLocalPortraitAutoAppliedVersion(response,selected);
  }
  async applyOpenEyesSelected(review:Review,signal?:AbortSignal){
   signal?.throwIfAborted();
   const owned=ownRenderReviewBinding(review);
   if(owned.kind!=='portrait.open_eyes'||owned.state!=='decided'||owned.revision!==2)throw new Error('OPEN_EYES_APPLY_INVALID');
   const selected=await this.restorationResultSelection(owned,signal);
   if(!selected.image||selected.route==='skip')throw new Error('OPEN_EYES_APPLY_INVALID');
   signal?.throwIfAborted();
   const response=await this.mutate<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(owned.id)}/open-eyes-apply`,{decision_hash:selected.decision_hash},undefined,'POST',undefined,signal);
   signal?.throwIfAborted();
   return readOpenEyesAppliedVersion(response,selected);
  }
  async applyGlareSelected(review:Review,signal?:AbortSignal){
   signal?.throwIfAborted();
   const owned=ownRenderReviewBinding(review);
   if(owned.kind!=='portrait.glare'||owned.state!=='decided'||owned.revision!==2)throw new Error('GLARE_APPLY_INVALID');
   const selected=await this.restorationResultSelection(owned,signal);
   if(!selected.image||selected.route==='skip')throw new Error('GLARE_APPLY_INVALID');
   signal?.throwIfAborted();
   const response=await this.mutate<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(owned.id)}/glare-apply`,{decision_hash:selected.decision_hash},undefined,'POST',undefined,signal);
   signal?.throwIfAborted();
   return readGlareAppliedVersion(response,selected);
  }
  async acknowledgeRelightViewed(review:Review,beforeToken:string,afterToken:string,signal?:AbortSignal){
   signal?.throwIfAborted();
   const owned=ownRenderReviewBinding(review);
   if(owned.kind!=='image.relight'||owned.state!=='pending'||owned.revision!==1||![beforeToken,afterToken].every(t=>typeof t==='string'&&t.length<=1600&&/^[-_A-Za-z0-9]+\.[-_A-Za-z0-9]+$/.test(t)))throw new Error('RELIGHT_VIEW_PAIR_INVALID');
   signal?.throwIfAborted();
   const response=await this.mutate<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(owned.id)}/relight-viewed`,{before_token:beforeToken,after_token:afterToken},undefined,'POST',undefined,signal);
   signal?.throwIfAborted();
   return readRelightViewedReceipt(response);
  }
  async decide(review:Review,decision:"accept"|"reject"|"skip",accepted:string[]=[],rejected:string[]=[],key?:string,signal?:AbortSignal,viewedToken?:string){signal?.throwIfAborted();const before=readReviewView(review);if(before.state!=='pending'||before.revision>=Number.MAX_SAFE_INTEGER||!['accept','reject','skip'].includes(decision))throw Error('REVIEW_DECISION_INVALID');const raw=await this.mutate<unknown>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(before.id)}/decision`,{expected_revision:before.revision,candidate_hash:before.candidate_hash,decision,accepted_candidate_ids:[...accepted],rejected_candidate_ids:[...rejected],...(viewedToken?{viewed_token:viewedToken}:{})},key,"POST",undefined,signal);signal?.throwIfAborted();return readReviewDecision(raw,before,decision);}
  async presets(cursor?:string,signal?:AbortSignal){signal?.throwIfAborted();if(cursor!==undefined&&(typeof cursor!=="string"||!cursor||cursor.length>4096))throw Error("EXPORT_PRESET_CURSOR_INVALID");const query=cursor?'?cursor='+encodeURIComponent(cursor):'';const raw=await this.get<unknown>(`/api/v1/tenants/{t}/export-presets${query}`,signal);signal?.throwIfAborted();return readExportPresetPage(raw);}
  async changePresetMetadataPolicy(preset:ExportPreset,metadata_policy:ExportPreset["config"]["metadata_policy"],key:string=crypto.randomUUID(),signal?:AbortSignal,original:ExportPreset|null=preset){signal?.throwIfAborted();const frozen=readExportPresetResponse(preset),baseline=original===null?null:readExportPresetResponse(original);const value=await this.mutate<unknown>(`/api/v1/tenants/{t}/export-presets/${encodeURIComponent(frozen.id)}/metadata-policy`,{revision:frozen.revision,metadata_policy},key,'PATCH',undefined,signal);signal?.throwIfAborted();return readPresetMetadataReceipt(value,{id:frozen.id,revision:frozen.revision,mode:metadata_policy},baseline);}
  watermarkAssets(cursor?:string,signal?:AbortSignal){return this.get<import('./catalog-types').Page<import('./export-watermark-picker').WatermarkAsset>>(`/api/v1/tenants/{t}/assets?status=verified&limit=50${cursor?'&cursor='+encodeURIComponent(cursor):''}`,signal);}
  async exportBatchStatus(id:string,signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(id);const value=await this.get<unknown>(`/api/v1/tenants/{t}/exports/batches/${encodeURIComponent(id)}`,signal);signal?.throwIfAborted();return value;}
  async createExportBatch(request:import('./export-batch-attempt-store').ExportBatchRequest,key:string,signal?:AbortSignal){signal?.throwIfAborted();const frozen=freezeExportBatchRequest(request);const raw=await this.mutate<unknown>(`/api/v1/tenants/{t}/exports`,frozen,key,"POST",undefined,signal);signal?.throwIfAborted();return readExportBatchTicket(raw,frozen);}
  async createPreset(name:string,config:ExportPreset["config"],key:string=crypto.randomUUID(),signal?:AbortSignal){signal?.throwIfAborted();const request=freezeExportPresetCreate(name,config);const raw=await this.mutate<unknown>(`/api/v1/tenants/{t}/export-presets`,request,key,"POST",undefined,signal);signal?.throwIfAborted();return readCreatedExportPreset(raw,request);}
  async startDownload(selectionSnapshotId:string,presetId:string,key:string=crypto.randomUUID(),signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(selectionSnapshotId);requireArchiveUUID(presetId);const value=await this.mutate<unknown>(`/api/v1/tenants/{t}/download-jobs`,{selection_snapshot_id:selectionSnapshotId,preset_id:presetId},key,"POST",undefined,signal);signal?.throwIfAborted();return readDownloadAccepted(value);}
  async downloadJob(id:string,signal?:AbortSignal){
    requireArchiveUUID(id);signal?.throwIfAborted();
    const value=await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/download-jobs/${encodeURIComponent(id)}`),{method:'GET',signal});
    signal?.throwIfAborted();return readDownloadJob(value,id);
  }
  async cancelDownloadJob(id:string,signal?:AbortSignal){
    requireArchiveUUID(id);signal?.throwIfAborted();
    const value=await this.mutate<unknown>(`/api/v1/tenants/{t}/download-jobs/${encodeURIComponent(id)}/cancel`,undefined,crypto.randomUUID(),'POST',undefined,signal);
    signal?.throwIfAborted();return readDownloadJob(value,id);
  }
  async downloadJobVolume(id:string,volume:number,signal?:AbortSignal){
    requireArchiveUUID(id);if(!Number.isInteger(volume)||volume<1||volume>5000)throw new Error('DOWNLOAD_JOB_INVALID');
    signal?.throwIfAborted();
    const intent=await this.mutate<unknown>(`/api/v1/tenants/{t}/download-jobs/${encodeURIComponent(id)}/download-intent`,{volume},crypto.randomUUID(),'POST',undefined,signal);
    signal?.throwIfAborted();const token=readDeliveryDownloadIntent(intent,Date.now(),65000);
    const link=await this.client.request<unknown>('/api/v1/downloads/redeem',{method:'POST',body:{token},signal});
    signal?.throwIfAborted();return readDeliveryDownloadLink(link,undefined,Date.now(),Number.MAX_SAFE_INTEGER,0);
  }

  async collectionApplyCapabilities(collection:Pick<Collection,'id'|'project_id'|'revision'>,version:Pick<StyleVersion,'id'|'config_hash'>,signal?:AbortSignal){signal?.throwIfAborted();const scope={tenant:this.tenant,project:collection.project_id,collection:collection.id,revision:collection.revision,version:version.id,styleSHA:version.config_hash};requireArchiveUUID(scope.collection);requireArchiveUUID(scope.version);const query=new URLSearchParams({source_revision:scope.revision,style_version_id:scope.version}),value=await this.get<unknown>(`/api/v1/tenants/{t}/collections/${encodeURIComponent(scope.collection)}/apply-capabilities?${query}`,signal);signal?.throwIfAborted();return readApplyCapabilities(value,scope);}
  selectionSnapshot(collection:Collection,key?:string,signal?:AbortSignal){return this.mutate<{id:string;digest:string;item_count:number;expires_at:string}>(`/api/v1/tenants/{t}/collections/${encodeURIComponent(collection.id)}/selection-snapshots`,{collection_revision:collection.revision,scope:"all"},key,"POST",undefined,signal);}
  proposeSemanticIntent(input:SemanticEditIntent,key:string,signal?:AbortSignal){return this.client.proposeSemanticIntent(this.tenant,input,key,signal);}
  async contactSheet(selectionSnapshotId:string,locale:"ru"|"bg"|"en"="en",columns=4,rows=5,signal?:AbortSignal,key?:string){signal?.throwIfAborted();const request=freezeContactSheetAdmission(selectionSnapshotId,locale,columns,rows);const value=await this.mutate<unknown>(`/api/v1/tenants/{t}/contact-sheets`,request,key,"POST",undefined,signal);signal?.throwIfAborted();return readContactSheetAccepted(value);}
  download(id:string,signal?:AbortSignal){return this.downloadJob(id,signal);}
  async draft(assetId:string,signal?:AbortSignal){requireArchiveUUID(assetId);signal?.throwIfAborted();const value=await this.get<unknown>(`/api/v1/tenants/{t}/assets/${encodeURIComponent(assetId)}/editor-draft`,signal);signal?.throwIfAborted();return readLegacyEditorDraft(value);}
  async saveDraft(assetId:string,draft:EditorDraft,signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(assetId);const owned=readLegacyEditorDraft(draft),revision=legacyJSONRevision(owned.revision);const value=await this.mutate<unknown>(`/api/v1/tenants/{t}/assets/${encodeURIComponent(assetId)}/editor-draft`,{expected_revision:revision,asset_version_id:owned.asset_version_id,params:owned.params,mask_refs:owned.mask_refs},undefined,"PUT",{"If-Match":`"${owned.revision}"`},signal);signal?.throwIfAborted();const saved=readLegacyEditorDraft(value,owned);if(BigInt(saved.revision)!==BigInt(owned.revision)+1n)throw new Error("EDITOR_DRAFT_REVISION_INVALID");return saved;}
  async preview(draft:EditorDraft,scale:"proxy"|"full"="proxy",signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(draft.asset_version_id);const value=await this.mutate<RunAccepted>(`/api/v1/tenants/{t}/previews`,{asset_version_id:draft.asset_version_id,draft_revision:legacyJSONRevision(draft.revision),params:structuredClone(draft.params),scale,generation:false},undefined,"POST",undefined,signal);signal?.throwIfAborted();return readLegacyRunAccepted(value);}
  async promptMask(draft:EditorDraft,points:Array<{x:number;y:number;label:"positive"|"negative"}>,signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(draft.asset_version_id);const sourceSHA=draft.source_sha256;const value=await this.mutate<RunAccepted>(`/api/v1/tenants/{t}/masks`,{asset_version_id:draft.asset_version_id,input_sha256:draft.source_sha256,selector:"generation",points:structuredClone(points)},undefined,"POST",undefined,signal);signal?.throwIfAborted();return readLegacyMaskAccepted(value,sourceSHA);}
  async manualMask(draft:EditorDraft,strokes:Array<{kind:"stroke";sequence:number;mode:"add"|"subtract"|"protect";points:Array<{x:number;y:number}>;radius:number;hardness:number}>,polygons:Array<{kind:"polygon";sequence:number;mode:"add"|"subtract"|"protect";points:Array<{x:number;y:number}>}>=[],signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(draft.asset_version_id);const sourceSHA=draft.source_sha256;const value=await this.mutate<RunAccepted>(`/api/v1/tenants/{t}/masks`,{asset_version_id:draft.asset_version_id,input_sha256:draft.source_sha256,selector:"manual",strokes:structuredClone(strokes),polygons:structuredClone(polygons)},undefined,"POST",undefined,signal);signal?.throwIfAborted();return readLegacyMaskAccepted(value,sourceSHA);}
  async v2Layers(assetId:string,ref:ArtifactRef,binding:V2VersionBinding,signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(assetId);const pinned=structuredClone(ref),source=structuredClone(binding);requireArchiveUUID(pinned.artifact_id);if(!/^[0-9a-f]{64}$/.test(pinned.sha256)||!isRenderSourceBinding(source))throw new Error("EDITOR_LAYERS_INVALID");const value=await this.get(`/api/v1/tenants/{t}/assets/${encodeURIComponent(assetId)}/v2-layers/${encodeURIComponent(pinned.artifact_id)}?sha256=${encodeURIComponent(pinned.sha256)}`,signal);signal?.throwIfAborted();return readLayerDocument(value,pinned,source);}
  async v2Draft(assetId:string,signal?:AbortSignal){requireArchiveUUID(assetId);signal?.throwIfAborted();const value=await this.get<unknown>(`/api/v1/tenants/{t}/assets/${encodeURIComponent(assetId)}/v2-draft`,signal);signal?.throwIfAborted();return readV2DraftResource(value,this.tenant);}
  async v2Binding(assetId:string,signal?:AbortSignal){requireArchiveUUID(assetId);signal?.throwIfAborted();const value=await this.get<unknown>(`/api/v1/tenants/{t}/assets/${encodeURIComponent(assetId)}/v2-binding`,signal);signal?.throwIfAborted();if(!isRenderSourceBinding(value))throw new Error("V2_SOURCE_BINDING_INVALID");return structuredClone(value);}
  async v2DraftHistory(assetId:string,signal?:AbortSignal){requireArchiveUUID(assetId);signal?.throwIfAborted();const value=await this.get<unknown>(`/api/v1/tenants/{t}/assets/${encodeURIComponent(assetId)}/v2-draft/history?limit=1`,signal);signal?.throwIfAborted();return readV2DraftHistory(value,this.tenant);}
  async v2Document(documentId:string,signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(documentId);const value=await this.get<unknown>(`/api/v1/tenants/{t}/v2-documents/${encodeURIComponent(documentId)}`,signal);signal?.throwIfAborted();return readEditingRecipe(value);}
  async bridgeEditorRecipe(assetId:string,request:{expected_revision:string;recipe:V2EditingRecipe;layers:EditorLayerStack|null;editor_bridge:{revision:string;params:Record<string,unknown>;document_id:string}},key:string,signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(assetId);const owned=structuredClone(request);owned.recipe=readEditingRecipe(owned.recipe);const validRevision=(value:unknown)=>typeof value==="string"&&/^[1-9][0-9]{0,18}$/.test(value)&&BigInt(value)<=9223372036854775807n;
    if(!validRevision(owned.expected_revision)||!owned.editor_bridge||!validRevision(owned.editor_bridge.revision)||!archiveUUID(owned.editor_bridge.document_id)||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error("V2_DRAFT_REQUEST_INVALID");
    const params=owned.editor_bridge.params;if(!params||typeof params!=="object"||Array.isArray(params)||Object.keys(params).length!==2||params.schema_version!==1||!params.modules||typeof params.modules!=="object"||Array.isArray(params.modules)||JSON.stringify(params).length>1048576)throw new Error("V2_DRAFT_REQUEST_INVALID");
    owned.layers=owned.layers===null?null:readLayerStack(owned.layers,owned.recipe.source);const value=await this.mutate<unknown>(`/api/v1/tenants/{t}/assets/${encodeURIComponent(assetId)}/v2-draft`,owned,key,"PUT",undefined,signal);signal?.throwIfAborted();return this.savedV2Draft(value,owned.recipe.source.asset_version_id);}
  async saveV2Draft(assetId:string,revision:string,recipe:V2EditingRecipe,layers:EditorLayerStack|null=null,key:string=crypto.randomUUID(),signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(assetId);const owned=readEditingRecipe(recipe);if(typeof revision!=="string"||!/^(0|[1-9][0-9]{0,18})$/.test(revision)||BigInt(revision)>9223372036854775807n||!key||key.length>128||!/^[!-~]+$/.test(key))throw new Error("V2_DRAFT_REQUEST_INVALID");const value=await this.mutate<unknown>(`/api/v1/tenants/{t}/assets/${encodeURIComponent(assetId)}/v2-draft`,{expected_revision:revision,recipe:owned,layers:layers===null?null:readLayerStack(layers,owned.source)},key,"PUT",undefined,signal);signal?.throwIfAborted();return this.savedV2Draft(value,owned.source.asset_version_id);}
  private savedV2Draft(value:unknown,version:string){const saved=readV2DraftResource(value,this.tenant);if(saved.data.asset_version_id!==version)throw new Error("V2_DRAFT_SOURCE_CHANGED");return saved;}
  private protectedMask(output:RunOutput){
    if(output.role!=="mask_protected_ref"||output.media_type!=="application/json"||!output.artifact_id||!/^[0-9a-f]{64}$/.test(output.sha256))throw new Error("Invalid protected mask output");
    return {artifact_id:output.artifact_id,sha256:output.sha256};
  }
  async createV2Draft(assetId:string,version:StyleVersion,protection?:RunOutput,expected?:{asset_version_id:string;source_sha256:string},signal?:AbortSignal){
    signal?.throwIfAborted();version=structuredClone(version);
    if(version.approval_status!=="accepted"||!version.export_artifact_id||!/^[0-9a-f]{64}$/.test(version.config_hash))throw new Error("No accepted StyleV2 artifact");
    const mask=protection?this.protectedMask(protection):undefined;
    const expectedSource=expected?{...expected}:undefined;
    const binding=await this.v2Binding(assetId,signal);signal?.throwIfAborted();
    if(expectedSource&&(binding.asset_version_id!==expectedSource.asset_version_id||binding.source.sha256!==expectedSource.source_sha256))throw new Error("EDITOR_V2_SOURCE_CHANGED");
    const recipe:V2EditingRecipe={schema_version:3,source:binding,base_style:{artifact_id:version.export_artifact_id,sha256:version.config_hash},session_corrections:null,learning_version_id:null,overrides:[],layer_stack:null,...(mask?{protected_mask:mask}:{}),renderer_version:"v2.2",parameter_delta:{ev:0,log_r:0,log_g:0,log_b:0,saturation_delta:0,retouch_delta:0}};
    const saved=await this.saveV2Draft(assetId,"0",recipe,null,crypto.randomUUID(),signal);signal?.throwIfAborted();return saved;
  }
  async bindProtectionToV2Draft(assetId:string,output:RunOutput,signal?:AbortSignal){
    signal?.throwIfAborted();
    const mask=this.protectedMask(output);
    const draft=await this.v2Draft(assetId,signal);signal?.throwIfAborted();const history=await this.v2DraftHistory(assetId,signal);signal?.throwIfAborted();const current=history.items[0];
    if(!current||current.document.artifact_id!==draft.document.artifact_id||current.document.sha256!==draft.document.sha256)throw new Error("V2 draft changed");
    const recipe=await this.v2Document(current.id,signal);signal?.throwIfAborted();
    if(recipe.schema_version!==3)throw new Error("Unsupported V2 draft");
    if(recipe.protected_mask&& (recipe.protected_mask.artifact_id!==mask.artifact_id||recipe.protected_mask.sha256!==mask.sha256))throw new Error("Protection is already pinned");
    if(recipe.protected_mask)return draft;
    const layers=recipe.layer_stack===null?null:await this.v2Layers(assetId,recipe.layer_stack,recipe.source,signal);signal?.throwIfAborted();
    const saved=await this.saveV2Draft(assetId,draft.revision,{...recipe,protected_mask:mask},layers,crypto.randomUUID(),signal);signal?.throwIfAborted();return saved;
  }
  private generationBody(draft:EditorDraft,mask:MaskRef,prompt:string,seed:number,variantCount:number){requireArchiveUUID(draft.asset_version_id);const protectedRefs=(draft.mask_refs as MaskRef[]).filter(value=>value?.protected===true);return {asset_version_id:draft.asset_version_id,draft_revision:legacyJSONRevision(draft.revision),mask_ref:mask,protected_refs:protectedRefs,params:{prompt,negative_prompt:"",seed,steps:25,guidance_scale:7.5,feather_pixels:8},variant_count:variantCount};}
  async generate(draft:EditorDraft,mask:MaskRef,prompt:string,seed:number,variantCount=1,key:string=crypto.randomUUID(),signal?:AbortSignal,replayOnly=false){signal?.throwIfAborted();const body=structuredClone(this.generationBody(draft,mask,prompt,seed,variantCount)),value=await this.mutate<unknown>(`/api/v1/tenants/{t}/generations`,body,key,"POST",replayOnly?{"Generation-Replay-Only":"true"}:undefined,signal);signal?.throwIfAborted();return readLegacyRunAccepted(value);}
  async abandonGeneration(draft:EditorDraft,mask:MaskRef,prompt:string,seed:number,variantCount:number,key:string,signal?:AbortSignal){signal?.throwIfAborted();const body=structuredClone(this.generationBody(draft,mask,prompt,seed,variantCount)),value=await this.mutate<unknown>(`/api/v1/tenants/{t}/generations/abandon`,body,key,"POST",undefined,signal);signal?.throwIfAborted();return readGenerationAbandonment(value,key);}

  async createOverride(draft:EditorDraft,signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(draft.asset_version_id);const version=draft.asset_version_id,sourceSHA=draft.source_sha256;const params={...structuredClone(draft.params),input_sha256:draft.source_sha256,revision:1};const value=await this.mutate<OverrideRecord>(`/api/v1/tenants/{t}/overrides`,{asset_version_id:draft.asset_version_id,input_sha256:draft.source_sha256,params},undefined,"POST",undefined,signal);signal?.throwIfAborted();return readLegacyOverride(value,version,sourceSHA,params);}
  async submitSaveEdits(collectionID:string,body:SaveEditsBody,key:string,signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(collectionID);requireArchiveUUID(key);const request=freezeSaveEditsBody(body,body.selected_item_ids[0]);const value=await this.mutate<unknown>(`/api/v1/tenants/{t}/collections/${encodeURIComponent(collectionID)}/save-edits`,request,key,"POST",undefined,signal);signal?.throwIfAborted();return readCollectionApplyAccepted(value);}
  async resolveSaveEdits(collectionID:string,body:SaveEditsBody,key:string,signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(collectionID);requireArchiveUUID(key);const request=freezeSaveEditsBody(body,body.selected_item_ids[0]),raw=await this.mutate<unknown>(`/api/v1/tenants/{t}/collections/${encodeURIComponent(collectionID)}/save-edits`,request,key,"POST",{'Save-Edits-Resolve-Only':'abandon'},signal);signal?.throwIfAborted();return readSaveEditsResolution(raw,collectionID,key);}
  async saveEdits(collection:Collection,draft:EditorDraft,overrideId:string,itemId:string,styleVersionId?:string,signal?:AbortSignal,key:string=crypto.randomUUID()){const body:SaveEditsBody={source_revision:legacyJSONRevision(collection.revision),scope:"selected",selected_item_ids:[itemId],overrides_id:overrideId,output_name:`${collection.name} — edited`,...(styleVersionId?{style_version_id:styleVersionId}:{})};return this.submitSaveEdits(collection.id,body,key,signal);}

  async me(signal?:AbortSignal){signal?.throwIfAborted();const value=await this.get<unknown>("/api/v1/me",signal);signal?.throwIfAborted();return readUserProfile(value);}
  async updateMe(profile:UserProfile,patch:Partial<Pick<UserProfile,"locale"|"theme"|"timezone">>,signal?:AbortSignal){signal?.throwIfAborted();const before=readUserProfile(profile),request=freezeUserPreferences(patch),value=await this.mutate<unknown>("/api/v1/me",request,undefined,"PATCH",{"If-Match":`"${before.revision}"`},signal);signal?.throwIfAborted();return readUserProfileUpdate(value,before,request);}
  async tenantSettings(signal?:AbortSignal){signal?.throwIfAborted();const tenant=this.tenant,raw=await this.get<unknown>("/api/v1/tenants/{t}",signal);signal?.throwIfAborted();return readTenantSettings(raw,tenant);}
  async updateTenant(value:TenantSettings,patch:Partial<Pick<TenantSettings,"name"|"quotas"|"retention">>,signal?:AbortSignal){
    signal?.throwIfAborted();const tenant=this.tenant,before=readTenantSettings(value,tenant),request=structuredClone(patch);
    if(!request||typeof request!=='object'||Array.isArray(request)||!Object.keys(request).length||Object.keys(request).some(key=>!['name','quotas','retention'].includes(key)))throw Error('TENANT_SETTINGS_PATCH_INVALID');
    if(request.name!==undefined){if(typeof request.name!=='string')throw Error('TENANT_SETTINGS_PATCH_INVALID');request.name=request.name.trim();}
    readTenantSettings({...before,...request},tenant);
    const updated=await this.mutate<unknown>("/api/v1/tenants/{t}",structuredClone(request),undefined,"PATCH",{"If-Match":`"${before.revision}"`},signal);signal?.throwIfAborted();return readTenantSettingsUpdate(updated,tenant,before,request);
  }
  async members(signal?:AbortSignal,cursor?:string){const value=await this.get<unknown>(`/api/v1/tenants/{t}/members${cursor?`?cursor=${encodeURIComponent(cursor)}`:""}`,signal);signal?.throwIfAborted();return readMemberPage(value);}
  async invite(email:string,role:"admin"|"editor"|"viewer",locale:"en"|"ru"|"bg"="en",signal?:AbortSignal,key?:string){signal?.throwIfAborted();const request=freezeTenantInvitation(email,role,locale);const raw=await this.mutate<unknown>(`/api/v1/tenants/{t}/invites`,request,key,"POST",undefined,signal);signal?.throwIfAborted();return readInvitationReceipt(raw,request);}
  async updateMember(member:Member,role:Member["role"],status:string,signal?:AbortSignal,key?:string){
    const frozen=readMember(member);if(!['admin','editor','viewer'].includes(role)||!['active','suspended'].includes(status))throw new Error('MEMBER_REQUEST_INVALID');signal?.throwIfAborted();
    const raw=await this.mutate<unknown>(`/api/v1/tenants/{t}/members/${encodeURIComponent(frozen.user_id)}`,{role,status},key,"PATCH",{"If-Match":`"${frozen.revision}"`},signal);signal?.throwIfAborted();
    const result=readMember(raw,frozen);if(result.role!==role||result.status!==status||BigInt(result.revision)!==BigInt(frozen.revision)+1n)throw new Error('MEMBER_RESPONSE_INVALID');return result;
  }
  async capabilities(signal?:AbortSignal){signal?.throwIfAborted();const value=await this.get<unknown>("/api/v1/tenants/{t}/capabilities",signal);signal?.throwIfAborted();return readCapabilities(value);}
  async mediaLinks(assetId:string,assetVersionId:string,variants:MediaLink["variant"][],signal?:AbortSignal){signal?.throwIfAborted();requireArchiveUUID(assetId);requireArchiveUUID(assetVersionId);const selected=[...variants];if(selected.length<1||selected.length>4||new Set(selected).size!==selected.length||selected.some(v=>!['thumb320','thumb640','preview1600','preview2560'].includes(v)))throw Error('MEDIA_LINK_REQUEST_INVALID');const value=await this.mutate<unknown>(`/api/v1/tenants/{t}/assets/${encodeURIComponent(assetId)}/media-links`,{asset_version_id:assetVersionId,variants:[...selected]},undefined,"POST",undefined,signal);signal?.throwIfAborted();return readMediaLinks(value,assetVersionId,selected);}
  streamEvents(cursor:string|undefined,onEvent:(event:{cursor:string;type:string;payload:unknown})=>void,signal:AbortSignal){signal.throwIfAborted();let after=cursor===undefined?undefined:eventCursor(cursor);return this.client.eventStream(this.path('/api/v1/tenants/{t}/events'),after,event=>{signal.throwIfAborted();const row=readTenantEvent(event,after);after=row.cursor;onEvent(row);},signal);}
  async events(cursor?:string,signal?:AbortSignal){signal?.throwIfAborted();const after=cursor===undefined?undefined:eventCursor(cursor);const raw=await this.get<unknown>(`/api/v1/tenants/{t}/events${after!==undefined?`?after=${encodeURIComponent(after)}`:""}`,signal);signal?.throwIfAborted();return readTenantEventPage(raw,after);}
}
