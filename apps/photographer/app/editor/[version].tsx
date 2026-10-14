import {EditorDelivery} from "../../src/editor-delivery";
import {EditorNegative} from "../../src/editor-negative";
import {EditorPerspective} from "../../src/editor-perspective";
import {EditorRelight} from "../../src/editor-relight";
import {EditorGlareDonor} from "../../src/editor-glare-donor";
import {EditorOpenEyes} from "../../src/editor-open-eyes";
import {EditorReflection} from "../../src/editor-reflection";
import {EditorLocalPortrait} from "../../src/editor-local-portrait";
import {EditorRenderCache,type PendingEditorRender} from "../../src/editor-render-cache";
import {cancelEditorRender} from "../../src/editor-render-cancel";
import {renderStateLabel} from "../../src/render-status";
import {watchRenderResults} from "../../src/render-results-poll";
import {renderPresentationLabel,loadEditorRenderResults,type RenderFrameResult} from "../../src/editor-render-results";
import {watchRenderRun} from "../../src/render-poll";
import {advanceRenderRunStatus,renderStatusLabel} from "../../src/render-status";
import {prepareEditorRenderFromServerPolicy,submitEditorRender} from "../../src/editor-render-submit";
import {prepareEditorRenderFreeze,freezeEditorRender} from "../../src/editor-render-freeze";
import {prepareV2LayerSave,saveV2Layers,V2LayerSavedRefreshRequired} from "../../src/editor-v2-save";
import {loadEditorV2Layers} from "../../src/editor-v2-load";
import {EditorLayerPanel} from "../../src/editor-layer-panel";
import {setEditorLayerStack} from "../../src/editor-layer-commands";
import {readLayerStack} from "../../src/editor-layers";
import AsyncStorage from "@react-native-async-storage/async-storage";
import {EditorDraftCache,editorCacheRestoreMode} from "../../src/editor-draft-cache";
import {APIError,canonicalJSON} from "@photoeditor/api-client";
import {useLocale} from "../../src/locale-context";
import {useEffect,useMemo,useRef,useState} from "react";
import {useLocalSearchParams,useRouter} from "expo-router";
import {AppState,Image,Pressable,SafeAreaView,ScrollView,Text,TextInput,View,type GestureResponderEvent,type LayoutChangeEvent} from "react-native";
import {useAuth} from "../../src/auth-context";
import {OwnerAPI} from "../../src/owner-api";
import type {Capabilities,Collection,EditorDraft,MaskRef,RunOutput,StyleProfile,StyleVersion} from "../../src/catalog-types";
import {catalogStyle as s} from "../../src/catalog-style";
import {appendPolygonPoint,appendPromptPoint,appendStrokePoint,finishMaskPolygon,startMaskStroke,type MaskPolygon,type MaskStroke,type PromptPoint} from "../../src/editor-mask";
import {acceptEditorDraftSave,acceptsEditorPreview,applyEditorCommand,beginEditorPreview,createEditorState,currentEditorSnapshot,prepareEditorDraftSave,resolveEditorDraftConflict,redoEditor,screenToSource,setEditorViewport,undoEditor,type EditorState,type EditorViewport} from "../../src/editor-store";
import {EditorPanels} from "../../src/editor-panels";
import {MaskCanvas} from "../../src/mask-canvas";
import {EditorDraftConflict} from "../../src/editor-draft-conflict";
import {CompareCanvas} from "../../src/compare-canvas";
import {PersistentSemanticCreate} from "../../src/persistent-semantic-create";

const initialParams={schema_version:1,modules:{strength:1}};

export default function Editor(){
  const query=useLocalSearchParams<{tenant:string;asset:string;version:string;collection?:string;item?:string}>();
  const {tenant,asset,version,collection:collectionId,item:itemId}=query;
  const router=useRouter(),auth=useAuth();
  const api=useMemo(()=>new OwnerAPI(auth.client(),tenant||""),[auth,tenant]);
  const {t}=useLocale();
  const[draft,setDraft]=useState<EditorDraft>();
  const[conflict,setConflict]=useState<EditorDraft>();
  const[collection,setCollection]=useState<Collection>();
  const[v2Styles,setV2Styles]=useState<StyleProfile[]>([]);
  const[v2Versions,setV2Versions]=useState<StyleVersion[]>([]);
  const[v2DraftStatus,setV2DraftStatus]=useState("");
  const[json,setJSON]=useState(JSON.stringify(initialParams,null,2));
  const[previewURLs,setPreviewURLs]=useState<string[]>([]);
  const[sourceURL,setSourceURL]=useState("");
  const[mask,setMask]=useState<MaskRef>();
  const[semanticMask,setSemanticMask]=useState<{artifact_id:string;sha256:string}>();
  const[protectedOutput,setProtectedOutput]=useState<RunOutput>();
  const[protectionStatus,setProtectionStatus]=useState("");
  const[points,setPoints]=useState<PromptPoint[]>([]),[pointLabel,setPointLabel]=useState<PromptPoint["label"]>("positive");
  const[maskTool,setMaskTool]=useState<"prompt"|"brush"|"polygon">("prompt"),[strokeMode,setStrokeMode]=useState<MaskStroke["mode"]>("add");
  const[strokes,setStrokes]=useState<MaskStroke[]>([]);
  const[polygons,setPolygons]=useState<MaskPolygon[]>([]),[pendingPolygon,setPendingPolygon]=useState<Array<{x:number;y:number}>>([]);
  const[canvasSize,setCanvasSize]=useState({width:0,height:0});
  const[imageAspect,setImageAspect]=useState(1);
  const[viewport,setViewport]=useState<EditorViewport>({scale:1,offsetX:0,offsetY:0});
  const[prompt,setPrompt]=useState(""),[seed,setSeed]=useState("1");
  const[error,setError]=useState(""),[busy,setBusy]=useState(false);
  const[capabilities,setCapabilities]=useState<Capabilities>();
  const editorState=useRef<EditorState|undefined>(undefined);
  const layerScope=JSON.stringify([auth.origin,tenant,asset,version]);
  const layerEpoch=useRef(0),layerRead=useRef<AbortController|null>(null),layerLock=useRef(false);
  const collectionEpoch=useRef(0),styleRequestEpoch=useRef(0);
  const [,refreshLayerPanel]=useState(0);
  const layerAttempt=useRef<ReturnType<typeof prepareV2LayerSave>|null>(null);
  const [layerSaveStatus,setLayerSaveStatus]=useState("");
  const freezeAttempt=useRef<ReturnType<typeof prepareEditorRenderFreeze>|null>(null);
  const [freezeStatus,setFreezeStatus]=useState("");
  const renderAttempt=useRef<Awaited<ReturnType<typeof prepareEditorRenderFromServerPolicy>>|null>(null);
  const [renderStatus,setRenderStatus]=useState("");
  const [watchedRun,setWatchedRun]=useState<string|null>(null);
  const [renderResults,setRenderResults]=useState<RenderFrameResult[]>([]);
  const [renderResultsError,setRenderResultsError]=useState<"render_results_access_lost"|"render_results_retrying"|null>(null);
  const [forgetRender,setForgetRender]=useState<PendingEditorRender|null>(null);
  const [pendingRender,setPendingRender]=useState<PendingEditorRender|null>(null);
  const renderCache=useRef<EditorRenderCache|null>(null);
  const renderTicket=useRef<Awaited<ReturnType<typeof submitEditorRender>>|null>(null);
  const renderObservation=useRef<ReturnType<typeof advanceRenderRunStatus>|null>(null);
  const frozenRenderSnapshot=useRef<Awaited<ReturnType<typeof freezeEditorRender>>|null>(null);
  const [layerContext,setLayerContext]=useState<{scope:string;api:OwnerAPI;loaded:Awaited<ReturnType<typeof loadEditorV2Layers>>}>();
  useEffect(()=>{layerEpoch.current++;layerRead.current?.abort();layerRead.current=null;setBusy(false);layerLock.current=false;layerAttempt.current=null;freezeAttempt.current=null;frozenRenderSnapshot.current=null;renderAttempt.current=null;renderTicket.current=null;renderCache.current=null;setPendingRender(null);setForgetRender(null);renderObservation.current=null;setWatchedRun(null);setRenderResults([]);setRenderResultsError(null);setRenderStatus("");setFreezeStatus("");setLayerSaveStatus("");setLayerContext(undefined);return()=>{layerEpoch.current++;layerRead.current?.abort();};},[layerScope,api]);

  useEffect(()=>{
    if(!watchedRun)return;
    const epoch=layerEpoch.current;let stop:(()=>void)|undefined;let stopResults:(()=>void)|undefined;
    const activate=(state:string)=>{
      stop?.();stop=undefined;stopResults?.();stopResults=undefined;
      if(state!=="active"||epoch!==layerEpoch.current||renderTicket.current?.run_id!==watchedRun)return;
      const ticket=renderTicket.current;
      stopResults=watchRenderResults({read:signal=>loadEditorRenderResults(api,ticket,signal),observe:frames=>{if(epoch===layerEpoch.current&&renderTicket.current?.run_id===watchedRun){setRenderResults(["cancelling","cancelled"].includes(renderObservation.current?.status??"")?[]:frames);setRenderResultsError(null);}},error:error=>{if(epoch===layerEpoch.current&&renderTicket.current?.run_id===watchedRun)setRenderResultsError(error instanceof APIError&&[401,403,404,410].includes(error.status)?"render_results_access_lost":"render_results_retrying");}});
      stop=watchRenderRun({run:watchedRun,read:(run,signal)=>api.run(run,signal),current:()=>renderObservation.current,
        observe:status=>{if(epoch!==layerEpoch.current||renderTicket.current?.run_id!==watchedRun)return;renderObservation.current=status;setRenderStatus(renderStatusLabel(status,t));if(["cancelling","cancelled"].includes(status.status)){setRenderResults([]);setRenderResultsError(null);}},
        error:()=>{if(epoch===layerEpoch.current)setRenderStatus(t("render_status_unavailable"));}});
    };
    activate(AppState.currentState??"active");
    const subscription=AppState.addEventListener("change",activate);
    return()=>{stop?.();stopResults?.();subscription.remove();};
  },[watchedRun,layerScope,api,t]);

  useEffect(()=>{
    const expiries=renderResults.flatMap(frame=>frame.images.map(image=>Date.parse(image.expires_at)));
    if(!expiries.length)return;
    const timer=setTimeout(()=>setRenderResults(current=>current.map(frame=>({...frame,images:frame.images.filter(image=>Date.parse(image.expires_at)>Date.now())}))),Math.min(2147483647,Math.max(0,Math.min(...expiries)-Date.now())));
    return()=>clearTimeout(timer);
  },[renderResults]);

  const draftCache=useRef<EditorDraftCache|undefined>(undefined);

  useEffect(()=>{
    if(!asset)return;
    let active=true;setConflict(undefined);setDraft(undefined);editorState.current=undefined;draftCache.current=undefined;
    void (async()=>{
      const server=await api.draft(asset);if(!active)return;
      if(server.asset_version_id!==version)throw new Error("EDITOR_ROUTE_SOURCE_CHANGED");
      let cached=null;
      try{
        const user=await api.me();if(!active)return;
        const store=new EditorDraftCache(AsyncStorage,auth.origin,user.id,tenant,server.asset_version_id);
        cached=await store.read();if(!active)return;draftCache.current=store;
        const history=new EditorRenderCache(AsyncStorage,auth.origin,user.id,tenant,server.asset_version_id);
        const previous=await history.read();if(!active)return;renderCache.current=history;
        const unfinished=await history.readPending();if(!active)return;setPendingRender(unfinished);
        if(previous&&!renderTicket.current){renderTicket.current=previous;renderObservation.current=null;setWatchedRun(previous.run_id);}
      }catch{/* Network draft editing remains available if local storage is unavailable. */}
      if(!active)return;
      const mode=cached?editorCacheRestoreMode(cached,server):undefined;
      const base=cached&&mode!=="source_changed"?cached.base:server;
      let state=createEditorState(base);
      if(cached&&mode!=="source_changed")state=applyEditorCommand(state,cached.local);
      editorState.current=state;setDraft(base);setJSON(JSON.stringify(currentEditorSnapshot(state).params,null,2));
      if(mode==="conflict")setConflict(server);
      setError(mode==="source_changed"?t("editor_conflict_source_changed"):"");
    })().catch(error=>{if(active)setError(error instanceof Error&&error.message==="EDITOR_ROUTE_SOURCE_CHANGED"?t("editor_route_source_changed"):t("editor_source_unavailable"))});
    return()=>{active=false};
  },[asset,version,api]);
  useEffect(()=>{
    const epoch=++collectionEpoch.current;
    styleRequestEpoch.current++;
    setCollection(undefined);setV2Styles([]);setV2Versions([]);setV2DraftStatus("");
    if(collectionId)void (async()=>{
      const value=await api.collection(collectionId);
      const styles=await api.styles(value.project_id);
      if(epoch!==collectionEpoch.current)return;
      setCollection(value);setV2Styles(styles.items);
    })().catch(()=>{if(epoch===collectionEpoch.current)setError("Collection or its styles are unavailable.")});
    return()=>{collectionEpoch.current++};
  },[api,collectionId,asset,version]);
  useEffect(()=>{
    let active=true;setSourceURL("");setPreviewURLs([]);setMask(undefined);setProtectedOutput(undefined);
    if(asset&&version)api.mediaLinks(asset,version,["preview1600"]).then(value=>{
      if(!active)return;const source=value.items.find(item=>item.variant==="preview1600");
      if(!source)throw new Error("preview");setSourceURL(source.url);
    }).catch(()=>{if(active)setError("The source preview is unavailable.")});
    return()=>{active=false};
  },[api,asset,version]);
  useEffect(()=>{let active=true;setCapabilities(undefined);api.capabilities().then(value=>{if(active)setCapabilities(value)}).catch(()=>{if(active)setError("Operation availability could not be loaded.")});return()=>{active=false}},[api,asset,version]);
  const capability=(operation:string)=>capabilities?.operations.find(value=>value.operation===operation);

  function cacheSnapshot(base:EditorDraft,state:EditorState){
    if(state.versionId!==base.asset_version_id||state.sourceSha!==base.source_sha256||String(state.draftRevision)!==base.revision)return;
    void draftCache.current?.write({schema_version:1,base,local:currentEditorSnapshot(state),saved_at:Date.now()}).catch(()=>setError(t("editor_cache_failed")));
  }
  async function saveDraftWithConflict(payload:EditorDraft){
    try{return await api.saveDraft(asset,payload)}
    catch(error){
      if(error instanceof APIError&&(error.status===409||error.status===412)){
        const server=await api.draft(asset);
        if(editorState.current?.versionId===server.asset_version_id&&editorState.current.sourceSha===server.source_sha256)setConflict(server);
      }
      throw error;
    }
  }
  async function saveCurrent(){
    const epoch=layerEpoch.current;
    if(conflict)throw new Error("EDITOR_CONFLICT_UNRESOLVED");
    if(!draft||!asset)throw new Error("draft");
    const params=JSON.parse(json) as Record<string,unknown>;
    if(params.schema_version!==1||typeof params.modules!=="object"||params.modules===null)throw new Error("schema");
    if(!editorState.current)throw new Error("state");
    const submitted=editorState.current;
    const saved=await saveDraftWithConflict(prepareEditorDraftSave(submitted,draft,params));
    if(epoch!==layerEpoch.current)throw new Error("EDITOR_ROUTE_CHANGED");
    if(!editorState.current)throw new Error("state");
    const accepted=acceptEditorDraftSave(editorState.current,submitted,saved);
    editorState.current=accepted.state;cacheSnapshot(saved,accepted.state);
    setDraft(saved);if(accepted.replaceRecipe)setJSON(JSON.stringify(saved.params,null,2));
    else throw new Error("EDITOR_LOCAL_EDITS_CHANGED");
    return saved;
  }
  async function save(){const epoch=layerEpoch.current;setBusy(true);try{await saveCurrent();if(epoch===layerEpoch.current)setError("")}catch(error){if(epoch===layerEpoch.current)setError(error instanceof Error&&error.message==="EDITOR_LOCAL_EDITS_CHANGED"?"The submitted revision was saved. Your newer local edits remain unsaved.":"Draft could not be saved. Check the recipe and reload if its revision changed.")}finally{if(epoch===layerEpoch.current)setBusy(false)}}
  async function waitForOutputs(runId:string){
    for(let attempt=0;attempt<90;attempt++){
      const run=await api.run(runId);
      if(run.status==="completed"||run.status==="waiting_review"){
        return (await api.runOutputs(runId)).items;
      }
      if(["failed","cancelled","completed_with_errors"].includes(run.status))throw new Error("run");
      await new Promise(resolve=>setTimeout(resolve,1000));
    }
    throw new Error("timeout");
  }
  async function preview(scale:"proxy"|"full"="proxy"){
    const epoch=layerEpoch.current;
    setBusy(true);setError("");
    try{const saved=await saveCurrent();if(epoch!==layerEpoch.current)return;if(!editorState.current)throw new Error("state");const request=beginEditorPreview(editorState.current);editorState.current=request.state;const accepted=await api.preview(saved,scale),outputs=await waitForOutputs(accepted.run_id),image=outputs.find(value=>value.role==="derivative");if(epoch!==layerEpoch.current)return;if(!image)throw new Error("output");if(editorState.current&&acceptsEditorPreview(editorState.current,request.token))setPreviewURLs([image.url])}
    catch{if(epoch===layerEpoch.current)setError("Preview could not be rendered. Check the recipe and worker availability.")}finally{if(epoch===layerEpoch.current)setBusy(false)}
  }
  async function makeMask(){
    if(!draft||!asset)return;
    if(points.length===0)return setError("Add at least one mask point on the rendered image.");
    const epoch=layerEpoch.current;setBusy(true);setError("");
    try{
      const saved=await saveCurrent(),accepted=await api.promptMask(saved,points);
      await persistMasks(saved,accepted.run_id,"prompt_mask_ref",value=>[value]);
    }catch{if(epoch===layerEpoch.current)setError("Mask could not be created. A qualified MobileSAM worker is required.")}finally{if(epoch===layerEpoch.current)setBusy(false)}
  }
  async function persistMasks(saved:EditorDraft,runId:string,role:string,select:(value:unknown)=>unknown[]){
      const epoch=layerEpoch.current;
      const submitted=editorState.current;
      if(conflict||!submitted)throw new Error("EDITOR_CONFLICT_UNRESOLVED");
      prepareEditorDraftSave(submitted,saved,saved.params);
      const outputs=await waitForOutputs(runId),ref=outputs.find(value=>value.role===role);
      if(epoch!==layerEpoch.current)throw new Error("EDITOR_ROUTE_CHANGED");
      if(!ref)throw new Error("mask");
      const response=await fetch(ref.url);if(!response.ok)throw new Error("mask-download");
      const values=select(await response.json()) as MaskRef[],value=values.find(item=>!item.protected);
      if(epoch!==layerEpoch.current)throw new Error("EDITOR_ROUTE_CHANGED");
      if(!value||values.length===0||values.some(item=>!item?.artifact?.artifact_id||!item.artifact.sha256))throw new Error("mask-contract");
      if(!editorState.current)throw new Error("state");
      // Recheck the captured source/revision before committing generated masks.
      prepareEditorDraftSave(editorState.current,saved,saved.params);
      const persisted=await saveDraftWithConflict({...saved,mask_refs:[...saved.mask_refs,...values]});
      if(epoch!==layerEpoch.current)throw new Error("EDITOR_ROUTE_CHANGED");
      if(!editorState.current)throw new Error("state");
      const accepted=acceptEditorDraftSave(editorState.current,submitted,persisted);
      editorState.current=accepted.state;cacheSnapshot(persisted,accepted.state);setDraft(persisted);
      if(!accepted.replaceRecipe){setConflict(persisted);throw new Error("EDITOR_LOCAL_EDITS_CHANGED")}
      setJSON(JSON.stringify(persisted.params,null,2));setMask(value);
      const semanticRoot=outputs.find(output=>output.role===(role==="rasterized_mask"?"mask_target_ref":"prompt_mask_ref")&&output.media_type==="application/json");
      setSemanticMask(semanticRoot?{artifact_id:semanticRoot.artifact_id,sha256:semanticRoot.sha256}:undefined);
      return outputs;
  }
  async function makeBrushMask(){
    if(!draft||!asset||strokes.length+polygons.length===0)return setError("Draw at least one stroke or polygon on the rendered image.");
    const epoch=layerEpoch.current;setBusy(true);setError("");
    try{setProtectedOutput(undefined);setProtectionStatus("");const saved=await saveCurrent(),accepted=await api.manualMask(saved,strokes,polygons);const outputs=await persistMasks(saved,accepted.run_id,"rasterized_mask",value=>{const result=value as{target?:unknown;protected?:unknown};return[result.target,result.protected]});if(epoch===layerEpoch.current)setProtectedOutput(outputs.find(value=>value.role==="mask_protected_ref"))}
    catch{if(epoch===layerEpoch.current)setError("Manual mask could not be rasterized. Check the strokes and CPU worker.")}finally{if(epoch===layerEpoch.current)setBusy(false)}
  }
  async function bindProtection(){
    if(!asset||!protectedOutput||layerLock.current||layerAttempt.current)return;
    const epoch=layerEpoch.current;layerLock.current=true;
    setBusy(true);setError("");setProtectionStatus("");
    try{await api.bindProtectionToV2Draft(asset,protectedOutput);if(epoch!==layerEpoch.current)return;setLayerContext(undefined);setLayerSaveStatus("");setProtectionStatus("Protection is pinned to this V2 draft. Training requires the same mask in its initial base checkpoint.")}
    catch{if(epoch===layerEpoch.current)setError("Protection could not be pinned. Create a V2 draft for this asset, or reload if its revision changed.")}
    finally{if(epoch===layerEpoch.current){layerLock.current=false;setBusy(false)}}
  }
  async function loadLayers(){
    const submitted=editorState.current;if(!submitted||layerLock.current||layerAttempt.current)return;
    const epoch=layerEpoch.current,controller=new AbortController();layerRead.current?.abort();layerRead.current=controller;layerLock.current=true;setBusy(true);setError("");
    try{
      const loaded=await loadEditorV2Layers(api,asset,submitted,controller.signal);
      if(controller.signal.aborted||epoch!==layerEpoch.current)return;
      if(editorState.current!==submitted)throw new Error("EDITOR_LOCAL_CHANGED");
      const local=currentEditorSnapshot(submitted).layerStack;
      const stack=local===undefined?loaded.stack:local===null?null:readLayerStack(local,loaded.binding);
      editorState.current=setEditorLayerStack(submitted,loaded.binding,stack);
      setLayerContext({scope:layerScope,api,loaded});refreshLayerPanel(value=>value+1);
      if(draft)cacheSnapshot(draft,editorState.current);
    }catch{if(!controller.signal.aborted&&epoch===layerEpoch.current)setError(t("layers_load_failed"));}
    finally{if(epoch===layerEpoch.current){layerLock.current=false;setBusy(false);if(layerRead.current===controller)layerRead.current=null;}}
  }
  async function saveLayers(){
    const submitted=editorState.current,context=layerContext;
    if(!submitted||!context||context.scope!==layerScope||context.api!==api||layerLock.current)return;
    const epoch=layerEpoch.current,controller=new AbortController();layerRead.current?.abort();layerRead.current=controller;layerLock.current=true;setBusy(true);setError("");setLayerSaveStatus("");
    try{
      const attempt=layerAttempt.current??prepareV2LayerSave(context.loaded,submitted,crypto.randomUUID());layerAttempt.current=attempt;
      const loaded=await saveV2Layers(api,asset,attempt,controller.signal);
      if(controller.signal.aborted||epoch!==layerEpoch.current)return;
      layerAttempt.current=null;setLayerContext({scope:layerScope,api,loaded});
      // Only advance the V2 baseline; local history and the legacy CAS revision remain intact.
      setLayerSaveStatus(t("layers_saved"));
    }catch(error){
      if(controller.signal.aborted||epoch!==layerEpoch.current)return;
      if(error instanceof V2LayerSavedRefreshRequired){layerAttempt.current=null;setLayerContext(undefined);setError(t("layers_saved_refresh"));}
      else if(error instanceof APIError&&[401,403,404,409,410,412,422].includes(error.status)){layerAttempt.current=null;setLayerContext(undefined);setError(t("layers_save_reload"));}
      else setError(t("layers_save_retry"));
    }finally{if(epoch===layerEpoch.current){layerLock.current=false;setBusy(false);refreshLayerPanel(value=>value+1);if(layerRead.current===controller)layerRead.current=null;}}
  }
  async function freezeSavedV2(){
    const submitted=editorState.current,context=layerContext;
    if(!submitted||!context||context.scope!==layerScope||context.api!==api||layerLock.current||layerAttempt.current)return;
    const epoch=layerEpoch.current,controller=new AbortController();layerRead.current?.abort();layerRead.current=controller;layerLock.current=true;setBusy(true);setError("");setFreezeStatus("");
    try{
      const prepared=prepareEditorRenderFreeze(context.loaded,submitted,crypto.randomUUID());
      if(!freezeAttempt.current||freezeAttempt.current.request.document_id!==prepared.request.document_id||freezeAttempt.current.request.expected_revision!==prepared.request.expected_revision)freezeAttempt.current=prepared;
      const snapshot=await freezeEditorRender(api,asset,freezeAttempt.current,controller.signal);
      if(epoch!==layerEpoch.current||controller.signal.aborted)return;
      if(frozenRenderSnapshot.current?.recipe.sha256!==snapshot.recipe.sha256||frozenRenderSnapshot.current?.recipe.artifact_id!==snapshot.recipe.artifact_id){renderAttempt.current=null;renderTicket.current=null;renderObservation.current=null;setWatchedRun(null);setRenderResults([]);setRenderResultsError(null);setRenderStatus("");}
      frozenRenderSnapshot.current=snapshot;
      setFreezeStatus(t("render_snapshot_ready"));
    }catch(error){if(epoch===layerEpoch.current&&!controller.signal.aborted)setError(error instanceof Error&&error.message==="EDITOR_V2_SAVE_REQUIRED"?t("render_save_required"):t("render_snapshot_failed"));}
    finally{if(epoch===layerEpoch.current){layerLock.current=false;setBusy(false);if(layerRead.current===controller)layerRead.current=null;}}
  }
  function prepareInterchangeExport(){
    const submitted=editorState.current,context=layerContext,snapshot=frozenRenderSnapshot.current;
    if(!submitted||!context||context.scope!==layerScope||context.api!==api||!snapshot||busy||layerLock.current||layerAttempt.current||conflict)return;
    try{
      const current=prepareEditorRenderFreeze(context.loaded,submitted,crypto.randomUUID());
      if(current.request.document_id!==snapshot.draft_document_id||current.request.expected_revision!==snapshot.draft_revision||canonicalJSON(current.binding)!==canonicalJSON(snapshot.source))throw new Error("EDITOR_V2_SOURCE_CHANGED");
      router.push({pathname:"/interchange/prepare",params:{tenant:tenant||"",project:context.loaded.draft.data.project_id,version:snapshot.source.asset_version_id,source:snapshot.source.source.artifact_id,source_sha:snapshot.source.source.sha256,canvas:snapshot.source.canvas_hash,recipe:snapshot.recipe.artifact_id,recipe_sha:snapshot.recipe.sha256}});
    }catch{setError(t("render_save_required"));}
  }
  async function renderSavedV2(){
    const submitted=editorState.current,context=layerContext,snapshot=frozenRenderSnapshot.current;
    if(!submitted||!context||context.scope!==layerScope||context.api!==api||!snapshot||layerLock.current||layerAttempt.current||pendingRender)return;
    const epoch=layerEpoch.current,controller=new AbortController();layerRead.current?.abort();layerRead.current=controller;layerLock.current=true;setBusy(true);setError("");
    try{
      const current=prepareEditorRenderFreeze(context.loaded,submitted,crypto.randomUUID());
      if(snapshot.draft_document_id!==current.request.document_id||snapshot.draft_revision!==current.request.expected_revision)throw new Error("EDITOR_V2_SOURCE_CHANGED");
      const project=context.loaded.draft.data.project_id;
      if(typeof project!=="string")throw new Error("EDITOR_V2_PROJECT_MISSING");
      if(!renderAttempt.current){
        const prepared=await prepareEditorRenderFromServerPolicy(api,project,snapshot,crypto.randomUUID(),crypto.randomUUID(),controller.signal);
        if(epoch!==layerEpoch.current||controller.signal.aborted||frozenRenderSnapshot.current!==snapshot)return;
        renderAttempt.current=prepared;
      }
      if(epoch!==layerEpoch.current||controller.signal.aborted||frozenRenderSnapshot.current!==snapshot)return;
      const history=renderCache.current;if(!history)throw new Error("RENDER_STORAGE_REQUIRED");
      const pending={project,command:renderAttempt.current};await history.writePending(pending);
      if(epoch!==layerEpoch.current||controller.signal.aborted)return;
      setPendingRender(pending);
      const ticket=await submitEditorRender(api,project,pending.command,controller.signal);
      if(epoch!==layerEpoch.current||controller.signal.aborted)return;
      if(renderTicket.current?.run_id!==ticket.run_id)renderObservation.current=null;
      renderTicket.current=ticket;setWatchedRun(ticket.run_id);
      try{await history.acceptPending(pending,ticket);if(epoch===layerEpoch.current&&!controller.signal.aborted)setPendingRender(null);}catch{if(epoch===layerEpoch.current&&!controller.signal.aborted)setError(t("render_history_failed"));}
      if(epoch!==layerEpoch.current||controller.signal.aborted)return;
      setRenderStatus(ticket.state==="waiting_review"?t("render_review_ready"):t("render_queued"));
    }catch{if(epoch===layerEpoch.current&&!controller.signal.aborted)setError(t("render_start_failed"));}
    finally{if(epoch===layerEpoch.current){layerLock.current=false;setBusy(false);if(layerRead.current===controller)layerRead.current=null;}}
  }
  async function retryPendingRender(){
    const pending=pendingRender,history=renderCache.current;if(!pending||!history||layerLock.current)return;
    const epoch=layerEpoch.current,controller=new AbortController();layerRead.current?.abort();layerRead.current=controller;layerLock.current=true;setBusy(true);setError("");
    try{
      await history.writePending(pending);
      if(epoch!==layerEpoch.current||controller.signal.aborted)return;
      const ticket=await submitEditorRender(api,pending.project,pending.command,controller.signal);
      if(epoch!==layerEpoch.current||controller.signal.aborted)return;
      if(renderTicket.current?.run_id!==ticket.run_id)renderObservation.current=null;
      renderTicket.current=ticket;setWatchedRun(ticket.run_id);setRenderStatus(ticket.state==="waiting_review"?t("render_review_ready"):t("render_queued"));
      try{await history.acceptPending(pending,ticket);if(epoch===layerEpoch.current&&!controller.signal.aborted)setPendingRender(null);}catch{if(epoch===layerEpoch.current&&!controller.signal.aborted)setError(t("render_history_failed"));}
    }catch{if(epoch===layerEpoch.current&&!controller.signal.aborted)setError(t("render_start_failed"));}
    finally{if(epoch===layerEpoch.current){layerLock.current=false;setBusy(false);if(layerRead.current===controller)layerRead.current=null;}}
  }
  async function forgetPendingRender(){
    const pending=pendingRender,history=renderCache.current;if(!pending||!history||layerLock.current||forgetRender!==pending)return;
    const epoch=layerEpoch.current;layerLock.current=true;setBusy(true);setError("");
    try{
      const removed=await history.clearPending(pending);
      if(epoch!==layerEpoch.current)return;
      if(!removed)throw new Error("RENDER_PENDING_CHANGED");
      setPendingRender(null);setForgetRender(null);renderAttempt.current=null;
    }catch{if(epoch===layerEpoch.current)setError(t("render_pending_forget_failed"));}
    finally{if(epoch===layerEpoch.current){layerLock.current=false;setBusy(false);}}
  }
  async function cancelRender(){
    const ticket=renderTicket.current;if(!ticket||layerLock.current)return;
    const epoch=layerEpoch.current,controller=new AbortController();layerRead.current?.abort();layerRead.current=controller;layerLock.current=true;setBusy(true);setError("");
    try{
      const response=await cancelEditorRender(api,ticket,controller.signal);
      if(epoch!==layerEpoch.current||controller.signal.aborted||renderTicket.current?.run_id!==ticket.run_id)return;
      const status=advanceRenderRunStatus(renderObservation.current,response,ticket.run_id);
      renderObservation.current=status;setRenderStatus(renderStatusLabel(status,t));
      if(["cancelling","cancelled"].includes(status.status)){setRenderResults([]);setRenderResultsError(null);}
    }catch{if(epoch===layerEpoch.current&&!controller.signal.aborted&&renderTicket.current?.run_id===ticket.run_id)setError(t("render_cancel_failed"));}
    finally{if(epoch===layerEpoch.current){layerLock.current=false;setBusy(false);if(layerRead.current===controller)layerRead.current=null;}}
  }
  async function refreshRender(){
    const ticket=renderTicket.current;if(!ticket||layerLock.current)return;
    const epoch=layerEpoch.current,controller=new AbortController();layerRead.current?.abort();layerRead.current=controller;layerLock.current=true;setBusy(true);
    try{
      const response=await api.run(ticket.run_id,controller.signal);
      if(epoch!==layerEpoch.current||controller.signal.aborted||renderTicket.current?.run_id!==ticket.run_id)return;
      const status=advanceRenderRunStatus(renderObservation.current,response,ticket.run_id);
      renderObservation.current=status;setRenderStatus(renderStatusLabel(status,t));setError("");if(["cancelling","cancelled"].includes(status.status)){setRenderResults([]);setRenderResultsError(null);}
    }catch{if(epoch===layerEpoch.current&&!controller.signal.aborted&&renderTicket.current?.run_id===ticket.run_id)setError(t("render_status_failed"));}
    finally{if(epoch===layerEpoch.current){layerLock.current=false;setBusy(false);if(layerRead.current===controller)layerRead.current=null;}}
  }
  async function showRenderResults(){
    const ticket=renderTicket.current;if(!ticket||layerLock.current)return;
    const epoch=layerEpoch.current,controller=new AbortController();layerRead.current?.abort();layerRead.current=controller;layerLock.current=true;setBusy(true);setError("");
    try{
      const frames=await loadEditorRenderResults(api,ticket,controller.signal);
      if(epoch!==layerEpoch.current||controller.signal.aborted||renderTicket.current?.run_id!==ticket.run_id)return;
      setRenderResults(["cancelling","cancelled"].includes(renderObservation.current?.status??"")?[]:frames);setRenderResultsError(null);
    }catch(error){if(epoch===layerEpoch.current&&!controller.signal.aborted&&renderTicket.current?.run_id===ticket.run_id){
      if(error instanceof APIError&&[401,403,404,410].includes(error.status)){setRenderResults([]);setRenderResultsError("render_results_access_lost");}
      else setError(t("render_results_failed"));
    }}
    finally{if(epoch===layerEpoch.current){layerLock.current=false;setBusy(false);if(layerRead.current===controller)layerRead.current=null;}}
  }
  async function createV2Draft(styleVersion:StyleVersion){
    if(!asset)return;
    const epoch=collectionEpoch.current;
    setBusy(true);setError("");setV2DraftStatus("");
    try{await api.createV2Draft(asset,styleVersion,protectedOutput);if(epoch===collectionEpoch.current)setV2DraftStatus(protectedOutput?"V2 draft and base checkpoint created with the protected mask.":"V2 draft created without a protected mask. For protected-mask training, rasterize the mask before creating the first draft.")}
    catch{if(epoch===collectionEpoch.current)setError("V2 draft could not be created. Use an accepted style from this project and check whether a V2 draft already exists.")}
    finally{if(epoch===collectionEpoch.current)setBusy(false)}
  }
  async function loadStyleVersions(styleId:string){
    const epoch=collectionEpoch.current;
    const request=++styleRequestEpoch.current;
    setV2Versions([]);
    try{const page=await api.styleVersions(styleId);if(epoch===collectionEpoch.current&&request===styleRequestEpoch.current)setV2Versions(page.items)}
    catch{if(epoch===collectionEpoch.current&&request===styleRequestEpoch.current)setError("Style versions are unavailable.")}
  }
  function addPoint(event:GestureResponderEvent){
    if(maskTool==="brush")return;
    try{const point=screenToSource({x:event.nativeEvent.locationX,y:event.nativeEvent.locationY},canvasSize,viewport);if(maskTool==="prompt")setPoints(current=>appendPromptPoint(current,{...point,label:pointLabel}));else setPendingPolygon(current=>appendPolygonPoint(current,point));setMask(undefined);setError("")}
    catch{setError("The mask supports at most 128 points.")}
  }
  function strokePoint(event:GestureResponderEvent){return screenToSource({x:event.nativeEvent.locationX,y:event.nativeEvent.locationY},canvasSize,viewport)}
  function beginStroke(event:GestureResponderEvent){
    if(maskTool!=="brush")return;try{const sequence=Math.max(-1,...strokes.map(x=>x.sequence),...polygons.map(x=>x.sequence))+1;setStrokes(current=>[...current,startMaskStroke(sequence,strokePoint(event),strokeMode)]);setMask(undefined);setError("")}catch{setError("The stroke could not be started.")}
  }
  function continueStroke(event:GestureResponderEvent){
    if(maskTool!=="brush")return;try{const point=strokePoint(event);setStrokes(current=>{if(current.length===0)return current;const copy=current.slice(),last=copy.length-1;copy[last]=appendStrokePoint(copy[last],point);return copy})}catch{setError("A stroke supports at most 4096 sampled points.")}
  }
  function finishPolygon(){try{const sequence=Math.max(-1,...strokes.map(x=>x.sequence),...polygons.map(x=>x.sequence))+1;setPolygons(current=>[...current,finishMaskPolygon(sequence,pendingPolygon,strokeMode)]);setPendingPolygon([]);setMask(undefined);setError("")}catch{setError("A polygon requires at least three vertices.")}}
  function undoShape(){const stroke=strokes.at(-1),polygon=polygons.at(-1);if((stroke?.sequence??-1)>(polygon?.sequence??-1))setStrokes(current=>current.slice(0,-1));else setPolygons(current=>current.slice(0,-1));setMask(undefined)}
  function measureCanvas(event:LayoutChangeEvent){const{width,height}=event.nativeEvent.layout;setCanvasSize({width,height})}
  async function generate(){
    if(!draft||!mask||!prompt.trim())return setError("Create a mask and enter a generation prompt first.");
    const value=Number(seed);if(!Number.isInteger(value)||value<0||value>4294967295)return setError("Seed must be an integer from 0 to 4294967295.");
    setBusy(true);setError("");
    try{
      const saved=await saveCurrent(),accepted=await api.generate(saved,mask,prompt.trim(),value,1);
      const outputs=await waitForOutputs(accepted.run_id),images=outputs.filter((item:RunOutput)=>item.role==="derivative").map(item=>item.url);
      if(images.length===0)throw new Error("generation-output");setPreviewURLs(images);
    }catch{setError("Generation failed. Check the mask, prompt, and qualified SD 1.5 worker.")}finally{setBusy(false)}
  }
  async function saveEdits(){
    if(!collection||!itemId)return setError("Open the editor from a collection to save a new version.");
    setBusy(true);setError("");
    try{
      const saved=await saveCurrent();
      const override=await api.createOverride(saved);
      const accepted=await api.saveEdits(collection,saved,override.id,itemId);
      router.push({pathname:"/runs/[id]",params:{id:accepted.run_id,tenant}});
    }catch{setError("Edits were not saved. Reload if the source collection or recipe changed.")}finally{setBusy(false)}
  }
  function resolveConflict(choice:"local"|"server"){
    if(!conflict||!editorState.current)return;
    try{
      editorState.current=resolveEditorDraftConflict(editorState.current,conflict,choice);
      cacheSnapshot(conflict,editorState.current);setDraft(conflict);if(choice==="server")setJSON(JSON.stringify(conflict.params,null,2));
      setConflict(undefined);setMask(undefined);setPreviewURLs([]);setError("");
    }catch{setError(t("editor_conflict_source_changed"))}
  }
  function editRecipe(text:string){
    setJSON(text);
    const current=editorState.current;if(!current)return;
    try{
      const params=JSON.parse(text) as Record<string,unknown>;
      if(!params||params.schema_version!==1||typeof params.modules!=="object"||params.modules===null||Array.isArray(params.modules))throw new Error("schema");
      editorState.current=applyEditorCommand(current,{params});
      if(draft)cacheSnapshot(draft,editorState.current);
    }catch{editorState.current={...current,requestRevision:current.requestRevision+1,pendingPreview:undefined}}
  }
  function navigateHistory(direction:"undo"|"redo"){
    if(!editorState.current)return;editorState.current=direction==="undo"?undoEditor(editorState.current):redoEditor(editorState.current);if(draft)cacheSnapshot(draft,editorState.current);const value=currentEditorSnapshot(editorState.current);setJSON(JSON.stringify(value.params,null,2));setMask(undefined);setError("");refreshLayerPanel(value=>value+1);
  }
  function zoom(value:number){if(!editorState.current)return;try{editorState.current=setEditorViewport(editorState.current,{...viewport,scale:value});setViewport(editorState.current.viewport)}catch{setError("Zoom must remain between 100% and 1600%.")}}

  return <SafeAreaView style={s.root}><ScrollView contentContainerStyle={s.header}>
    <Text style={s.title}>Editor</Text>
    <Text style={s.subtitle}>Asset version {version?.slice(0,8)} · revision {draft?.revision??"…"}</Text>
    {sourceURL&&previewURLs[0]?<CompareCanvas before={sourceURL} after={previewURLs[0]}/>:null}
    {(previewURLs.length?previewURLs:sourceURL?[sourceURL]:[]).map((url,index)=><MaskCanvas key={url} url={url} index={index} aspect={imageAspect} viewport={viewport} tool={maskTool} pointLabel={pointLabel} points={points} strokes={strokes} polygons={polygons} pendingPolygon={pendingPolygon} onLayout={measureCanvas} onImageSize={(width,height)=>setImageAspect(width/height)} onPoint={addPoint} onStrokeStart={beginStroke} onStrokeMove={continueStroke}/>)}
    {sourceURL||previewURLs.length?<><Text style={s.subtitle}>Viewport · {Math.round(viewport.scale*100)}%</Text><View style={{flexDirection:"row",gap:8}}><Pressable onPress={()=>zoom(Math.max(1,viewport.scale/1.25))} style={[s.button,{flex:1}]}><Text style={s.buttonText}>−</Text></Pressable><Pressable onPress={()=>zoom(Math.min(16,viewport.scale*1.25))} style={[s.button,{flex:1}]}><Text style={s.buttonText}>+</Text></Pressable><Pressable onPress={()=>{if(editorState.current)editorState.current=setEditorViewport(editorState.current,{scale:1,offsetX:0,offsetY:0});setViewport({scale:1,offsetX:0,offsetY:0})}} style={[s.button,{flex:2}]}><Text style={s.buttonText}>Reset view</Text></Pressable></View><View style={{flexDirection:"row",gap:8}}>{([[-25,0,"←"],[0,-25,"↑"],[0,25,"↓"],[25,0,"→"]]as const).map(([dx,dy,label])=><Pressable key={label} onPress={()=>{if(!editorState.current)return;const next={...viewport,offsetX:viewport.offsetX+dx,offsetY:viewport.offsetY+dy};editorState.current=setEditorViewport(editorState.current,next);setViewport(next)}} style={[s.button,{flex:1}]}><Text style={s.buttonText}>{label}</Text></Pressable>)}</View></>:null}
    <EditorPanels params={(()=>{try{return JSON.parse(json)as Record<string,unknown>}catch{return{}}})()} capabilities={capabilities} extendedApply onChange={value=>editRecipe(JSON.stringify(value,null,2))} onError={setError}/>
    <Text style={s.name}>Advanced Editor Params v1</Text><TextInput accessibilityLabel="Editor recipe" multiline value={json} onChangeText={editRecipe} autoCapitalize="none" style={[s.input,{minHeight:260,textAlignVertical:"top"}]}/>
    <View style={{flexDirection:"row",gap:8}}><Pressable disabled={busy||!editorState.current||editorState.current.historyIndex===0} onPress={()=>navigateHistory("undo")} style={[s.button,{flex:1}]}><Text style={s.buttonText}>Undo recipe</Text></Pressable><Pressable disabled={busy||!editorState.current||editorState.current.historyIndex>=editorState.current.history.length-1} onPress={()=>navigateHistory("redo")} style={[s.button,{flex:1}]}><Text style={s.buttonText}>Redo recipe</Text></Pressable></View>
    <Pressable accessibilityRole="button" disabled={busy||!draft} onPress={()=>void save()} style={s.button}><Text style={s.buttonText}>Save draft</Text></Pressable>
    {v2Styles.length?<><Text style={s.name}>Initial V2 draft style</Text>{v2Styles.map(style=><Pressable key={style.id} disabled={busy} onPress={()=>void loadStyleVersions(style.id)} style={s.button}><Text style={s.buttonText}>{style.name}</Text></Pressable>)}{v2Versions.filter(value=>value.approval_status==="accepted"&&!!value.export_artifact_id).map(value=><Pressable key={value.id} disabled={busy} onPress={()=>void createV2Draft(value)} style={s.button}><Text style={s.buttonText}>Create V2 draft · style version {value.version_no}</Text></Pressable>)}{v2DraftStatus?<Text style={s.subtitle}>{v2DraftStatus}</Text>:null}</>:null}
    <Pressable accessibilityRole="button" disabled={busy||!editorState.current||!!layerAttempt.current} onPress={()=>void loadLayers()} style={s.button}><Text style={s.buttonText}>{t("layers_load")}</Text></Pressable>
    {pendingRender?<View><Text style={s.meta}>{t("render_pending_restore")}</Text><Pressable accessibilityRole="button" disabled={busy} onPress={()=>void retryPendingRender()} style={s.button}><Text style={s.buttonText}>{t("render_pending_retry")}</Text></Pressable><Pressable accessibilityRole="button" disabled={busy} onPress={()=>setForgetRender(pendingRender)} style={s.button}><Text style={s.buttonText}>{t("render_pending_forget")}</Text></Pressable>{forgetRender===pendingRender?<View><Text style={s.meta}>{t("render_pending_forget_warning")}</Text><Pressable accessibilityRole="button" disabled={busy} onPress={()=>void forgetPendingRender()} style={s.button}><Text style={s.buttonText}>{t("render_pending_forget_confirm")}</Text></Pressable><Pressable accessibilityRole="button" disabled={busy} onPress={()=>setForgetRender(null)} style={s.button}><Text style={s.buttonText}>{t("render_pending_keep")}</Text></Pressable></View>:null}</View>:null}
    {renderTicket.current&&!(layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current)?<View>
      <Text style={s.meta}>{t("render_recovered")}</Text><Text style={s.meta}>{renderStatus}</Text>
      <Pressable accessibilityRole="button" disabled={busy} onPress={()=>void refreshRender()} style={s.button}><Text style={s.buttonText}>{t("render_refresh_status")}</Text></Pressable>
      <Pressable accessibilityRole="button" disabled={busy} onPress={()=>void showRenderResults()} style={s.button}><Text style={s.buttonText}>{t("render_show_results")}</Text></Pressable>
      <Pressable accessibilityRole="button" disabled={busy||!!renderObservation.current&&['completed','completed_with_errors','failed','cancelled','cancelling'].includes(renderObservation.current.status)} onPress={()=>void cancelRender()} style={s.button}><Text style={s.buttonText}>{t("render_cancel")}</Text></Pressable>
      {renderResultsError?<Text accessibilityRole="alert" style={s.meta}>{t(renderResultsError)}</Text>:null}
      {renderResults.map(frame=><View key={frame.run}><Text style={s.meta}>{t("render_frame")}: {renderStateLabel(frame.status,t)}</Text>{frame.images.length===0?<Text style={s.meta}>{renderPresentationLabel(frame,t)}</Text>:frame.images.map(image=><Image key={image.artifact_id} accessibilityLabel={t("render_image_accessibility")} source={{uri:image.url}} resizeMode="contain" style={{width:"100%",aspectRatio:1}}/>)}</View>)}
    </View>:null}
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current?<><Text style={s.meta}>{t("layers_local_only")}</Text><Pressable accessibilityRole="button" disabled={busy} onPress={()=>void saveLayers()} style={s.button}><Text style={s.buttonText}>{t(layerAttempt.current?"layers_retry_save":"layers_save")}</Text></Pressable><Text style={s.meta}>{layerSaveStatus}</Text><Pressable accessibilityRole="button" disabled={busy||!!layerAttempt.current} onPress={()=>void freezeSavedV2()} style={s.button}><Text style={s.buttonText}>{t("render_prepare_saved")}</Text></Pressable><Text style={s.meta}>{freezeStatus}</Text><Pressable accessibilityRole="button" disabled={busy||!!layerAttempt.current||!frozenRenderSnapshot.current||!!pendingRender} onPress={()=>void renderSavedV2()} style={s.button}><Text style={s.buttonText}>{t("render_process_saved")}</Text></Pressable><Text style={s.meta}>{renderStatus}</Text><Pressable accessibilityRole="button" disabled={busy||!renderTicket.current||!!renderObservation.current&&['completed','completed_with_errors','failed','cancelled','cancelling'].includes(renderObservation.current.status)} onPress={()=>void cancelRender()} style={s.button}><Text style={s.buttonText}>{t("render_cancel")}</Text></Pressable><Pressable accessibilityRole="button" disabled={busy||!renderTicket.current} onPress={()=>void refreshRender()} style={s.button}><Text style={s.buttonText}>{t("render_refresh_status")}</Text></Pressable><Pressable accessibilityRole="button" disabled={busy||!renderTicket.current} onPress={()=>void showRenderResults()} style={s.button}><Text style={s.buttonText}>{t("render_show_results")}</Text></Pressable>{renderResultsError?<Text accessibilityRole="alert" style={s.meta}>{t(renderResultsError)}</Text>:null}{renderResults.map(frame=><View key={frame.run}><Text style={s.meta}>{t("render_frame")}: {renderStateLabel(frame.status,t)}</Text>{frame.images.length===0?<Text style={s.meta}>{renderPresentationLabel(frame,t)}</Text>:frame.images.map(image=><Image key={image.artifact_id} accessibilityLabel={t("render_image_accessibility")} source={{uri:image.url}} resizeMode="contain" style={{width:"100%",aspectRatio:1}}/>)}</View>)}<EditorLayerPanel state={editorState.current} binding={layerContext.loaded.binding} disabled={busy||!!layerAttempt.current} onChange={state=>{setLayerSaveStatus("");editorState.current=state;refreshLayerPanel(value=>value+1);if(draft)cacheSnapshot(draft,state);}}/></>:null}
    <View style={{flexDirection:"row",gap:8}}><Pressable disabled={busy||!draft} onPress={()=>void preview("proxy")} style={[s.button,{flex:1}]}><Text style={s.buttonText}>Render proxy preview</Text></Pressable><Pressable disabled={busy||!draft} onPress={()=>void preview("full")} style={[s.button,{flex:1}]}><Text style={s.buttonText}>Render 100% preview</Text></Pressable></View>
    <View style={{flexDirection:"row",gap:8}}><Pressable disabled={capability("mask.prompt")?.available===false} onPress={()=>setMaskTool("prompt")} style={[s.button,{flex:1,opacity:maskTool==="prompt"?1:.55}]}><Text style={s.buttonText}>AI points</Text></Pressable><Pressable disabled={capability("mask.rasterize")?.available===false} onPress={()=>setMaskTool("brush")} style={[s.button,{flex:1,opacity:maskTool==="brush"?1:.55}]}><Text style={s.buttonText}>Brush</Text></Pressable><Pressable disabled={capability("mask.rasterize")?.available===false} onPress={()=>setMaskTool("polygon")} style={[s.button,{flex:1,opacity:maskTool==="polygon"?1:.55}]}><Text style={s.buttonText}>Polygon</Text></Pressable></View>
    {capability(maskTool==="prompt"?"mask.prompt":"mask.rasterize")?.available===false?<Text style={s.error}>{capability(maskTool==="prompt"?"mask.prompt":"mask.rasterize")?.reason||"This mask operation is unavailable."}</Text>:null}
    {maskTool==="prompt"?<><Text style={s.subtitle}>Generation mask · tap the rendered image to add points ({points.length}/128)</Text>
    <View style={{flexDirection:"row",gap:8}}><Pressable accessibilityState={{selected:pointLabel==="positive"}} onPress={()=>setPointLabel("positive")} style={[s.button,{flex:1,opacity:pointLabel==="positive"?1:.55}]}><Text style={s.buttonText}>Include</Text></Pressable><Pressable accessibilityState={{selected:pointLabel==="negative"}} onPress={()=>setPointLabel("negative")} style={[s.button,{flex:1,opacity:pointLabel==="negative"?1:.55}]}><Text style={s.buttonText}>Exclude</Text></Pressable></View>
    <View style={{flexDirection:"row",gap:8}}><Pressable disabled={busy||points.length===0} onPress={()=>{setPoints(current=>current.slice(0,-1));setMask(undefined)}} style={[s.button,{flex:1}]}><Text style={s.buttonText}>Undo point</Text></Pressable><Pressable disabled={busy||points.length===0} onPress={()=>{setPoints([]);setMask(undefined)}} style={[s.button,{flex:1}]}><Text style={s.buttonText}>Clear points</Text></Pressable></View>
    <Pressable disabled={busy||!draft||points.length===0} onPress={()=>void makeMask()} style={s.button}><Text style={s.buttonText}>{mask?"Rebuild generation mask":"Create generation mask"}</Text></Pressable></>:<><Text style={s.subtitle}>Manual mask · {maskTool==="brush"?"drag to paint":"tap vertices and finish the polygon"} ({strokes.length+polygons.length}/1024 shapes)</Text><View style={{flexDirection:"row",gap:8}}>{(["add","subtract","protect"] as const).map(mode=><Pressable key={mode} onPress={()=>setStrokeMode(mode)} style={[s.button,{flex:1,opacity:strokeMode===mode?1:.55}]}><Text style={s.buttonText}>{mode}</Text></Pressable>)}</View>{maskTool==="polygon"?<View style={{flexDirection:"row",gap:8}}><Pressable disabled={pendingPolygon.length<3} onPress={finishPolygon} style={[s.button,{flex:1}]}><Text style={s.buttonText}>Finish polygon ({pendingPolygon.length})</Text></Pressable><Pressable disabled={!pendingPolygon.length} onPress={()=>setPendingPolygon(current=>current.slice(0,-1))} style={[s.button,{flex:1}]}><Text style={s.buttonText}>Undo vertex</Text></Pressable></View>:null}<View style={{flexDirection:"row",gap:8}}><Pressable disabled={busy||strokes.length+polygons.length===0} onPress={undoShape} style={[s.button,{flex:1}]}><Text style={s.buttonText}>Undo shape</Text></Pressable><Pressable disabled={busy||strokes.length+polygons.length===0} onPress={()=>{setStrokes([]);setPolygons([]);setPendingPolygon([]);setMask(undefined)}} style={[s.button,{flex:1}]}><Text style={s.buttonText}>Clear shapes</Text></Pressable></View><Pressable disabled={busy||!draft||strokes.length+polygons.length===0} onPress={()=>void makeBrushMask()} style={s.button}><Text style={s.buttonText}>Rasterize manual mask</Text></Pressable></>}
    {protectedOutput?<Pressable disabled={busy||!!layerAttempt.current} onPress={()=>void bindProtection()} style={s.button}><Text style={s.buttonText}>Pin protection to V2 draft</Text></Pressable>:null}
    {protectionStatus?<Text style={s.subtitle}>{protectionStatus}</Text>:null}
    <TextInput accessibilityLabel="Generation prompt" value={prompt} onChangeText={setPrompt} placeholder="Describe the replacement" style={s.input}/>
    <TextInput accessibilityLabel="Generation seed" value={seed} onChangeText={setSeed} keyboardType="number-pad" style={s.input}/>
    {capability("image.generate")?.available===false?<Text style={s.error}>{capability("image.generate")?.reason||"Generation is unavailable."}</Text>:null}
    <Pressable accessibilityHint="Confirms that AI generation may modify every pixel inside the selected mask" disabled={busy||!draft||!mask||!prompt.trim()||capability("image.generate")?.available===false} onPress={()=>void generate()} style={s.button}><Text style={s.buttonText}>Confirm and generate candidate</Text></Pressable>
    <Pressable disabled={busy||!draft||!collection||!itemId} onPress={()=>void saveEdits()} style={s.button}><Text style={s.buttonText}>Save edits as new collection</Text></Pressable>
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current?<EditorNegative api={api} origin={auth.origin} tenant={tenant||""} project={layerContext.loaded.draft.data.project_id} asset={asset||""} version={version||""} disabled={busy||!!conflict||!!layerAttempt.current||capability("optical.negative")?.available===false} onSubmitted={ticket=>router.push({pathname:"/runs/[id]",params:{tenant:tenant||"",id:ticket.run_id}})}/>:null}
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current?<EditorPerspective selection={collection&&itemId&&asset&&version?{collection:collection.id,item:itemId,asset,revision:collection.revision,expectedVersion:version}:null} url={sourceURL} aspect={imageAspect} api={api} origin={auth.origin} tenant={tenant||""} project={layerContext.loaded.draft.data.project_id} asset={asset||""} version={version||""} disabled={busy||!!conflict||!!layerAttempt.current||capability("optical.perspective")?.available===false} onApplied={result=>router.replace({pathname:"/editor/[version]",params:{tenant:tenant||"",asset:asset||"",version:result.asset_version_id}})} onSubmitted={ticket=>router.push({pathname:"/runs/[id]",params:{tenant:tenant||"",id:ticket.run_id}})}/>:null}
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current&&frozenRenderSnapshot.current?<Pressable accessibilityRole="button" disabled={busy||!!conflict||!!layerAttempt.current} onPress={prepareInterchangeExport} style={s.button}><Text style={s.buttonText}>{t("interchange_editor_export_bundle")}</Text></Pressable>:null}
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current?<EditorDelivery api={api} tenant={tenant||""} project={layerContext.loaded.draft.data.project_id} asset={asset||""} version={version||""} disabled={busy||!!conflict||!!layerAttempt.current} onPrepared={snapshot=>router.push({pathname:"/deliveries/new",params:{tenant:tenant||"",project:layerContext.loaded.draft.data.project_id,snapshot}})}/>:null}
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current?<EditorRelight api={api} origin={auth.origin} tenant={tenant||""} project={layerContext.loaded.draft.data.project_id} asset={asset||""} version={version||""} disabled={busy||!!conflict||!!layerAttempt.current||capability("image.relight")?.available===false} onSubmitted={ticket=>router.push({pathname:"/runs/[id]",params:{tenant:tenant||"",id:ticket.run_id,relightWorkflow:ticket.workflow_id,relightAsset:asset||"",relightVersion:version||""}})}/>:null}
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current?<EditorGlareDonor api={api} origin={auth.origin} tenant={tenant||""} project={layerContext.loaded.draft.data.project_id} asset={asset||""} version={version||""} collection={collection?.id} disabled={busy||!!conflict||!!layerAttempt.current||capability("portrait.glare")?.available!==true} onSubmitted={ticket=>router.push({pathname:"/runs/[id]",params:{tenant:tenant||"",id:ticket.run_id}})}/>:null}
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current&&capability("portrait.open_eyes")?.available===true?<EditorOpenEyes api={api} origin={auth.origin} tenant={tenant||""} project={layerContext.loaded.draft.data.project_id} asset={asset||""} version={version||""} collection={collection?.id} disabled={busy||!!conflict||!!layerAttempt.current} onSubmitted={ticket=>router.push({pathname:"/runs/[id]",params:{tenant:tenant||"",id:ticket.run_id}})}/>:null}
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current&&capability("portrait.open_eyes")?.available!==true?<Text style={s.meta}>Open eyes: {capability("portrait.open_eyes")?.reason||"Unavailable on this deployment."}</Text>:null}
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current?<EditorReflection api={api} origin={auth.origin} tenant={tenant||""} project={layerContext.loaded.draft.data.project_id} asset={asset||""} version={version||""} disabled={busy||!!conflict||!!layerAttempt.current||capability("image.reflection")?.available!==true} onSubmitted={ticket=>router.push({pathname:"/runs/[id]",params:{tenant:tenant||"",id:ticket.run_id}})}/>:null}
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current?<EditorLocalPortrait api={api} origin={auth.origin} tenant={tenant||""} project={layerContext.loaded.draft.data.project_id} asset={asset||""} version={version||""} hairAvailable={capability("portrait.hair")?.available===true} clothingAvailable={capability("portrait.clothing")?.available===true} disabled={busy||!!conflict||!!layerAttempt.current} onSubmitted={ticket=>router.push({pathname:"/runs/[id]",params:{tenant:tenant||"",id:ticket.run_id}})}/>:null}
    {collection&&draft?<PersistentSemanticCreate origin={auth.origin} api={api} tenant={tenant||""} asset={asset||""} version={version||""} mask={mask?semanticMask:undefined} collection={collection} disabled={busy||!!conflict||capability("semantic.transfer")?.available===false} onSubmitted={ticket=>router.push({pathname:"/runs/[id]",params:{tenant:tenant||"",id:ticket.run_id,semanticWorkflow:ticket.workflow_id}})}/>:null}
    {conflict?<EditorDraftConflict localRecipe={json} localMasks={editorState.current?currentEditorSnapshot(editorState.current).maskRefs:[]} server={conflict} busy={busy} onResolve={resolveConflict}/>:null}
    {error?<Text accessibilityRole="alert" style={s.error}>{error}</Text>:null}
  </ScrollView></SafeAreaView>;
}
