import {prepareV2LayerSave,saveV2Layers,V2LayerSavedRefreshRequired} from "../../src/editor-v2-save";
import {loadEditorV2Layers} from "../../src/editor-v2-load";
import {EditorLayerPanel} from "../../src/editor-layer-panel";
import {setEditorLayerStack} from "../../src/editor-layer-commands";
import {readLayerStack} from "../../src/editor-layers";
import AsyncStorage from "@react-native-async-storage/async-storage";
import {EditorDraftCache,editorCacheRestoreMode} from "../../src/editor-draft-cache";
import {APIError} from "@photoeditor/api-client";
import {useLocale} from "../../src/locale-context";
import {useEffect,useMemo,useRef,useState} from "react";
import {useLocalSearchParams,useRouter} from "expo-router";
import {Pressable,SafeAreaView,ScrollView,Text,TextInput,View,type GestureResponderEvent,type LayoutChangeEvent} from "react-native";
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
  const [,refreshLayerPanel]=useState(0);
  const layerAttempt=useRef<ReturnType<typeof prepareV2LayerSave>|null>(null);
  const [layerSaveStatus,setLayerSaveStatus]=useState("");
  const [layerContext,setLayerContext]=useState<{scope:string;api:OwnerAPI;loaded:Awaited<ReturnType<typeof loadEditorV2Layers>>}>();
  useEffect(()=>{layerEpoch.current++;layerRead.current?.abort();layerRead.current=null;if(layerLock.current)setBusy(false);layerLock.current=false;layerAttempt.current=null;setLayerSaveStatus("");setLayerContext(undefined);return()=>{layerEpoch.current++;layerRead.current?.abort();};},[layerScope,api]);

  const draftCache=useRef<EditorDraftCache|undefined>(undefined);

  useEffect(()=>{
    if(!asset)return;
    let active=true;setConflict(undefined);setDraft(undefined);editorState.current=undefined;draftCache.current=undefined;
    void (async()=>{
      const server=await api.draft(asset);if(!active)return;
      let cached=null;
      try{
        const user=await api.me();if(!active)return;
        const store=new EditorDraftCache(AsyncStorage,auth.origin,user.id,tenant,server.asset_version_id);
        cached=await store.read();if(!active)return;draftCache.current=store;
      }catch{/* Network draft editing remains available if local storage is unavailable. */}
      if(!active)return;
      const mode=cached?editorCacheRestoreMode(cached,server):undefined;
      const base=cached&&mode!=="source_changed"?cached.base:server;
      let state=createEditorState(base);
      if(cached&&mode!=="source_changed")state=applyEditorCommand(state,cached.local);
      editorState.current=state;setDraft(base);setJSON(JSON.stringify(currentEditorSnapshot(state).params,null,2));
      if(mode==="conflict")setConflict(server);
      setError(mode==="source_changed"?t("editor_conflict_source_changed"):"");
    })().catch(()=>{if(active)setError("The editor source is unavailable or you no longer have access.")});
    return()=>{active=false};
  },[asset,api]);
  useEffect(()=>{if(collectionId)api.collection(collectionId).then(async value=>{setCollection(value);setV2Styles((await api.styles(value.project_id)).items)}).catch(()=>setError("Collection or its styles are unavailable."))},[api,collectionId]);
  useEffect(()=>{
    let active=true;setSourceURL("");setPreviewURLs([]);setMask(undefined);setProtectedOutput(undefined);
    if(asset&&version)api.mediaLinks(asset,version,["preview1600"]).then(value=>{
      if(!active)return;const source=value.items.find(item=>item.variant==="preview1600");
      if(!source)throw new Error("preview");setSourceURL(source.url);
    }).catch(()=>{if(active)setError("The source preview is unavailable.")});
    return()=>{active=false};
  },[api,asset,version]);
  useEffect(()=>{api.capabilities().then(setCapabilities).catch(()=>setError("Operation availability could not be loaded."))},[api]);
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
    if(conflict)throw new Error("EDITOR_CONFLICT_UNRESOLVED");
    if(!draft||!asset)throw new Error("draft");
    const params=JSON.parse(json) as Record<string,unknown>;
    if(params.schema_version!==1||typeof params.modules!=="object"||params.modules===null)throw new Error("schema");
    if(!editorState.current)throw new Error("state");
    const submitted=editorState.current;
    const saved=await saveDraftWithConflict(prepareEditorDraftSave(submitted,draft,params));
    if(!editorState.current)throw new Error("state");
    const accepted=acceptEditorDraftSave(editorState.current,submitted,saved);
    editorState.current=accepted.state;cacheSnapshot(saved,accepted.state);
    setDraft(saved);if(accepted.replaceRecipe)setJSON(JSON.stringify(saved.params,null,2));
    else throw new Error("EDITOR_LOCAL_EDITS_CHANGED");
    return saved;
  }
  async function save(){setBusy(true);try{await saveCurrent();setError("")}catch(error){setError(error instanceof Error&&error.message==="EDITOR_LOCAL_EDITS_CHANGED"?"The submitted revision was saved. Your newer local edits remain unsaved.":"Draft could not be saved. Check the recipe and reload if its revision changed.")}finally{setBusy(false)}}
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
    setBusy(true);setError("");
    try{const saved=await saveCurrent();if(!editorState.current)throw new Error("state");const request=beginEditorPreview(editorState.current);editorState.current=request.state;const accepted=await api.preview(saved,scale),outputs=await waitForOutputs(accepted.run_id),image=outputs.find(value=>value.role==="derivative");if(!image)throw new Error("output");if(editorState.current&&acceptsEditorPreview(editorState.current,request.token))setPreviewURLs([image.url])}
    catch{setError("Preview could not be rendered. Check the recipe and worker availability.")}finally{setBusy(false)}
  }
  async function makeMask(){
    if(!draft||!asset)return;
    if(points.length===0)return setError("Add at least one mask point on the rendered image.");
    setBusy(true);setError("");
    try{
      const saved=await saveCurrent(),accepted=await api.promptMask(saved,points);
      await persistMasks(saved,accepted.run_id,"prompt_mask_ref",value=>[value]);
    }catch{setError("Mask could not be created. A qualified MobileSAM worker is required.")}finally{setBusy(false)}
  }
  async function persistMasks(saved:EditorDraft,runId:string,role:string,select:(value:unknown)=>unknown[]){
      const submitted=editorState.current;
      if(conflict||!submitted)throw new Error("EDITOR_CONFLICT_UNRESOLVED");
      prepareEditorDraftSave(submitted,saved,saved.params);
      const outputs=await waitForOutputs(runId),ref=outputs.find(value=>value.role===role);
      if(!ref)throw new Error("mask");
      const response=await fetch(ref.url);if(!response.ok)throw new Error("mask-download");
      const values=select(await response.json()) as MaskRef[],value=values.find(item=>!item.protected);
      if(!value||values.length===0||values.some(item=>!item?.artifact?.artifact_id||!item.artifact.sha256))throw new Error("mask-contract");
      if(!editorState.current)throw new Error("state");
      // Recheck the captured source/revision before committing generated masks.
      prepareEditorDraftSave(editorState.current,saved,saved.params);
      const persisted=await saveDraftWithConflict({...saved,mask_refs:[...saved.mask_refs,...values]});
      if(!editorState.current)throw new Error("state");
      const accepted=acceptEditorDraftSave(editorState.current,submitted,persisted);
      editorState.current=accepted.state;cacheSnapshot(persisted,accepted.state);setDraft(persisted);
      if(!accepted.replaceRecipe){setConflict(persisted);throw new Error("EDITOR_LOCAL_EDITS_CHANGED")}
      setJSON(JSON.stringify(persisted.params,null,2));setMask(value);
      return outputs;
  }
  async function makeBrushMask(){
    if(!draft||!asset||strokes.length+polygons.length===0)return setError("Draw at least one stroke or polygon on the rendered image.");
    setBusy(true);setError("");
    try{setProtectedOutput(undefined);setProtectionStatus("");const saved=await saveCurrent(),accepted=await api.manualMask(saved,strokes,polygons);const outputs=await persistMasks(saved,accepted.run_id,"rasterized_mask",value=>{const result=value as{target?:unknown;protected?:unknown};return[result.target,result.protected]});setProtectedOutput(outputs.find(value=>value.role==="mask_protected_ref"))}
    catch{setError("Manual mask could not be rasterized. Check the strokes and CPU worker.")}finally{setBusy(false)}
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
  async function createV2Draft(styleVersion:StyleVersion){
    if(!asset)return;
    setBusy(true);setError("");setV2DraftStatus("");
    try{await api.createV2Draft(asset,styleVersion,protectedOutput);setV2DraftStatus(protectedOutput?"V2 draft and base checkpoint created with the protected mask.":"V2 draft created without a protected mask. For protected-mask training, rasterize the mask before creating the first draft.")}
    catch{setError("V2 draft could not be created. Use an accepted style from this project and check whether a V2 draft already exists.")}
    finally{setBusy(false)}
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
    {v2Styles.length?<><Text style={s.name}>Initial V2 draft style</Text>{v2Styles.map(style=><Pressable key={style.id} disabled={busy} onPress={()=>void api.styleVersions(style.id).then(page=>setV2Versions(page.items)).catch(()=>setError("Style versions are unavailable."))} style={s.button}><Text style={s.buttonText}>{style.name}</Text></Pressable>)}{v2Versions.filter(value=>value.approval_status==="accepted"&&!!value.export_artifact_id).map(value=><Pressable key={value.id} disabled={busy} onPress={()=>void createV2Draft(value)} style={s.button}><Text style={s.buttonText}>Create V2 draft · style version {value.version_no}</Text></Pressable>)}{v2DraftStatus?<Text style={s.subtitle}>{v2DraftStatus}</Text>:null}</>:null}
    <Pressable accessibilityRole="button" disabled={busy||!editorState.current||!!layerAttempt.current} onPress={()=>void loadLayers()} style={s.button}><Text style={s.buttonText}>{t("layers_load")}</Text></Pressable>
    {layerContext?.scope===layerScope&&layerContext.api===api&&editorState.current?<><Text style={s.meta}>{t("layers_local_only")}</Text><Pressable accessibilityRole="button" disabled={busy} onPress={()=>void saveLayers()} style={s.button}><Text style={s.buttonText}>{t(layerAttempt.current?"layers_retry_save":"layers_save")}</Text></Pressable><Text style={s.meta}>{layerSaveStatus}</Text><EditorLayerPanel state={editorState.current} binding={layerContext.loaded.binding} disabled={busy||!!layerAttempt.current} onChange={state=>{setLayerSaveStatus("");editorState.current=state;refreshLayerPanel(value=>value+1);if(draft)cacheSnapshot(draft,state);}}/></>:null}
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
    {conflict?<EditorDraftConflict localRecipe={json} localMasks={editorState.current?currentEditorSnapshot(editorState.current).maskRefs:[]} server={conflict} busy={busy} onResolve={resolveConflict}/>:null}
    {error?<Text accessibilityRole="alert" style={s.error}>{error}</Text>:null}
  </ScrollView></SafeAreaView>;
}
