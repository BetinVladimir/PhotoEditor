import {Button,TouchableRipple,Text,TextInput} from 'react-native-paper';
import {EditorSaveEditsAttemptStore,freezeSaveEditsBody,type SaveEditsBody} from '../../src/editor-save-edits-attempt';
import {readDeadline} from '../../src/read-deadline';
import {waitEditorStorage} from "../../src/editor-local-wait";
import {readEditorMaskArtifact} from "../../src/editor-mask-artifact";
import {loadApplyCatalogPages} from "../../src/collection-apply-pages";
import {readGenerationResolution} from "../../src/editor-generation-resolution";
import {GenerationTerminalError,generationResultState,type GenerationTerminalStatus} from "../../src/editor-generation-result";
import {EditorGenerationStore} from "../../src/editor-generation-store";
import {EditorGenerationAttempt} from "../../src/editor-generation-attempt";
import {admitEditorOperation} from "../../src/editor-operation-admission";
import {liveSemanticAvailability} from "../../src/semantic-kind-availability";
import {readEditorSourcePreview} from "../../src/editor-source-preview-link";
import {watchEditorSourcePreview} from "../../src/editor-source-preview-poll";
import {useEditorCapabilities} from "../../src/use-editor-capabilities";
import {operationAvailable} from "../../src/operation-availability";
import {EditorDelivery} from "../../src/editor-delivery";
import {EditorNegative} from "../../src/editor-negative";
import {EditorPerspective} from "../../src/editor-perspective";
import {EditorRelight} from "../../src/editor-relight";
import {EditorGlareDonor} from "../../src/editor-glare-donor";
import {EditorOpenEyes} from "../../src/editor-open-eyes";
import {EditorReflection} from "../../src/editor-reflection";
import {EditorLocalPortrait} from "../../src/editor-local-portrait";
import {EditorRecipeBridgeStore} from "../../src/editor-recipe-bridge-store";
import {EditorEffectsStore} from "../../src/editor-effects-store";
import {prepareEditorEffectsAttempt,submitEditorEffectsAttempt,type EditorEffectsAttempt} from "../../src/editor-effects-attempt";
import {observeEditorEffects} from "../../src/editor-effects-watch";
import {prepareEditorRecipeBridge,submitEditorRecipeBridge} from "../../src/editor-recipe-bridge";
import {EditorRenderCache,type PendingEditorRender} from "../../src/editor-render-cache";
import {cancelEditorRender} from "../../src/editor-render-cancel";
import {renderStateLabel} from "../../src/render-status";
import {watchRenderResults} from "../../src/render-results-poll";
import {readRenderImages,renderPresentationLabel,loadEditorRenderResults,type RenderFrameResult} from "../../src/editor-render-results";
import {watchRenderRun} from "../../src/render-poll";
import {advanceRenderRunStatus,renderStatusLabel} from "../../src/render-status";
import {prepareEditorRenderFromServerPolicy,recoverEditorRender,submitEditorRender} from "../../src/editor-render-submit";
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
import {useLocalSearchParams,useRouter} from '../../src/localized-router';
import {AppState,Image,SafeAreaView,ScrollView,View,type GestureResponderEvent,type LayoutChangeEvent} from 'react-native';
import {useAuth} from "../../src/auth-context";
import {OwnerAPI} from "../../src/owner-api";
import type {Collection,EditorDraft,MaskRef,RunOutput,StyleProfile,StyleVersion} from "../../src/catalog-types";
import {catalogStyle as s} from "../../src/catalog-style";
import {appendPolygonPoint,appendPromptPoint,appendStrokePoint,finishMaskPolygon,startMaskStroke,type MaskPolygon,type MaskStroke,type PromptPoint} from "../../src/editor-mask";
import {acceptEditorDraftSave,acceptsEditorPreview,applyEditorCommand,beginEditorPreview,createEditorState,currentEditorSnapshot,prepareEditorDraftSave,resolveEditorDraftConflict,redoEditor,screenToSource,setEditorViewport,undoEditor,type EditorState,type EditorViewport} from "../../src/editor-store";
import {EditorPanels} from "../../src/editor-panels";
import {MaskCanvas} from "../../src/mask-canvas";
import {EditorDraftConflict} from "../../src/editor-draft-conflict";
import {CompareCanvas} from "../../src/compare-canvas";
import {PersistentSemanticCreate} from "../../src/persistent-semantic-create";

const initialParams={schema_version:1,modules:{strength:1}};

function EditorScope(){
 const __r2Message=useLocale().message;

 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;

  const query=useLocalSearchParams<{tenant:string;asset:string;version:string;collection?:string;item?:string;project?:string}>();
  const {tenant,asset,version,collection:collectionId,item:itemId,project:projectId}=query;
  const router=useRouter(),auth=useAuth();
  const authority=useRef<{active:boolean;revoke:()=>void}>({active:true,revoke:()=>{}});
  const [accessLost,setAccessLost]=useState(false);
  const api=useMemo(()=>new Proxy(new OwnerAPI(auth.client(),tenant||""),{get(target,key){
    const member=Reflect.get(target,key,target);if(typeof member!=='function')return member;
    return (...args:unknown[])=>{const result:unknown=Reflect.apply(member,target,args);if(!result||typeof (result as Promise<unknown>).then!=='function')return result;
      return (result as Promise<unknown>).catch((error:unknown)=>{if(authority.current.active&&error instanceof APIError&&[401,403].includes(error.status))authority.current.revoke();throw error});};
  }}),[auth,tenant]);
  const {t}=useLocale();
  const[draft,setDraft]=useState<EditorDraft>();
  const[conflict,setConflict]=useState<EditorDraft>();
  const[collection,setCollection]=useState<Collection>();
  const[v2Styles,setV2Styles]=useState<StyleProfile[]>([]);
  const[v2Versions,setV2Versions]=useState<StyleVersion[]>([]);
  const[v2DraftStatus,setV2DraftStatus]=useState("");
  const[json,setJSON]=useState(JSON.stringify(initialParams,null,2));
  const[previewImages,setPreviewImages]=useState<RunOutput[]>([]);
  const previewURLs=previewImages.map(image=>image.url);
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
  const {capabilities,current:capabilityState}=useEditorCapabilities(api,JSON.stringify([tenant,asset,version]),()=>setError("Operation availability could not be loaded."));
  const semanticAvailability=useMemo(()=>liveSemanticAvailability(capabilityState),[capabilityState]);
  const editorState=useRef<EditorState|undefined>(undefined);
  const layerScope=JSON.stringify([auth.origin,tenant,asset,version]);
  const generationAttempt=useRef<EditorGenerationAttempt|null>(null),generationLock=useRef(false);
  const[generationPending,setGenerationPending]=useState(false);
  const[generationTerminal,setGenerationTerminal]=useState<GenerationTerminalStatus|null>(null);
  const generationStore=useRef<EditorGenerationStore|null>(null),generationCleanup=useRef(false);
  const saveEditsStore=useRef<{scope:string;api:OwnerAPI;store:EditorSaveEditsAttemptStore}|null>(null);
  const legacyFlight=useRef(new AbortController());
  const generationForeground=useRef((AppState.currentState??"active")==="active"),generationFlight=useRef<AbortController|null>(null),generationSequence=useRef(0);
  const[generationActive,setGenerationActive]=useState(generationForeground.current);
  const[generationReady,setGenerationReady]=useState(false),[generationFailed,setGenerationFailed]=useState(false),[generationBoot,setGenerationBoot]=useState(0);
  const layerEpoch=useRef(0),layerRead=useRef<AbortController|null>(null),layerLock=useRef(false);
  const collectionEpoch=useRef(0),styleRequestEpoch=useRef(0);
  const styleRead=useRef<AbortController|null>(null);
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
  const bridgeStore=useRef<EditorRecipeBridgeStore|null>(null);
  const effectsStore=useRef<EditorEffectsStore|null>(null);
  const [catalogError,setCatalogError]=useState("");
  const [effectsAttempt,setEffectsAttempt]=useState<EditorEffectsAttempt|null>(null);
  const [effectsTerminal,setEffectsTerminal]=useState(false);
  const [effectsReviewRun,setEffectsReviewRun]=useState<string|null>(null);
  const [bridgePending,setBridgePending]=useState(false);
  const [bridgeRejected,setBridgeRejected]=useState<string|null>(null);
  const [bridgeDiscardConfirm,setBridgeDiscardConfirm]=useState(false);
  const renderCache=useRef<EditorRenderCache|null>(null);
  const renderTicket=useRef<Awaited<ReturnType<typeof submitEditorRender>>|null>(null);
  const renderObservation=useRef<ReturnType<typeof advanceRenderRunStatus>|null>(null);
  const frozenRenderSnapshot=useRef<Awaited<ReturnType<typeof freezeEditorRender>>|null>(null);
  const [layerContext,setLayerContext]=useState<{scope:string;api:OwnerAPI;loaded:Awaited<ReturnType<typeof loadEditorV2Layers>>}>();
  authority.current.revoke=()=>{
    layerEpoch.current++;collectionEpoch.current++;styleRequestEpoch.current++;generationSequence.current++;
    legacyFlight.current.abort();generationFlight.current?.abort();layerRead.current?.abort();styleRead.current?.abort();
    editorState.current=undefined;draftCache.current=undefined;setDraft(undefined);setConflict(undefined);setLayerContext(undefined);
    setSourceURL("");setPreviewImages([]);setMask(undefined);setSemanticMask(undefined);setProtectedOutput(undefined);setRenderResults([]);setWatchedRun(null);
    setCollection(undefined);setV2Styles([]);setV2Versions([]);setAccessLost(true);
  };
  useEffect(()=>{authority.current.active=true;return()=>{authority.current.active=false}},[]);
  useEffect(()=>{
    legacyFlight.current.abort();
    const controller=new AbortController();legacyFlight.current=controller;
    if((AppState.currentState??"active")!=="active")controller.abort();
    return()=>controller.abort();
  },[layerScope,api]);
  useEffect(()=>{layerEpoch.current++;generationFlight.current?.abort();generationFlight.current=null;generationSequence.current++;generationCleanup.current=false;generationAttempt.current=null;generationLock.current=false;setGenerationTerminal(null);setGenerationPending(false);layerRead.current?.abort();layerRead.current=null;setBusy(false);layerLock.current=false;layerAttempt.current=null;freezeAttempt.current=null;frozenRenderSnapshot.current=null;renderAttempt.current=null;renderTicket.current=null;renderCache.current=null;bridgeStore.current=null;setBridgePending(false);setBridgeRejected(null);setBridgeDiscardConfirm(false);setPendingRender(null);setForgetRender(null);renderObservation.current=null;setWatchedRun(null);setRenderResults([]);setRenderResultsError(null);setRenderStatus("");setFreezeStatus("");setLayerSaveStatus("");setLayerContext(undefined);return()=>{layerEpoch.current++;generationSequence.current++;generationFlight.current?.abort();generationFlight.current=null;layerRead.current?.abort();};},[layerScope,api]);

  useEffect(()=>{
    const controller=new AbortController(),epoch=layerEpoch.current;
    generationStore.current=null;setGenerationReady(false);setGenerationFailed(false);setGenerationTerminal(null);
    if(!generationForeground.current)return()=>controller.abort();
    void(async()=>{try{
      const user=await api.me(controller.signal);controller.signal.throwIfAborted();
      const store=new EditorGenerationStore(AsyncStorage,auth.origin,user.id,tenant||"",asset||"",version||"");
      const saved=await store.read();controller.signal.throwIfAborted();if(epoch!==layerEpoch.current)return;
      generationStore.current=store;
      if(saved){generationCleanup.current=false;generationAttempt.current=EditorGenerationAttempt.restore(JSON.stringify(saved)).attach(store);setGenerationPending(true);}
      else if(!generationCleanup.current){generationAttempt.current=null;setGenerationPending(false);}
      setGenerationReady(true);
    }catch{if(!controller.signal.aborted&&epoch===layerEpoch.current)setGenerationFailed(true);}})();
    return()=>{controller.abort();generationStore.current=null;};
  },[api,layerScope,generationBoot]);

  useEffect(()=>{
    const subscription=AppState.addEventListener("change",state=>{
      if(state!=="active"){stopWaiting(false);}
      else if(legacyFlight.current.signal.aborted)legacyFlight.current=new AbortController();

      const active=state==="active";if(active===generationForeground.current)return;
      generationForeground.current=active;setGenerationActive(active);generationSequence.current++;
      if(generationFlight.current){generationFlight.current.abort();generationFlight.current=null;generationLock.current=false;setBusy(false);}
      setGenerationReady(false);setGenerationBoot(value=>value+1);
    });return()=>{subscription.remove();legacyFlight.current.abort()};
  },[]);

  useEffect(()=>{
    if(!watchedRun)return;
    const epoch=layerEpoch.current;let stop:(()=>void)|undefined;let stopResults:(()=>void)|undefined;
    const activate=(state:string)=>{
      stop?.();stop=undefined;stopResults?.();stopResults=undefined;
      // Signed presentations must be reacquired after foreground restoration.
      // A stopped observer cannot keep its previous authority visible.
      if(epoch===layerEpoch.current&&renderTicket.current?.run_id===watchedRun){setRenderResults([]);setRenderResultsError(null);}
      if(state!=="active"||epoch!==layerEpoch.current||renderTicket.current?.run_id!==watchedRun)return;
      const ticket=renderTicket.current;
      stopResults=watchRenderResults({read:signal=>loadEditorRenderResults(api,ticket,signal),observe:frames=>{if(epoch===layerEpoch.current&&renderTicket.current?.run_id===watchedRun){setRenderResults(["cancelling","cancelled"].includes(renderObservation.current?.status??"")?[]:frames);setRenderResultsError(null);}},error:error=>{if(epoch!==layerEpoch.current||renderTicket.current?.run_id!==watchedRun)return;if(error instanceof APIError&&[401,403,404,410].includes(error.status)){authority.current.revoke();return;}setRenderResultsError("render_results_retrying");}});
      stop=watchRenderRun({run:watchedRun,read:(run,signal)=>api.run(run,signal),current:()=>renderObservation.current,
        observe:status=>{if(epoch!==layerEpoch.current||renderTicket.current?.run_id!==watchedRun)return;renderObservation.current=status;setRenderStatus(renderStatusLabel(status,t));if(["cancelling","cancelled"].includes(status.status)){setRenderResults([]);setRenderResultsError(null);}},
        error:error=>{if(epoch!==layerEpoch.current)return;if(error instanceof APIError&&[401,403,404,410].includes(error.status)){authority.current.revoke();return;}setRenderStatus(t("render_status_unavailable"));}});
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

  useEffect(()=>{
    if(!previewImages.length)return;
    const expiry=Math.min(...previewImages.map(image=>Date.parse(image.expires_at)));
    const timer=setTimeout(()=>setPreviewImages(current=>current.filter(image=>Date.parse(image.expires_at)>Date.now())),Math.min(2147483647,Math.max(0,expiry-Date.now())));
    return()=>clearTimeout(timer);
  },[previewImages]);

  const draftCache=useRef<EditorDraftCache|undefined>(undefined);

  useEffect(()=>{effectsStore.current=null;setEffectsAttempt(null);setEffectsTerminal(false);setEffectsReviewRun(null);},[layerScope,api]);
  useEffect(()=>{
    if(!effectsAttempt?.accepted||accessLost)return;
    const epoch=layerEpoch.current;let stopped=false,flight:AbortController|null=null,timer:ReturnType<typeof setTimeout>|undefined;
    const stop=()=>{flight?.abort();flight=null;if(timer)clearTimeout(timer);};
    const active=()=>!stopped&&epoch===layerEpoch.current&&(AppState.currentState??"active")==="active";
    const poll=async()=>{
      if(!active())return;const controller=new AbortController();flight=controller;
      try{const result=await observeEditorEffects(api,effectsAttempt,controller.signal,active);if(!active()||controller.signal.aborted)return;
        setRenderResults(result.frames);setRenderResultsError(null);
        setEffectsTerminal(result.status.state==='complete'||result.status.state==='failed');
        setEffectsReviewRun(result.status.review_required?(result.status.final_run_id??result.status.build_run_id):null);
        setRenderStatus(result.status.review_required?"Photographer review is required before this recipe can continue.":result.status.state==='complete'?"Full recipe render complete.":result.status.state==='failed'?"Full recipe processing failed. The saved request is retained.":`Full recipe processing: ${result.status.state}.`);
        if(result.status.state==='failed')return;
      }catch(error){if(!active()||controller.signal.aborted)return;if(error instanceof APIError&&[401,403,404,410].includes(error.status)){setEffectsReviewRun(null);setEffectsTerminal(false);authority.current.revoke();return;}setRenderResultsError("render_results_retrying");}
      finally{if(flight===controller)flight=null;}
      if(active())timer=setTimeout(()=>void poll(),1500);
    };
    void poll();const subscription=AppState.addEventListener("change",state=>{stop();if(epoch===layerEpoch.current){setRenderResults([]);setRenderResultsError(null);setEffectsReviewRun(null);setEffectsTerminal(false);}if(state==='active')void poll();});
    return()=>{stopped=true;stop();subscription.remove();};
  },[effectsAttempt,accessLost,api,layerScope]);

  useEffect(()=>{
    if(!asset)return;
    const epoch=layerEpoch.current;let active=true;setConflict(undefined);setDraft(undefined);editorState.current=undefined;draftCache.current=undefined;
    void (async()=>{
      const server=await api.draft(asset);if(!active||epoch!==layerEpoch.current)return;
      if(server.asset_version_id!==version)throw new Error("EDITOR_ROUTE_SOURCE_CHANGED");
      let cached=null;
      try{
        const user=await api.me();if(!active||epoch!==layerEpoch.current)return;
        const store=new EditorDraftCache(AsyncStorage,auth.origin,user.id,tenant,server.asset_version_id);
        cached=await store.read();if(!active||epoch!==layerEpoch.current)return;draftCache.current=store;
        const bridges=new EditorRecipeBridgeStore(AsyncStorage,auth.origin,user.id,tenant,server.asset_version_id);
        const bridge=await bridges.read();if(!active||epoch!==layerEpoch.current)return;bridgeStore.current=bridges;setBridgePending(bridge!==null);
        const effects=new EditorEffectsStore(AsyncStorage,auth.origin,user.id,tenant,server.asset_version_id);
        const savedEffects=await effects.read();if(!active||epoch!==layerEpoch.current)return;effectsStore.current=effects;setEffectsAttempt(savedEffects);
        const history=new EditorRenderCache(AsyncStorage,auth.origin,user.id,tenant,server.asset_version_id);
        const previous=await history.read();if(!active||epoch!==layerEpoch.current)return;renderCache.current=history;
        const unfinished=await history.readPending();if(!active||epoch!==layerEpoch.current)return;setPendingRender(unfinished);
        if(previous&&!renderTicket.current){renderTicket.current=previous;renderObservation.current=null;setWatchedRun(previous.run_id);}
      }catch(error){
        if(error instanceof APIError&&[401,403,404].includes(error.status))throw error;
        /* Network draft editing remains available if local storage is unavailable. */
      }
      if(!active||epoch!==layerEpoch.current)return;
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
    const epoch=++collectionEpoch.current,controller=new AbortController();
    styleRequestEpoch.current++;styleRead.current?.abort();
    setCollection(undefined);setV2Styles([]);setV2Versions([]);setV2DraftStatus("");setCatalogError("");
    const active=()=>epoch===collectionEpoch.current&&!controller.signal.aborted;
    void(async()=>{
      if(!collectionId&&!projectId)return;
      const value=collectionId?await api.collection(collectionId,controller.signal):undefined;
      if(!active())return;
      const styles=await loadApplyCatalogPages(cursor=>api.styles(value?.project_id||projectId!,cursor,controller.signal),active);
      if(!active())return;
      setCollection(value);setV2Styles(styles);
    })().catch(()=>{if(active())setCatalogError(collectionId?"Collection or its styles are unavailable.":"Project styles are unavailable.")});
    return()=>{controller.abort();styleRead.current?.abort();collectionEpoch.current++;styleRequestEpoch.current++};
  },[api,collectionId,projectId,asset,version]);
  useEffect(()=>{
    setSourceURL("");setPreviewImages([]);setMask(undefined);setProtectedOutput(undefined);
    let stop:(()=>void)|undefined;
    const activate=(state:string)=>{
      stop?.();stop=undefined;setSourceURL("");
      if(accessLost||state!=="active"||!asset||!version)return;
      stop=watchEditorSourcePreview({read:async signal=>{
        const value=await api.mediaLinks(asset,version,["preview1600"],signal);
        return readEditorSourcePreview(value,version);
      },ready:setSourceURL,failed:()=>setError("The source preview is unavailable.")});
    };
    activate(AppState.currentState??"active");const subscription=AppState.addEventListener("change",activate);
    return()=>{subscription.remove();stop?.();};
  },[api,asset,version,accessLost]);

  const operationReady=(operation:string)=>operationAvailable(capabilities,operation);

  const legacyMutationLock=useRef<AbortSignal|null>(null);
  async function runLegacyMutation(action:()=>Promise<void>){
    const signal=legacyFlight.current.signal;
    if(legacyMutationLock.current&&!legacyMutationLock.current.aborted||accessLost||!authority.current.active||signal.aborted)return;
    legacyMutationLock.current=signal;
    try{await action()}finally{if(legacyMutationLock.current===signal)legacyMutationLock.current=null}
  }
  const save=()=>runLegacyMutation(performSave);
  const preview=(scale:"proxy"|"full"="proxy")=>runLegacyMutation(()=>performPreview(scale));
  const makeMask=()=>runLegacyMutation(performMakeMask);
  const makeBrushMask=()=>runLegacyMutation(performMakeBrushMask);
  const bindProtection=()=>runLegacyMutation(performBindProtection);
  const createV2Draft=(styleVersion:StyleVersion)=>runLegacyMutation(()=>performCreateV2Draft(styleVersion));
  const saveEdits=()=>runLegacyMutation(performSaveEdits);

  function cacheSnapshot(base:EditorDraft,state:EditorState){
    if(state.versionId!==base.asset_version_id||state.sourceSha!==base.source_sha256||String(state.draftRevision)!==base.revision)return;
    const epoch=layerEpoch.current,cache=draftCache.current;
    void cache?.write({schema_version:1,base,local:currentEditorSnapshot(state),saved_at:Date.now()}).catch(()=>{
      if(epoch===layerEpoch.current&&cache===draftCache.current&&authority.current.active)setError(t("editor_cache_failed"));
    });
  }
  async function saveDraftWithConflict(payload:EditorDraft,signal:AbortSignal){
    const epoch=layerEpoch.current;
    try{return await api.saveDraft(asset,payload,signal)}
    catch(error){
      if(!signal.aborted&&epoch===layerEpoch.current&&error instanceof APIError&&(error.status===409||error.status===412)){
        const server=await api.draft(asset,signal);
        if(!signal.aborted&&epoch===layerEpoch.current&&editorState.current?.versionId===server.asset_version_id&&editorState.current.sourceSha===server.source_sha256)setConflict(server);
      }
      throw error;
    }
  }
  async function saveCurrent(signal:AbortSignal=legacyFlight.current.signal){
    signal.throwIfAborted();
    const epoch=layerEpoch.current;
    if(conflict)throw new Error("EDITOR_CONFLICT_UNRESOLVED");
    if(!draft||!asset)throw new Error("draft");
    const params=JSON.parse(json) as Record<string,unknown>;
    if(params.schema_version!==1||typeof params.modules!=="object"||params.modules===null)throw new Error("schema");
    if(!editorState.current)throw new Error("state");
    const submitted=editorState.current;
    const saved=await saveDraftWithConflict(prepareEditorDraftSave(submitted,draft,params),signal);
    signal.throwIfAborted();
    if(epoch!==layerEpoch.current)throw new Error("EDITOR_ROUTE_CHANGED");
    if(!editorState.current)throw new Error("state");
    const accepted=acceptEditorDraftSave(editorState.current,submitted,saved);
    editorState.current=accepted.state;cacheSnapshot(saved,accepted.state);
    setDraft(saved);if(accepted.replaceRecipe)setJSON(JSON.stringify(saved.params,null,2));
    else throw new Error("EDITOR_LOCAL_EDITS_CHANGED");
    return saved;
  }
  function legacyCurrent(epoch:number,signal:AbortSignal){return epoch===layerEpoch.current&&legacyFlight.current.signal===signal&&!signal.aborted&&authority.current.active;}
  function stopWaiting(visible=true){
    const legacy=legacyMutationLock.current;const layer=layerRead.current;const generation=generationFlight.current;
    if(!legacy&&!layer&&!generation)return;
    legacyFlight.current.abort();legacyMutationLock.current=null;
    layer?.abort();layerRead.current=null;layerLock.current=false;
    generation?.abort();generationFlight.current=null;generationSequence.current++;generationLock.current=false;
    if((AppState.currentState??"active")==="active")legacyFlight.current=new AbortController();
    setBusy(false);if(visible)setError(t("editor_wait_stopped"));
  }
  async function performSave(){const epoch=layerEpoch.current,signal=legacyFlight.current.signal;setBusy(true);try{await saveCurrent(signal);if(legacyCurrent(epoch,signal))setError("")}catch(error){if(legacyCurrent(epoch,signal))setError(error instanceof Error&&error.message==="EDITOR_LOCAL_EDITS_CHANGED"?"The submitted revision was saved. Your newer local edits remain unsaved.":"Draft could not be saved. Check the recipe and reload if its revision changed.")}finally{if(legacyCurrent(epoch,signal))setBusy(false)}}
  async function waitForOutputs(runId:string,signal:AbortSignal=legacyFlight.current.signal,generation=false){
    for(let attempt=0;attempt<90;attempt++){
      signal?.throwIfAborted();const run=await (signal?api.run(runId,signal):api.run(runId));signal?.throwIfAborted();
      const generationState=generation?generationResultState(run.status):null;
      if(generationState==="terminal")throw new GenerationTerminalError(run.status as GenerationTerminalStatus);
      if(generationState==="outputs"||run.status==="completed"||run.status==="waiting_review"){
        const outputs=await (signal?api.runOutputs(runId,signal):api.runOutputs(runId));signal?.throwIfAborted();
        if(generation&&run.status==="completed_with_errors"&&!outputs.items.some(item=>item.role==="derivative"))throw new GenerationTerminalError("completed_with_errors");
        return outputs.items;
      }
      if(["failed","cancelled","completed_with_errors"].includes(run.status))throw new Error("run");
      await new Promise<void>((resolve,reject)=>{
        const abort=()=>{clearTimeout(timer);signal.removeEventListener('abort',abort);reject(signal.reason??new DOMException('Aborted','AbortError'))};
        const timer=setTimeout(()=>{signal.removeEventListener('abort',abort);resolve()},1000);
        signal.addEventListener('abort',abort,{once:true});if(signal.aborted)abort();
      });
    }
    throw new Error("timeout");
  }
  async function fullRecipePreview(){
    if(pendingRender||layerLock.current||layerAttempt.current)return;
    const epoch=layerEpoch.current,controller=new AbortController();layerRead.current?.abort();layerRead.current=controller;layerLock.current=true;setBusy(true);setError("");
    const current=()=>epoch===layerEpoch.current&&layerRead.current===controller&&!controller.signal.aborted&&generationForeground.current;
    setBridgeRejected(null);setBridgeDiscardConfirm(false);
    try{
      const store=bridgeStore.current,effects=effectsStore.current;if(!store||!effects)throw new Error("EDITOR_BRIDGE_STORAGE_REQUIRED");
      const existing=await effects.read();if(!current())return;
      if(existing){const recovered=await submitEditorEffectsAttempt(api,existing,controller.signal,current);if(!current())return;await effects.write(recovered.attempt);if(current())setEffectsAttempt(recovered.attempt);return;}
      let attempt=await store.read();if(!current())return;
      const freshBridge=!attempt;
      if(!attempt){
        const saved=await saveCurrent(controller.signal);if(!current())return;
        const submitted=editorState.current;if(!submitted)throw new Error("EDITOR_STATE_REQUIRED");
        const loaded=await loadEditorV2Layers(api,asset,submitted,controller.signal);if(!current())return;
        attempt=prepareEditorRecipeBridge(loaded,submitted,saved,crypto.randomUUID());
        await store.write(attempt);if(!current())return;setBridgePending(true);
      }
      const loaded=await submitEditorRecipeBridge(api,asset,attempt,controller.signal);if(!current())return;
      // Freeze the exact acknowledged immutable document, never the latest local
      // history. Review barriers remain owned by the normal render policy.
      if(freshBridge){const advisory=await readDeadline(controller.signal,signal=>api.editorCapabilities(asset,{document_id:loaded.draft.data.current_document_id as string,expected_revision:loaded.draft.revision,assume_profile:null},loaded.binding,signal));if(!current())return;
      if(!advisory.available){setError(t('capability_unsupported'));return;}}
      const snapshot=await freezeEditorRender(api,asset,{key:attempt.freeze_key,request:{document_id:loaded.draft.data.current_document_id as string,expected_revision:loaded.draft.revision,assume_profile:null},binding:loaded.binding},controller.signal);if(!current())return;
      const project=loaded.draft.data.project_id;if(typeof project!=="string")throw new Error("EDITOR_V2_PROJECT_MISSING");
      const command=prepareEditorEffectsAttempt(project,snapshot,crypto.randomUUID());
      await effects.write(command);if(!current())return;
      setEffectsAttempt(command);setLayerContext({scope:layerScope,api,loaded});frozenRenderSnapshot.current=snapshot;
      await store.clear(attempt.key);if(!current())return;setBridgePending(false);
      const accepted=await submitEditorEffectsAttempt(api,command,controller.signal,current);if(!current())return;
      await effects.write(accepted.attempt);if(!current())return;setEffectsAttempt(accepted.attempt);setRenderStatus("Full recipe processing queued; photographer review may be required.");
    }catch(error){if(current()){
      if(error instanceof APIError&&[400,404,409,410,412,422].includes(error.status)){try{const pending=await bridgeStore.current?.read();if(current()&&pending)setBridgeRejected(pending.key);}catch{/* Keep the unreadable journal; never replace it. */}}
      if(current())setError("Full recipe render needs a saved V2 style draft and an available render worker. Pending requests are retained for retry; review may be required.");
    }}
    finally{if(epoch===layerEpoch.current&&layerRead.current===controller){layerLock.current=false;setBusy(false);layerRead.current=null;}}
  }
  async function acknowledgeEffects(){
    const saved=effectsAttempt,store=effectsStore.current;if(!effectsTerminal||!saved?.accepted||!store||layerLock.current)return;
    const epoch=layerEpoch.current,controller=new AbortController();layerRead.current=controller;layerLock.current=true;setBusy(true);
    const current=()=>epoch===layerEpoch.current&&layerRead.current===controller&&!controller.signal.aborted;
    try{await waitEditorStorage(store.clearAccepted(saved.key,saved.accepted.run_id),controller.signal);if(current()){setEffectsAttempt(null);setEffectsTerminal(false);}}
    catch{if(current())setError("The completed request could not be acknowledged. Retry safely.");}
    finally{if(epoch===layerEpoch.current&&layerRead.current===controller){layerRead.current=null;layerLock.current=false;setBusy(false);}}
  }
  async function discardRejectedBridge(){
    const key=bridgeRejected,store=bridgeStore.current;if(!key||!store||pendingRender||layerLock.current)return;
    if(!bridgeDiscardConfirm){setBridgeDiscardConfirm(true);return;}
    const epoch=layerEpoch.current,controller=new AbortController();layerRead.current=controller;layerLock.current=true;setBusy(true);
    const current=()=>epoch===layerEpoch.current&&layerRead.current===controller&&!controller.signal.aborted;
    try{await waitEditorStorage(store.clear(key),controller.signal);if(current()){setBridgePending(false);setBridgeRejected(null);setBridgeDiscardConfirm(false);setLayerContext(undefined);setError("");}}
    catch{if(current())setError("Saved bridge could not be cleared.");}
    finally{if(epoch===layerEpoch.current&&layerRead.current===controller){layerRead.current=null;layerLock.current=false;setBusy(false);}}
  }
  async function performPreview(scale:"proxy"|"full"="proxy"){
    const params=editorState.current?currentEditorSnapshot(editorState.current).params:undefined;
    const modules=params?.modules;
    if(bridgePending||layerContext||modules&&typeof modules==="object"&&Object.keys(modules).some(name=>name!=="global"&&name!=="strength"))return fullRecipePreview();
    if(!operationAvailable(capabilityState.current,"editor.preview"))return setError("Preview is unavailable.");
    const epoch=layerEpoch.current,signal=legacyFlight.current.signal;
    setBusy(true);setError("");
    try{const saved=await saveCurrent(signal);if(epoch!==layerEpoch.current||signal.aborted)return;if(!editorState.current)throw new Error("state");const request=beginEditorPreview(editorState.current);editorState.current=request.state;const accepted=await admitEditorOperation("editor.preview",{current:()=>epoch===layerEpoch.current&&!signal.aborted,capabilities:()=>capabilityState.current},()=>api.preview(saved,scale,signal)),outputs=await waitForOutputs(accepted.run_id,signal),image=readRenderImages({items:outputs})[0];if(epoch!==layerEpoch.current||signal.aborted)return;if(!image)throw new Error("output");if(editorState.current&&acceptsEditorPreview(editorState.current,request.token))setPreviewImages([image])}
    catch{if(legacyCurrent(epoch,signal))setError("Preview could not be rendered. Check the recipe and worker availability.")}finally{if(legacyCurrent(epoch,signal))setBusy(false)}
  }
  async function performMakeMask(){
    if(!operationAvailable(capabilityState.current,"mask.prompt"))return setError("This mask operation is unavailable.");
    if(!draft||!asset)return;
    if(points.length===0)return setError("Add at least one mask point on the rendered image.");
    const epoch=layerEpoch.current,signal=legacyFlight.current.signal;setBusy(true);setError("");
    try{
      const saved=await saveCurrent(signal);if(epoch!==layerEpoch.current||signal.aborted)return;const accepted=await admitEditorOperation("mask.prompt",{current:()=>epoch===layerEpoch.current&&!signal.aborted,capabilities:()=>capabilityState.current},()=>api.promptMask(saved,points,signal));
      signal.throwIfAborted();
      await persistMasks(saved,accepted.run_id,"prompt_mask_ref",value=>[value],epoch,signal);
    }catch{if(legacyCurrent(epoch,signal))setError("Mask could not be created. A qualified MobileSAM worker is required.")}finally{if(legacyCurrent(epoch,signal))setBusy(false)}
  }
  async function persistMasks(saved:EditorDraft,runId:string,role:string,select:(value:unknown)=>unknown[],epoch:number,signal:AbortSignal=legacyFlight.current.signal){
      signal.throwIfAborted();
      if(epoch!==layerEpoch.current)throw new Error("EDITOR_ROUTE_CHANGED");
      const submitted=editorState.current;
      if(conflict||!submitted)throw new Error("EDITOR_CONFLICT_UNRESOLVED");
      prepareEditorDraftSave(submitted,saved,saved.params);
      const outputs=await waitForOutputs(runId,signal),ref=outputs.find(value=>value.role===role);
      signal.throwIfAborted();
      if(epoch!==layerEpoch.current)throw new Error("EDITOR_ROUTE_CHANGED");
      if(!ref)throw new Error("mask");
      const document=await readEditorMaskArtifact(ref,role,signal);
      signal.throwIfAborted();
      const values=select(document) as MaskRef[],value=values.find(item=>!item.protected);
      if(epoch!==layerEpoch.current)throw new Error("EDITOR_ROUTE_CHANGED");
      if(!value||values.length===0||values.some(item=>!item?.artifact?.artifact_id||!item.artifact.sha256))throw new Error("mask-contract");
      if(!editorState.current)throw new Error("state");
      // Recheck the captured source/revision before committing generated masks.
      prepareEditorDraftSave(editorState.current,saved,saved.params);
      const persisted=await saveDraftWithConflict({...saved,mask_refs:[...saved.mask_refs,...values]},signal);
      signal.throwIfAborted();
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
  async function performMakeBrushMask(){
    if(!operationAvailable(capabilityState.current,"mask.rasterize"))return setError("This mask operation is unavailable.");
    if(!draft||!asset||strokes.length+polygons.length===0)return setError("Draw at least one stroke or polygon on the rendered image.");
    const epoch=layerEpoch.current,signal=legacyFlight.current.signal;setBusy(true);setError("");
    try{setProtectedOutput(undefined);setProtectionStatus("");const saved=await saveCurrent(signal);if(epoch!==layerEpoch.current||signal.aborted)return;const accepted=await admitEditorOperation("mask.rasterize",{current:()=>epoch===layerEpoch.current&&!signal.aborted,capabilities:()=>capabilityState.current},()=>api.manualMask(saved,strokes,polygons,signal));const outputs=await persistMasks(saved,accepted.run_id,"rasterized_mask",value=>{const result=value as{target?:unknown;protected?:unknown};return[result.target,result.protected]},epoch,signal);if(epoch===layerEpoch.current&&!signal.aborted)setProtectedOutput(outputs.find(value=>value.role==="mask_protected_ref"))}
    catch{if(legacyCurrent(epoch,signal))setError("Manual mask could not be rasterized. Check the strokes and CPU worker.")}finally{if(legacyCurrent(epoch,signal))setBusy(false)}
  }
  async function performBindProtection(){
    if(!asset||!protectedOutput||layerLock.current||layerAttempt.current||bridgePending)return;
    const epoch=layerEpoch.current,signal=legacyFlight.current.signal;layerLock.current=true;
    setBusy(true);setError("");setProtectionStatus("");
    try{await api.bindProtectionToV2Draft(asset,protectedOutput,signal);if(epoch!==layerEpoch.current||signal.aborted)return;setLayerContext(undefined);setLayerSaveStatus("");setProtectionStatus("Protection is pinned to this V2 draft. Training requires the same mask in its initial base checkpoint.")}
    catch{if(legacyCurrent(epoch,signal))setError("Protection could not be pinned. Create a V2 draft for this asset, or reload if its revision changed.")}
    finally{if(legacyCurrent(epoch,signal)){layerLock.current=false;setBusy(false)}}
  }
  async function loadLayers(){
    const submitted=editorState.current;if(!submitted||layerLock.current||layerAttempt.current||bridgePending)return;
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
    finally{if(epoch===layerEpoch.current&&layerRead.current===controller){layerLock.current=false;setBusy(false);layerRead.current=null;}}
  }
  async function saveLayers(){
    const submitted=editorState.current,context=layerContext;
    if(!submitted||!context||context.scope!==layerScope||context.api!==api||layerLock.current||bridgePending)return;
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
    }finally{if(epoch===layerEpoch.current&&layerRead.current===controller){layerLock.current=false;setBusy(false);refreshLayerPanel(value=>value+1);layerRead.current=null;}}
  }
  async function freezeSavedV2(){
    const submitted=editorState.current,context=layerContext;
    if(!submitted||!context||context.scope!==layerScope||context.api!==api||layerLock.current||layerAttempt.current||bridgePending)return;
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
    finally{if(epoch===layerEpoch.current&&layerRead.current===controller){layerLock.current=false;setBusy(false);layerRead.current=null;}}
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
      const ticket=await submitEditorRender(api,project,pending.command,controller.signal,()=>epoch===layerEpoch.current&&operationAvailable(capabilityState.current,"render.compose"));
      if(epoch!==layerEpoch.current||controller.signal.aborted)return;
      if(renderTicket.current?.run_id!==ticket.run_id)renderObservation.current=null;
      renderTicket.current=ticket;setWatchedRun(ticket.run_id);
      try{await history.acceptPending(pending,ticket);if(epoch===layerEpoch.current&&!controller.signal.aborted){setPendingRender(null);const store=bridgeStore.current,bridge=await store?.read();if(bridge&&epoch===layerEpoch.current&&!controller.signal.aborted){await store?.clear(bridge.key);if(epoch===layerEpoch.current&&!controller.signal.aborted)setBridgePending(false);}}}catch{if(epoch===layerEpoch.current&&!controller.signal.aborted)setError(t("render_history_failed"));}
      if(epoch!==layerEpoch.current||controller.signal.aborted)return;
      setRenderStatus(ticket.state==="waiting_review"?t("render_review_ready"):t("render_queued"));
    }catch{if(epoch===layerEpoch.current&&!controller.signal.aborted)setError(t("render_start_failed"));}
    finally{if(epoch===layerEpoch.current&&layerRead.current===controller){layerLock.current=false;setBusy(false);layerRead.current=null;}}
  }
  async function retryPendingRender(){
    const pending=pendingRender,history=renderCache.current;if(!pending||!history||layerLock.current)return;
    const epoch=layerEpoch.current,controller=new AbortController();layerRead.current?.abort();layerRead.current=controller;layerLock.current=true;setBusy(true);setError("");
    try{
      await history.writePending(pending);
      if(epoch!==layerEpoch.current||controller.signal.aborted)return;
      const ticket=await recoverEditorRender(api,pending.project,pending.command,controller.signal,()=>epoch===layerEpoch.current);
      if(epoch!==layerEpoch.current||controller.signal.aborted)return;
      if(renderTicket.current?.run_id!==ticket.run_id)renderObservation.current=null;
      renderTicket.current=ticket;setWatchedRun(ticket.run_id);setRenderStatus(ticket.state==="waiting_review"?t("render_review_ready"):t("render_queued"));
      try{await history.acceptPending(pending,ticket);if(epoch===layerEpoch.current&&!controller.signal.aborted){setPendingRender(null);const store=bridgeStore.current,bridge=await store?.read();if(bridge&&epoch===layerEpoch.current&&!controller.signal.aborted){await store?.clear(bridge.key);if(epoch===layerEpoch.current&&!controller.signal.aborted)setBridgePending(false);}}}catch{if(epoch===layerEpoch.current&&!controller.signal.aborted)setError(t("render_history_failed"));}
    }catch{if(epoch===layerEpoch.current&&!controller.signal.aborted)setError(t("render_start_failed"));}
    finally{if(epoch===layerEpoch.current&&layerRead.current===controller){layerLock.current=false;setBusy(false);layerRead.current=null;}}
  }
  async function forgetPendingRender(){
    const pending=pendingRender,history=renderCache.current;if(!pending||!history||layerLock.current||forgetRender!==pending)return;
    const epoch=layerEpoch.current,controller=new AbortController();layerRead.current=controller;layerLock.current=true;setBusy(true);
    const current=()=>epoch===layerEpoch.current&&layerRead.current===controller&&!controller.signal.aborted;setError("");
    try{
      const removed=await waitEditorStorage(history.clearPending(pending),controller.signal);
      if(!current())return;
      if(!removed)throw new Error("RENDER_PENDING_CHANGED");
      setPendingRender(null);setForgetRender(null);renderAttempt.current=null;
    }catch{if(current())setError(t("render_pending_forget_failed"));}
    finally{if(epoch===layerEpoch.current&&layerRead.current===controller){layerRead.current=null;layerLock.current=false;setBusy(false);}}
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
    finally{if(epoch===layerEpoch.current&&layerRead.current===controller){layerLock.current=false;setBusy(false);layerRead.current=null;}}
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
    finally{if(epoch===layerEpoch.current&&layerRead.current===controller){layerLock.current=false;setBusy(false);layerRead.current=null;}}
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
    finally{if(epoch===layerEpoch.current&&layerRead.current===controller){layerLock.current=false;setBusy(false);layerRead.current=null;}}
  }
  async function performCreateV2Draft(styleVersion:StyleVersion){
    if(!asset||!draft||draft.asset_version_id!==version)return;
    const epoch=collectionEpoch.current,signal=legacyFlight.current.signal;
    setBusy(true);setError("");setV2DraftStatus("");
    try{await api.createV2Draft(asset,styleVersion,protectedOutput,{asset_version_id:version,source_sha256:draft.source_sha256},signal);if(epoch===collectionEpoch.current&&!signal.aborted)setV2DraftStatus(protectedOutput?"V2 draft and base checkpoint created with the protected mask.":"V2 draft created without a protected mask. For protected-mask training, rasterize the mask before creating the first draft.")}
    catch{if(epoch===collectionEpoch.current&&legacyFlight.current.signal===signal&&!signal.aborted)setError("V2 draft could not be created. Use an accepted style from this project and check whether a V2 draft already exists.")}
    finally{if(epoch===collectionEpoch.current&&legacyFlight.current.signal===signal&&!signal.aborted)setBusy(false)}
  }
  async function loadStyleVersions(styleId:string){
    const epoch=collectionEpoch.current,request=++styleRequestEpoch.current;
    styleRead.current?.abort();const controller=new AbortController();styleRead.current=controller;
    const active=()=>epoch===collectionEpoch.current&&request===styleRequestEpoch.current&&!controller.signal.aborted;
    setV2Versions([]);
    try{const versions=await loadApplyCatalogPages(cursor=>api.styleVersions(styleId,cursor,controller.signal),active);if(active())setV2Versions(versions)}
    catch{if(active())setError("Style versions are unavailable.")}
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
    if(!generationForeground.current||generationLock.current||!generationReady||!generationStore.current)return;
    if(!generationAttempt.current&&!operationAvailable(capabilityState.current,"image.generate"))return setError("Generation is unavailable.");
    if(!generationAttempt.current&&(!draft||!mask||!prompt.trim()))return setError("Create a mask and enter a generation prompt first.");
    const value=Number(seed);if(!generationAttempt.current&&(!Number.isInteger(value)||value<0||value>4294967295))return setError("Seed must be an integer from 0 to 4294967295.");
    const selected=mask?{mask:structuredClone(mask),prompt:prompt.trim(),seed:value}:null;
    generationLock.current=true;
    const epoch=layerEpoch.current,sequence=generationSequence.current,controller=new AbortController();generationFlight.current=controller;
    const currentScope=()=>epoch===layerEpoch.current&&sequence===generationSequence.current&&generationForeground.current&&!controller.signal.aborted;
    setBusy(true);setError("");
    try{
      if(!generationAttempt.current){
        if(!selected)throw new Error("mask");
        const saved=await saveCurrent(controller.signal);if(!currentScope())return;
        generationAttempt.current=new EditorGenerationAttempt(saved,selected.mask,selected.prompt,selected.seed).attach(generationStore.current!);setGenerationPending(true);
      }
      const current=generationAttempt.current!,store=generationStore.current!;
      const accepted=generationCleanup.current?current.accepted!:await current.submit((...args)=>{
        if(!currentScope())throw new Error("EDITOR_ROUTE_CHANGED");
        if(!operationAvailable(capabilityState.current,"image.generate"))return api.generate(...args,controller.signal,true);
        return admitEditorOperation("image.generate",{current:currentScope,capabilities:()=>capabilityState.current},()=>api.generate(...args,controller.signal));
      });
      if(!currentScope())return;
      const outputs=await waitForOutputs(accepted.run_id,controller.signal,true),images=readRenderImages({items:outputs});
      if(!currentScope())return;if(images.length===0)throw new Error("generation-output");generationCleanup.current=true;await store.clear(current.key);if(!currentScope())return;generationCleanup.current=false;setPreviewImages(images);generationAttempt.current=null;setGenerationTerminal(null);setGenerationPending(false);
    }catch(error){if(currentScope()){
      if(error instanceof GenerationTerminalError)setGenerationTerminal(error.status);
      setError(error instanceof GenerationTerminalError?"This generation ended without a usable result. Acknowledge it to start a new request.":"Generation failed. Check the mask, prompt, and qualified SD 1.5 worker.");
    }}finally{if(generationFlight.current===controller){generationFlight.current=null;generationLock.current=false;if(currentScope())setBusy(false)}}
  }
  async function abandonPendingGeneration(){
    const attempt=generationAttempt.current,store=generationStore.current;
    if(!attempt||attempt.accepted||!store||!generationForeground.current||!generationReady||generationLock.current)return;
    const epoch=layerEpoch.current,sequence=generationSequence.current,controller=new AbortController();
    const currentScope=()=>epoch===layerEpoch.current&&sequence===generationSequence.current&&generationForeground.current&&!controller.signal.aborted;
    generationFlight.current=controller;generationLock.current=true;setBusy(true);setError("");
    try{
      await store.write(attempt.snapshot);if(!currentScope())return;
      const input=attempt.snapshot.input;
      const response=await api.abandonGeneration(input.draft,input.mask,input.prompt,input.seed,1,attempt.key,controller.signal);
      if(!currentScope())return;const resolution=readGenerationResolution(response,attempt.key);
      if(resolution.kind==="accepted"){
        await attempt.submit(async()=>resolution.receipt);if(!currentScope())return;
        setError("This generation was already accepted. Retry to load its result.");
      }else{
        await store.clearAbandoned(resolution.receipt);if(!currentScope())return;
        generationAttempt.current=null;generationCleanup.current=false;setGenerationPending(false);setGenerationTerminal(null);
      }
    }catch{if(currentScope())setError("The pending generation could not be resolved. Your saved request has been kept.");}
    finally{if(generationFlight.current===controller){generationFlight.current=null;generationLock.current=false;if(currentScope())setBusy(false);}}
  }
  async function acknowledgeGenerationFailure(){
    const attempt=generationAttempt.current,store=generationStore.current,terminal=generationTerminal;
    if(!attempt?.accepted||!store||!terminal||!generationForeground.current||!generationReady||generationLock.current)return;
    const epoch=layerEpoch.current,sequence=generationSequence.current,controller=new AbortController();
    const currentScope=()=>epoch===layerEpoch.current&&sequence===generationSequence.current&&generationForeground.current&&!controller.signal.aborted;
    generationFlight.current=controller;generationLock.current=true;setBusy(true);setError("");
    try{
      const run=await api.run(attempt.accepted.run_id,controller.signal);if(!currentScope())return;
      if(run.status!==terminal)throw new Error("GENERATION_TERMINAL_CHANGED");
      if(terminal==="completed_with_errors"){
        const outputs=await api.runOutputs(attempt.accepted.run_id,controller.signal);if(!currentScope())return;
        if(outputs.items.some(item=>item.role==="derivative"))throw new Error("GENERATION_RESULT_AVAILABLE");
      }
      await store.clear(attempt.key);if(!currentScope())return;
      generationAttempt.current=null;generationCleanup.current=false;setGenerationPending(false);setGenerationTerminal(null);
    }catch{if(currentScope())setError("The generation outcome could not be confirmed. Your saved request has been kept.");}
    finally{if(generationFlight.current===controller){generationFlight.current=null;generationLock.current=false;if(currentScope())setBusy(false);}}
  }
  async function performSaveEdits(acknowledge=false,resolve=false,displayed?:{store:EditorSaveEditsAttemptStore;api:OwnerAPI;key:string}){
    if(!collection||!itemId)return setError("Open the editor from a collection to save a new version.");
    if((acknowledge||resolve)&&(!displayed||displayed.api!==api||saveEditsStore.current?.store!==displayed.store||displayed.store.identityKey()!==displayed.key))return;
    const epoch=layerEpoch.current,collectionRequest=collectionEpoch.current,signal=legacyFlight.current.signal;
    const current=()=>epoch===layerEpoch.current&&collectionRequest===collectionEpoch.current&&legacyFlight.current.signal===signal&&!signal.aborted;
    setBusy(true);setError("");
    try{
      const accepted=await readDeadline(signal,async child=>{
        const user=await api.me(child);child.throwIfAborted();if(!current())throw Error('SAVE_EDITS_SCOPE_CHANGED');
        const scope=JSON.stringify([auth.origin,user.id,tenant,version,collection.id,itemId]);
        if(saveEditsStore.current?.scope!==scope||saveEditsStore.current.api!==api)saveEditsStore.current={scope,api,store:new EditorSaveEditsAttemptStore(AsyncStorage,auth.origin,user.id,tenant||"",version||"",collection.id,itemId)};
        const store=saveEditsStore.current.store,pending=await store.read(child);child.throwIfAborted();
        if((acknowledge||resolve)&&(!displayed||displayed.store!==store||store.identityKey()!==displayed.key))throw Error('SAVE_EDITS_SCOPE_CHANGED');
        if(acknowledge){await store.acknowledge(displayed!.key,child);child.throwIfAborted();return null;}
        if(resolve)return store.resolve(displayed!.key,(body,key,requestSignal)=>api.resolveSaveEdits(collection.id,body,key,requestSignal),child);
        if(!current())throw Error('SAVE_EDITS_SCOPE_CHANGED');
        let body:SaveEditsBody|null=null;
        if(!pending){
          freezeSaveEditsBody({source_revision:Number(collection.revision),scope:'selected',selected_item_ids:[itemId],overrides_id:version,output_name:`${collection.name} — edited`},itemId);
          const saved=await saveCurrent(child);child.throwIfAborted();if(!current())throw Error('SAVE_EDITS_SCOPE_CHANGED');
          const override=await api.createOverride(saved,child);child.throwIfAborted();if(!current())throw Error('SAVE_EDITS_SCOPE_CHANGED');
          body=freezeSaveEditsBody({source_revision:Number(collection.revision),scope:'selected',selected_item_ids:[itemId],overrides_id:override.id,output_name:`${collection.name} — edited`},itemId);
        }
        return store.submit(body,(frozen,key,requestSignal)=>api.submitSaveEdits(collection.id,frozen,key,requestSignal),child);
      },15000);
      if(current()&&accepted)router.push({pathname:"/runs/[id]",params:{id:accepted.run_id,tenant}});
    }catch{if(current())setError("The saved request has been kept. Retry to recover the same collection without creating another request.")}finally{if(current())setBusy(false)}
  }

  function resolveConflict(choice:"local"|"server"){
    if(!conflict||!editorState.current)return;
    try{
      editorState.current=resolveEditorDraftConflict(editorState.current,conflict,choice);
      cacheSnapshot(conflict,editorState.current);setDraft(conflict);if(choice==="server")setJSON(JSON.stringify(conflict.params,null,2));
      setConflict(undefined);setMask(undefined);setPreviewImages([]);setError("");
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

  if(accessLost)return <SafeAreaView style={s.root}><Text accessibilityRole="alert" style={s.error}>{__uiT('ui_2f7d3df1368d')}</Text></SafeAreaView>;
  return <SafeAreaView style={s.root}><ScrollView contentContainerStyle={s.header}>
    <Text style={s.title}>{__uiT('ui_936a161c3931')}</Text>
    <Text style={s.subtitle}>{__uiT('ui_987b7559583d')}{' '}{version?.slice(0,8)} {__uiT('ui_c281c642339e')}{' '}{draft?.revision??"…"}</Text>
    {sourceURL&&previewURLs[0]?<CompareCanvas before={sourceURL} after={previewURLs[0]}/>:null}
    {(previewURLs.length?previewURLs:sourceURL?[sourceURL]:[]).map((url,index)=><MaskCanvas key={url} url={url} index={index} aspect={imageAspect} viewport={viewport} tool={maskTool} pointLabel={pointLabel} points={points} strokes={strokes} polygons={polygons} pendingPolygon={pendingPolygon} onLayout={measureCanvas} onImageSize={(width,height)=>setImageAspect(width/height)} onPoint={addPoint} onStrokeStart={beginStroke} onStrokeMove={continueStroke}/>)}
    {sourceURL||previewURLs.length?<><Text style={s.subtitle}>{__uiT('ui_ab47d7dd2d87')}{' '}{Math.round(viewport.scale*100)}%</Text><View style={{flexDirection:"row",gap:8}}><Button accessibilityRole="button" onPress={()=>zoom(Math.max(1,viewport.scale/1.25))} style={[s.button,{flex:1}]} mode="outlined" labelStyle={s.buttonText}>−</Button><Button accessibilityRole="button" onPress={()=>zoom(Math.min(16,viewport.scale*1.25))} style={[s.button,{flex:1}]} mode="outlined" labelStyle={s.buttonText}>+</Button><Button accessibilityRole="button" onPress={()=>{if(editorState.current)editorState.current=setEditorViewport(editorState.current,{scale:1,offsetX:0,offsetY:0});setViewport({scale:1,offsetX:0,offsetY:0})}} style={[s.button,{flex:2}]} mode="outlined" labelStyle={s.buttonText}>{__uiT('ui_ce89ae822e8b')}</Button></View><View style={{flexDirection:"row",gap:8}}>{([[-25,0,"←"],[0,-25,"↑"],[0,25,"↓"],[25,0,"→"]]as const).map(([dx,dy,label])=><Button accessibilityRole="button" key={label} onPress={()=>{if(!editorState.current)return;const next={...viewport,offsetX:viewport.offsetX+dx,offsetY:viewport.offsetY+dy};editorState.current=setEditorViewport(editorState.current,next);setViewport(next)}} style={[s.button,{flex:1}]} mode="outlined" labelStyle={s.buttonText}>{label}</Button>)}</View></>:null}
    <EditorPanels params={(()=>{try{return JSON.parse(json)as Record<string,unknown>}catch{return{}}})()} capabilities={capabilities} extendedApply onChange={value=>editRecipe(JSON.stringify(value,null,2))} onError={setError}/>
    <Text style={s.name}>{__uiT('ui_4e90d955baa3')}</Text><TextInput accessibilityLabel={__uiT('ui_e16c07055c36')} multiline value={json} onChangeText={editRecipe} autoCapitalize="none" style={[s.input,{minHeight:260,textAlignVertical:"top"}]}/>
    <View style={{flexDirection:"row",gap:8}}><Button accessibilityRole="button" disabled={busy||!editorState.current||editorState.current.historyIndex===0} onPress={()=>navigateHistory("undo")} style={[s.button,{flex:1}]} mode="outlined" labelStyle={s.buttonText}>{__uiT('ui_50a06538a94f')}</Button><Button accessibilityRole="button" disabled={busy||!editorState.current||editorState.current.historyIndex>=editorState.current.history.length-1} onPress={()=>navigateHistory("redo")} style={[s.button,{flex:1}]} mode="outlined" labelStyle={s.buttonText}>{__uiT('ui_958e6a646c36')}</Button></View>
    <Button accessibilityRole="button" disabled={busy||!draft} onPress={()=>void save()}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_3de100106dd4')}</Button>
    {v2Styles.length?<><Text style={s.name}>{__uiT('ui_7837bd5d1d07')}</Text>{v2Styles.map(style=><Button accessibilityRole="button" key={style.id} disabled={busy} onPress={()=>void loadStyleVersions(style.id)}    mode="contained" contentStyle={{minHeight:48}}>{style.name}</Button>)}{v2Versions.filter(value=>value.approval_status==="accepted"&&!!value.export_artifact_id).map(value=><Button accessibilityRole="button" key={value.id} disabled={busy} onPress={()=>void createV2Draft(value)}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_625c8926ea4f')}{' '}{value.version_no}</Button>)}{v2DraftStatus?<Text style={s.subtitle}>{__r2Message(v2DraftStatus,false)}</Text>:null}</>:null}
    <Button accessibilityRole="button" disabled={busy||!editorState.current||!!layerAttempt.current} onPress={()=>void loadLayers()}    mode="contained" contentStyle={{minHeight:48}}>{t("layers_load")}</Button>
    {pendingRender?<View><Text style={s.meta}>{t("render_pending_restore")}</Text><Button accessibilityRole="button" disabled={busy} onPress={()=>void retryPendingRender()}    mode="contained" contentStyle={{minHeight:48}}>{t("render_pending_retry")}</Button><Button accessibilityRole="button" disabled={busy} onPress={()=>setForgetRender(pendingRender)}    mode="contained" contentStyle={{minHeight:48}}>{t("render_pending_forget")}</Button>{forgetRender===pendingRender?<View><Text style={s.meta}>{t("render_pending_forget_warning")}</Text><Button accessibilityRole="button" disabled={busy} onPress={()=>void forgetPendingRender()}    mode="contained" contentStyle={{minHeight:48}}>{t("render_pending_forget_confirm")}</Button><Button accessibilityRole="button" disabled={busy} onPress={()=>setForgetRender(null)}    mode="contained" contentStyle={{minHeight:48}}>{t("render_pending_keep")}</Button></View>:null}</View>:null}
    {renderTicket.current&&!(layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current)?<View>
      <Text style={s.meta}>{t("render_recovered")}</Text><Text style={s.meta}>{__r2Message(renderStatus,false)}</Text>
      <Button accessibilityRole="button" disabled={busy} onPress={()=>void refreshRender()}    mode="contained" contentStyle={{minHeight:48}}>{t("render_refresh_status")}</Button>
      <Button accessibilityRole="button" disabled={busy} onPress={()=>void showRenderResults()}    mode="contained" contentStyle={{minHeight:48}}>{t("render_show_results")}</Button>
      <Button accessibilityRole="button" disabled={busy||!!renderObservation.current&&['completed','completed_with_errors','failed','cancelled','cancelling'].includes(renderObservation.current.status)} onPress={()=>void cancelRender()}    mode="contained" contentStyle={{minHeight:48}}>{t("render_cancel")}</Button>
      {renderResultsError?<Text accessibilityRole="alert" style={s.meta}>{t(renderResultsError)}</Text>:null}
      {renderResults.map(frame=><View key={frame.run}><Text style={s.meta}>{t("render_frame")}: {renderStateLabel(frame.status,t)}</Text>{frame.images.length===0?<Text style={s.meta}>{renderPresentationLabel(frame,t)}</Text>:frame.images.map(image=><Image key={image.artifact_id} accessibilityLabel={t("render_image_accessibility")} source={{uri:image.url}} resizeMode="contain" style={{width:"100%",aspectRatio:1}}/>)}</View>)}
    </View>:null}
    {busy&&(legacyMutationLock.current||layerRead.current||generationFlight.current)?<Button accessibilityRole="button" onPress={()=>stopWaiting()}    mode="contained" contentStyle={{minHeight:48}}>{t("editor_stop_waiting")}</Button>:null}
    <Button accessibilityRole="button" disabled={busy||!!pendingRender||!!layerAttempt.current||!effectsAttempt&&!bridgePending&&!operationReady("render.compose")} onPress={()=>void runLegacyMutation(fullRecipePreview)}    mode="contained" contentStyle={{minHeight:48}}>{effectsAttempt?__uiT('ui_afe62c8ea2c4'):bridgePending?__uiT('ui_26032a41fe65'):__uiT('ui_86fc1a3e2d24')}</Button>
    {effectsAttempt&&!renderTicket.current&&!(layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current)?<View>
      <Text style={s.meta}>{__r2Message(renderStatus,false)}</Text>
      {renderResultsError?<Text accessibilityRole="alert" style={s.meta}>{t(renderResultsError)}</Text>:null}
      {renderResults.map(frame=><View key={frame.run}><Text style={s.meta}>{t("render_frame")}: {renderStateLabel(frame.status,t)}</Text>{frame.images.length===0?<Text style={s.meta}>{renderPresentationLabel(frame,t)}</Text>:frame.images.map(image=><Image key={image.artifact_id} accessibilityLabel={t("render_image_accessibility")} source={{uri:image.url}} resizeMode="contain" style={{width:"100%",aspectRatio:1}}/>)}</View>)}
    </View>:null}
    {effectsTerminal?<Button accessibilityRole="button" disabled={busy} onPress={()=>void acknowledgeEffects()}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_7b4c9c207e8a')}</Button>:null}
    {effectsReviewRun?<Button accessibilityRole="button" disabled={busy||accessLost} onPress={()=>router.push({pathname:"/runs/[id]",params:{tenant:tenant||"",id:effectsReviewRun}})}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_807613363edd')}</Button>:null}
    {bridgeRejected?<Button accessibilityRole="button" disabled={busy||!!pendingRender} onPress={()=>void discardRejectedBridge()}    mode="contained" contentStyle={{minHeight:48}}>{bridgeDiscardConfirm?__uiT('ui_1d210ca31726'):__uiT('ui_8923c7db2188')}</Button>:null}
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current?<><Text style={s.meta}>{t("layers_local_only")}</Text><Button accessibilityRole="button" disabled={busy} onPress={()=>void saveLayers()}    mode="contained" contentStyle={{minHeight:48}}>{t(layerAttempt.current?"layers_retry_save":"layers_save")}</Button><Text style={s.meta}>{__r2Message(layerSaveStatus,false)}</Text><Button accessibilityRole="button" disabled={busy||!!layerAttempt.current} onPress={()=>void freezeSavedV2()}    mode="contained" contentStyle={{minHeight:48}}>{t("render_prepare_saved")}</Button><Text style={s.meta}>{__r2Message(freezeStatus,false)}</Text><Button accessibilityRole="button" disabled={busy||!!layerAttempt.current||!frozenRenderSnapshot.current||!!pendingRender||!operationReady("render.compose")} onPress={()=>void renderSavedV2()}    mode="contained" contentStyle={{minHeight:48}}>{t("render_process_saved")}</Button><Text style={s.meta}>{__r2Message(renderStatus,false)}</Text><Button accessibilityRole="button" disabled={busy||!renderTicket.current||!!renderObservation.current&&['completed','completed_with_errors','failed','cancelled','cancelling'].includes(renderObservation.current.status)} onPress={()=>void cancelRender()}    mode="contained" contentStyle={{minHeight:48}}>{t("render_cancel")}</Button><Button accessibilityRole="button" disabled={busy||!renderTicket.current} onPress={()=>void refreshRender()}    mode="contained" contentStyle={{minHeight:48}}>{t("render_refresh_status")}</Button><Button accessibilityRole="button" disabled={busy||!renderTicket.current} onPress={()=>void showRenderResults()}    mode="contained" contentStyle={{minHeight:48}}>{t("render_show_results")}</Button>{renderResultsError?<Text accessibilityRole="alert" style={s.meta}>{t(renderResultsError)}</Text>:null}{renderResults.map(frame=><View key={frame.run}><Text style={s.meta}>{t("render_frame")}: {renderStateLabel(frame.status,t)}</Text>{frame.images.length===0?<Text style={s.meta}>{renderPresentationLabel(frame,t)}</Text>:frame.images.map(image=><Image key={image.artifact_id} accessibilityLabel={t("render_image_accessibility")} source={{uri:image.url}} resizeMode="contain" style={{width:"100%",aspectRatio:1}}/>)}</View>)}<EditorLayerPanel state={editorState.current} binding={layerContext.loaded.binding} disabled={busy||!!layerAttempt.current} onChange={state=>{setLayerSaveStatus("");editorState.current=state;refreshLayerPanel(value=>value+1);if(draft)cacheSnapshot(draft,state);}}/></>:null}
    <View style={{flexDirection:"row",gap:8}}><Button accessibilityRole="button" disabled={busy||!draft||!operationReady("editor.preview")} onPress={()=>void preview("proxy")} style={[s.button,{flex:1}]} mode="outlined" labelStyle={s.buttonText}>{__uiT('ui_e14ea211e950')}</Button><Button accessibilityRole="button" disabled={busy||!draft||!operationReady("editor.preview")} onPress={()=>void preview("full")} style={[s.button,{flex:1}]} mode="outlined" labelStyle={s.buttonText}>{__uiT('ui_01c7b30ae4e6')}</Button></View>
    <View style={{flexDirection:"row",gap:8}}><Button accessibilityRole="button" disabled={!operationReady("mask.prompt")} onPress={()=>setMaskTool("prompt")} style={[s.button,{flex:1,opacity:maskTool==="prompt"?1:.55}]} mode="outlined" labelStyle={s.buttonText}>{__uiT('ui_5fd07da742b0')}</Button><Button accessibilityRole="button" disabled={!operationReady("mask.rasterize")} onPress={()=>setMaskTool("brush")} style={[s.button,{flex:1,opacity:maskTool==="brush"?1:.55}]} mode="outlined" labelStyle={s.buttonText}>{__uiT('ui_8624ca0fcb3c')}</Button><Button accessibilityRole="button" disabled={!operationReady("mask.rasterize")} onPress={()=>setMaskTool("polygon")} style={[s.button,{flex:1,opacity:maskTool==="polygon"?1:.55}]} mode="outlined" labelStyle={s.buttonText}>{__uiT('ui_0ef705dd99da')}</Button></View>
    {!operationReady(maskTool==="prompt"?"mask.prompt":"mask.rasterize")?<Text style={s.error}>{t("editor_mask_unavailable")}</Text>:null}
    {maskTool==="prompt"?<><Text style={s.subtitle}>{__uiT('ui_5ba166b0c871')}{points.length}/128)</Text>
    <View style={{flexDirection:"row",gap:8}}><TouchableRipple accessibilityRole="button" accessibilityState={{selected:pointLabel==="positive"}} onPress={()=>setPointLabel("positive")} style={[s.button,{flex:1,opacity:pointLabel==="positive"?1:.55}]}><View style={{alignItems:'center',justifyContent:'center'}}><Text style={s.buttonText}>{__uiT('ui_7285576bdacf')}</Text></View></TouchableRipple><TouchableRipple accessibilityRole="button" accessibilityState={{selected:pointLabel==="negative"}} onPress={()=>setPointLabel("negative")} style={[s.button,{flex:1,opacity:pointLabel==="negative"?1:.55}]}><View style={{alignItems:'center',justifyContent:'center'}}><Text style={s.buttonText}>{__uiT('ui_5b76f62e1ba0')}</Text></View></TouchableRipple></View>
    <View style={{flexDirection:"row",gap:8}}><Button accessibilityRole="button" disabled={busy||points.length===0} onPress={()=>{setPoints(current=>current.slice(0,-1));setMask(undefined)}} style={[s.button,{flex:1}]} mode="outlined" labelStyle={s.buttonText}>{__uiT('ui_ff8cf43b8234')}</Button><Button accessibilityRole="button" disabled={busy||points.length===0} onPress={()=>{setPoints([]);setMask(undefined)}} style={[s.button,{flex:1}]} mode="outlined" labelStyle={s.buttonText}>{__uiT('ui_9ca61c88039c')}</Button></View>
    <Button accessibilityRole="button" disabled={busy||!draft||points.length===0||!operationAvailable(capabilities,"mask.prompt")} onPress={()=>void makeMask()}    mode="contained" contentStyle={{minHeight:48}}>{mask?__uiT('ui_6b057d4b7629'):__uiT('ui_9e70377c5ba8')}</Button></>:<><Text style={s.subtitle}>{__uiT('ui_c2cf4a46cd80')}{' '}{maskTool==="brush"?__uiT('ui_fedabd4612ed'):__uiT('ui_984578a25336')} ({strokes.length+polygons.length}{__uiT('ui_d012e7775ecf')}</Text><View style={{flexDirection:"row",gap:8}}>{(["add","subtract","protect"] as const).map(mode=><Button accessibilityRole="button" key={mode} onPress={()=>setStrokeMode(mode)} style={[s.button,{flex:1,opacity:strokeMode===mode?1:.55}]} mode="outlined" labelStyle={s.buttonText}>{mode}</Button>)}</View>{maskTool==="polygon"?<View style={{flexDirection:"row",gap:8}}><Button accessibilityRole="button" disabled={pendingPolygon.length<3} onPress={finishPolygon} style={[s.button,{flex:1}]} mode="outlined" labelStyle={s.buttonText}>{__uiT('ui_769a341ca207')}{pendingPolygon.length})</Button><Button accessibilityRole="button" disabled={!pendingPolygon.length} onPress={()=>setPendingPolygon(current=>current.slice(0,-1))} style={[s.button,{flex:1}]} mode="outlined" labelStyle={s.buttonText}>{__uiT('ui_01d4a4ee41cc')}</Button></View>:null}<View style={{flexDirection:"row",gap:8}}><Button accessibilityRole="button" disabled={busy||strokes.length+polygons.length===0} onPress={undoShape} style={[s.button,{flex:1}]} mode="outlined" labelStyle={s.buttonText}>{__uiT('ui_efab23a7e768')}</Button><Button accessibilityRole="button" disabled={busy||strokes.length+polygons.length===0} onPress={()=>{setStrokes([]);setPolygons([]);setPendingPolygon([]);setMask(undefined)}} style={[s.button,{flex:1}]} mode="outlined" labelStyle={s.buttonText}>{__uiT('ui_58b3dfc532dc')}</Button></View><Button accessibilityRole="button" disabled={busy||!draft||strokes.length+polygons.length===0||!operationAvailable(capabilities,"mask.rasterize")} onPress={()=>void makeBrushMask()}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_bd4e1e6c95c3')}</Button></>}
    {protectedOutput?<Button accessibilityRole="button" disabled={busy||!!layerAttempt.current} onPress={()=>void bindProtection()}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_6d9ffcc03327')}</Button>:null}
    {protectionStatus?<Text style={s.subtitle}>{__r2Message(protectionStatus,false)}</Text>:null}
    <TextInput accessibilityLabel={__uiT('ui_d3e4e4494791')} value={prompt} onChangeText={setPrompt} placeholder={__uiT('ui_de1041b8477f')} style={s.input}/>
    <TextInput accessibilityLabel={__uiT('ui_f6642601ff04')} value={seed} onChangeText={setSeed} keyboardType="number-pad" style={s.input}/>
    {!operationReady("image.generate")?<Text style={s.error}>{t("editor_generation_unavailable")}</Text>:null}
    {generationFailed?<View><Text accessibilityRole="alert">{__uiT('ui_a6b0f8c1782c')}</Text><Button accessibilityRole="button" onPress={()=>setGenerationBoot(value=>value+1)} mode="outlined">{__uiT('ui_7f6bb896af1d')}</Button></View>:null}
    {generationPending&&!generationAttempt.current?.accepted?<Button accessibilityRole="button" disabled={busy||!generationActive||!generationReady} onPress={()=>void abandonPendingGeneration()} mode="outlined">{__uiT('ui_275e07ff4152')}</Button>:null}
    {generationTerminal?<Button accessibilityRole="button" disabled={busy||!generationActive||!generationReady} onPress={()=>void acknowledgeGenerationFailure()} mode="outlined">{__uiT('ui_aa7caeb5fce1')}</Button>:null}
    <Button accessibilityRole="button" accessibilityHint={__uiT('ui_2e5d9769aebf')} disabled={busy||!generationActive||!generationReady||!generationPending&&(!draft||!mask||!prompt.trim())||!generationPending&&!operationReady("image.generate")} onPress={()=>void generate()}    mode="contained" contentStyle={{minHeight:48}}>{generationPending?__uiT('ui_93313309eec0'):__uiT('ui_4919a220d4d9')}</Button>
    <Button accessibilityRole="button" disabled={busy||!draft||!collection||!itemId} onPress={()=>void saveEdits()}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_ab57bf684f71')}</Button>
    {saveEditsStore.current?.api===api&&saveEditsStore.current.store.pending()?(()=>{const displayed={api,store:saveEditsStore.current!.store,key:saveEditsStore.current!.store.identityKey()!};return <Button accessibilityRole="button" disabled={busy} onPress={()=>void runLegacyMutation(()=>performSaveEdits(false,true,displayed))}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_60ac52d6ca43')}</Button>})():null}
    {saveEditsStore.current?.api===api&&saveEditsStore.current.store.accepted()?(()=>{const displayed={api,store:saveEditsStore.current!.store,key:saveEditsStore.current!.store.identityKey()!};return <Button accessibilityRole="button" disabled={busy} onPress={()=>void runLegacyMutation(()=>performSaveEdits(true,false,displayed))}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_86df8fc903d8')}</Button>})():null}
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current?<EditorNegative operationAvailable={()=>operationAvailable(capabilityState.current,"optical.negative")} api={api} origin={auth.origin} tenant={tenant||""} project={layerContext.loaded.draft.data.project_id} asset={asset||""} version={version||""} disabled={busy||!!conflict||!!layerAttempt.current} onSubmitted={ticket=>router.push({pathname:"/runs/[id]",params:{tenant:tenant||"",id:ticket.run_id}})}/>:null}
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current?<EditorPerspective operationAvailable={operation=>operationAvailable(capabilityState.current,operation)} detectionAvailable={operationReady("optical.perspective_lines")} selection={collection&&itemId&&asset&&version?{collection:collection.id,item:itemId,asset,revision:collection.revision,expectedVersion:version}:null} url={sourceURL} aspect={imageAspect} api={api} origin={auth.origin} tenant={tenant||""} project={layerContext.loaded.draft.data.project_id} asset={asset||""} version={version||""} disabled={busy||!!conflict||!!layerAttempt.current} onApplied={result=>router.replace({pathname:"/editor/[version]",params:{tenant:tenant||"",asset:asset||"",version:result.asset_version_id}})} onSubmitted={ticket=>router.push({pathname:"/runs/[id]",params:{tenant:tenant||"",id:ticket.run_id}})}/>:null}
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current&&frozenRenderSnapshot.current?<Button accessibilityRole="button" disabled={busy||!!conflict||!!layerAttempt.current} onPress={prepareInterchangeExport}    mode="contained" contentStyle={{minHeight:48}}>{t("interchange_editor_export_bundle")}</Button>:null}
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current?<EditorDelivery api={api} tenant={tenant||""} project={layerContext.loaded.draft.data.project_id} asset={asset||""} version={version||""} disabled={busy||!!conflict||!!layerAttempt.current} onPrepared={snapshot=>router.push({pathname:"/deliveries/new",params:{tenant:tenant||"",project:layerContext.loaded.draft.data.project_id,snapshot}})}/>:null}
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current?<EditorRelight operationAvailable={operation=>operationAvailable(capabilityState.current,operation)} depthAvailable={operationAvailable(capabilities,"depth.predict")} personMaskAvailable={operationAvailable(capabilities,"mask.person")} api={api} origin={auth.origin} tenant={tenant||""} project={layerContext.loaded.draft.data.project_id} asset={asset||""} version={version||""} disabled={busy||!!conflict||!!layerAttempt.current} capabilityAvailable={operationReady("image.relight")} onSubmitted={ticket=>router.push({pathname:"/runs/[id]",params:{tenant:tenant||"",id:ticket.run_id,relightWorkflow:ticket.workflow_id,relightAsset:asset||"",relightVersion:version||""}})}/>:null}
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current?<EditorGlareDonor operationAvailable={()=>operationAvailable(capabilityState.current,"portrait.glare")} api={api} origin={auth.origin} tenant={tenant||""} project={layerContext.loaded.draft.data.project_id} asset={asset||""} version={version||""} collection={collection?.id} disabled={busy||!!conflict||!!layerAttempt.current} onSubmitted={ticket=>router.push({pathname:"/runs/[id]",params:{tenant:tenant||"",id:ticket.run_id}})}/>:null}
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current?<EditorOpenEyes operationAvailable={()=>operationAvailable(capabilityState.current,"portrait.open_eyes")} api={api} origin={auth.origin} tenant={tenant||""} project={layerContext.loaded.draft.data.project_id} asset={asset||""} version={version||""} collection={collection?.id} disabled={busy||!!conflict||!!layerAttempt.current} onSubmitted={ticket=>router.push({pathname:"/runs/[id]",params:{tenant:tenant||"",id:ticket.run_id}})}/>:null}
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current&&!operationReady("portrait.open_eyes")?<Text style={s.meta}>{t("editor_open_eyes_unavailable")}</Text>:null}
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current?<EditorReflection operationAvailable={()=>operationAvailable(capabilityState.current,"image.reflection")} api={api} origin={auth.origin} tenant={tenant||""} project={layerContext.loaded.draft.data.project_id} asset={asset||""} version={version||""} disabled={busy||!!conflict||!!layerAttempt.current} onSubmitted={ticket=>router.push({pathname:"/runs/[id]",params:{tenant:tenant||"",id:ticket.run_id}})}/>:null}
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current?<EditorLocalPortrait operationAvailable={operation=>operationAvailable(capabilityState.current,operation)} api={api} origin={auth.origin} tenant={tenant||""} project={layerContext.loaded.draft.data.project_id} asset={asset||""} version={version||""} hairAvailable={operationReady("portrait.hair")} clothingAvailable={operationReady("portrait.clothing")} disabled={busy||!!conflict||!!layerAttempt.current} onSubmitted={ticket=>router.push({pathname:"/runs/[id]",params:{tenant:tenant||"",id:ticket.run_id,localPortraitWorkflow:ticket.workflow_id}})}/>:null}
    {collection&&draft?<PersistentSemanticCreate kindAvailable={semanticAvailability.kindAvailable} origin={auth.origin} api={api} tenant={tenant||""} asset={asset||""} version={version||""} mask={mask?semanticMask:undefined} collection={collection} disabled={busy||!!conflict} onSubmitted={ticket=>router.push({pathname:"/runs/[id]",params:{tenant:tenant||"",id:ticket.run_id,semanticWorkflow:ticket.workflow_id}})}/>:null}
    {conflict?<EditorDraftConflict localRecipe={json} localMasks={editorState.current?currentEditorSnapshot(editorState.current).maskRefs:[]} server={conflict} busy={busy} onResolve={resolveConflict}/>:null}
    {catalogError?<Text accessibilityRole="alert" style={s.error}>{__uiMessage(catalogError,true)}</Text>:null}
    {error?<Text accessibilityRole="alert" style={s.error}>{__uiMessage(error,true)}</Text>:null}
  </ScrollView></SafeAreaView>;
}

// Foreground retains durable generation reconciliation; source/session switches
// discard every in-memory source-bound mask, prompt, history and receipt view.
export default function Editor(){
 const query=useLocalSearchParams(),auth=useAuth();
 const generation=typeof auth.sessionGeneration==='function'?auth.sessionGeneration():0;
 const identity=useMemo(()=>crypto.randomUUID(),[auth]);
 return <EditorScope key={JSON.stringify([identity,generation,auth.origin,query.tenant,query.asset,query.version])}/>;
}
