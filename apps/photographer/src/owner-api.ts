import {readRegionDescriptor} from './region-preview-descriptor';
import {readLayerDocument,readLayerStack,type EditorLayerStack} from './editor-layers';
import {readSessionAnalyses} from './session-analysis-list';
import type {SceneAnalysisRequest} from './scene-analysis-request';
import {readSceneBoardDocument} from './scene-board';
import {v2ResourceData} from './v2-resource';
import {freezeRegionRequest,regionJobTicket} from './region-preview';
import type {RegionPreviewRequest} from './region-preview';
import type {CapturePerson,ShootSession,ShootSessionCreate,ShootSessionUpdate,CaptureAlert,CaptureFrameConfirmation,CaptureShotListView,CaptureShotRequirement} from "./capture-types";
import { ApiClient } from "@photoeditor/api-client";
import type {ArtifactRef,ApplyAccepted,Capabilities,Collection,CollectionItem,DownloadJob,EditorDraft,ExportPreset,MaskRef,MediaLink,Member,OverrideRecord,Page,Review,RunAccepted,RunOutput,RunStatus,StyleProfile,StyleVersion,TenantSettings,TrainingPair,TrainingSet,UserProfile,V2DocumentResource,V2DraftResource,V2EditingRecipe,V2VersionBinding} from "./catalog-types";
export class OwnerAPI {
  constructor(private client: ApiClient, readonly tenant: string) {}
  private path(value:string){return value.replace("{t}",encodeURIComponent(this.tenant));}
  async regionDescriptor(asset:string,run:string,request:RegionPreviewRequest,source:ArtifactRef,signal?:AbortSignal){const expected=freezeRegionRequest(request),pinned=structuredClone(source);return readRegionDescriptor(await this.get(`/api/v1/tenants/{t}/assets/${encodeURIComponent(asset)}/region-previews/${encodeURIComponent(run)}/descriptor`,signal),asset,expected,pinned);}
  async regionPreview(asset:string,request:RegionPreviewRequest,key:string,signal?:AbortSignal){const body=freezeRegionRequest(request);return regionJobTicket(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/assets/${encodeURIComponent(asset)}/region-previews`),{method:'POST',body,idempotencyKey:key,signal}));}
  async analyzeSession(id:string,request:SceneAnalysisRequest,key:string,signal?:AbortSignal){return regionJobTicket(await this.client.request<unknown>(this.path(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(id)}/analyses`),{method:'POST',body:structuredClone(request),idempotencyKey:key,signal}));}
  async sessionAnalyses(sessionId:string,cursor?:string,signal?:AbortSignal){return readSessionAnalyses(await this.get(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(sessionId)}/analyses?limit=100${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`,signal),sessionId);}
  async sceneBoard(analysisId:string,sessionId:string,signal?:AbortSignal){return readSceneBoardDocument(await this.get(`/api/v1/tenants/{t}/session-analyses/${encodeURIComponent(analysisId)}/scene-board`,signal),analysisId,sessionId);}
  get<T>(path:string,signal?:AbortSignal){return this.client.request<T>(this.path(path),{signal});}
  mutate<T>(path:string,body:unknown,key:string=crypto.randomUUID(),method="POST",headers?:HeadersInit,signal?:AbortSignal){return this.client.request<T>(this.path(path),{method,body,idempotencyKey:key,headers,signal});}
  async createShootSession(request:ShootSessionCreate,key:string=crypto.randomUUID()){return v2ResourceData<ShootSession>(await this.mutate('/api/v1/tenants/{t}/shoot-sessions',request,key),'shoot_sessions');}
  async updateShootSession(id:string,request:ShootSessionUpdate,key:string=crypto.randomUUID()){return v2ResourceData<ShootSession>(await this.mutate(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(id)}`,request,key,'PATCH'),'shoot_sessions',id);}
  async shootSession(id:string,signal?:AbortSignal){return v2ResourceData<ShootSession>(await this.get(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(id)}`,signal),'shoot_sessions',id);}
  async shootSessions(project:string,cursor?:string,signal?:AbortSignal){const page=await this.get<Page<unknown>>(`/api/v1/tenants/{t}/shoot-sessions?project_id=${encodeURIComponent(project)}&limit=100${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`,signal);return {...page,items:page.items.map(item=>v2ResourceData<ShootSession>(item,'shoot_sessions'))};}
  async capturePeople(project:string,cursor?:string,signal?:AbortSignal){const page=await this.get<Page<unknown>>(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/persons?limit=100${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`,signal);return {...page,items:page.items.map(item=>v2ResourceData<CapturePerson>(item,'project_persons'))};}
  captureShotList(sessionId:string,signal?:AbortSignal){return this.get<CaptureShotListView>(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(sessionId)}/shot-list`,signal);}
  replaceCaptureShotList(sessionId:string,revision:string,requirements:CaptureShotRequirement[],key:string=crypto.randomUUID()){return this.mutate<CaptureShotListView>(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(sessionId)}/shot-list`,{expected_revision:revision,requirements},key,"PUT");}
  captureAlerts(sessionId:string,cursor?:string,signal?:AbortSignal){return this.get<Page<CaptureAlert>>(`/api/v1/tenants/{t}/shoot-sessions/${encodeURIComponent(sessionId)}/alerts${cursor?`?cursor=${encodeURIComponent(cursor)}`:""}`,signal);}
  acknowledgeCaptureAlert(alert:CaptureAlert,key:string=crypto.randomUUID()){return this.mutate<CaptureAlert>(`/api/v1/tenants/{t}/capture-alerts/${encodeURIComponent(alert.id)}/ack`,{expected_revision:alert.revision},key);}
  confirmCaptureFrame(receiptId:string,alertId:string,key:string=crypto.randomUUID()){return this.mutate<CaptureFrameConfirmation>(`/api/v1/tenants/{t}/capture-receipts/${encodeURIComponent(receiptId)}/confirm`,{alert_id:alertId},key);}
  projects(cursor?:string){return this.get(`/api/v1/tenants/{t}/projects${cursor?`?cursor=${encodeURIComponent(cursor)}`:""}`);}
  collection(id:string){return this.get<Collection>(`/api/v1/tenants/{t}/collections/${encodeURIComponent(id)}`);}
  items(id:string,cursor?:string){return this.get<Page<CollectionItem>>(`/api/v1/tenants/{t}/collections/${encodeURIComponent(id)}/items${cursor?`?cursor=${encodeURIComponent(cursor)}`:""}`);}
  moderateApplyItem(collection:Collection,item:CollectionItem,decision:"accept"|"reject"){return this.mutate<CollectionItem>(`/api/v1/tenants/{t}/collections/${encodeURIComponent(collection.id)}/items/${encodeURIComponent(item.id)}/moderation`,{expected_revision:Number(collection.revision),decision});}
  run(id:string,signal?:AbortSignal){return this.get<RunStatus>(`/api/v1/tenants/{t}/runs/${encodeURIComponent(id)}`,signal);}
  runOutputs(id:string,signal?:AbortSignal){return this.get<{items:RunOutput[]}>(`/api/v1/tenants/{t}/runs/${encodeURIComponent(id)}/outputs`,signal);}
  styles(projectId?:string){const q=projectId?`?project_id=${encodeURIComponent(projectId)}`:"";return this.get<Page<StyleProfile>>(`/api/v1/tenants/{t}/styles${q}`);}
  createStyle(projectId:string,name:string,description=""){return this.mutate<StyleProfile>(`/api/v1/tenants/{t}/styles`,{project_id:projectId,name,description});}
  style(id:string){return this.get<StyleProfile>(`/api/v1/tenants/{t}/styles/${encodeURIComponent(id)}`);}
  styleVersions(id:string){return this.get<Page<StyleVersion>>(`/api/v1/tenants/{t}/styles/${encodeURIComponent(id)}/versions`);}
  createTrainingSet(styleId:string){return this.mutate<TrainingSet>(`/api/v1/tenants/{t}/styles/${encodeURIComponent(styleId)}/training-sets`,{});}
  trainingSet(id:string){return this.get<TrainingSet>(`/api/v1/tenants/{t}/training-sets/${encodeURIComponent(id)}`);}
  addPair(id:string,pair:{before_asset_version_id:string;after_asset_version_id:string;session_id:string;confirmed:boolean;included:boolean}){return this.mutate<TrainingPair>(`/api/v1/tenants/{t}/training-sets/${encodeURIComponent(id)}/pairs`,pair);}
  analyze(styleId:string,set:TrainingSet){return this.mutate<{run_id:string;status:string}>(`/api/v1/tenants/{t}/styles/${encodeURIComponent(styleId)}/analyses`,{training_set_id:set.id,training_set_revision:Number(set.revision),analysis_options:{alignment_mode:"auto",geometry_enabled:true,seed:0}});}
  apply(collection:Collection,styleVersionId:string,strength:number,outputName:string){return this.mutate<ApplyAccepted>(`/api/v1/tenants/{t}/collections/${encodeURIComponent(collection.id)}/apply-profile`,{source_revision:Number(collection.revision),style_version_id:styleVersionId,scope:"all",strength,output_name:outputName});}
  reviews(state="pending"){return this.get<Page<Review>>(`/api/v1/tenants/{t}/reviews?state=${encodeURIComponent(state)}`);}
  review(id:string){return this.get<Review>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(id)}`);}
  decide(review:Review,decision:"accept"|"reject"|"skip",accepted:string[]=[],rejected:string[]=[]){return this.mutate<Review>(`/api/v1/tenants/{t}/reviews/${encodeURIComponent(review.id)}/decision`,{expected_revision:review.revision,candidate_hash:review.candidate_hash,decision,accepted_candidate_ids:accepted,rejected_candidate_ids:rejected});}
  presets(){return this.get<Page<ExportPreset>>(`/api/v1/tenants/{t}/export-presets`);}
  createPreset(name:string,config:ExportPreset["config"]){return this.mutate<ExportPreset>(`/api/v1/tenants/{t}/export-presets`,{name,config});}
  startDownload(selectionSnapshotId:string,presetId:string){return this.mutate<{download_job_id:string;status:string}>(`/api/v1/tenants/{t}/download-jobs`,{selection_snapshot_id:selectionSnapshotId,preset_id:presetId});}
  selectionSnapshot(collection:Collection){return this.mutate<{id:string;digest:string;item_count:number;expires_at:string}>(`/api/v1/tenants/{t}/collections/${encodeURIComponent(collection.id)}/selection-snapshots`,{collection_revision:collection.revision,scope:"all"});}
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
  me(){return this.get<UserProfile>("/api/v1/me");}
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
