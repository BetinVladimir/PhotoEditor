import {useLocale} from './locale-context';
import {Button,TouchableRipple,ActivityIndicator,Text} from 'react-native-paper';
import {readDeadline} from './read-deadline';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {useEffect,useMemo,useRef,useState} from 'react';
import {FlatList,View} from 'react-native';
import {canonicalJSON,APIError} from '@photoeditor/api-client';
import {SceneFramePreview} from './scene-frame-preview';
import {loadCullingSelection,cullingDecisionIntent,cullingDecisionReceipt,recoverCullingDecision,type CullingSelection} from './culling-selection';
import {CullingDecisionAttemptStore,type CullingDecisionAttempt} from './culling-decision-attempt-store';
import type {OwnerAPI} from './owner-api';
import type {Review} from './catalog-types';
import {catalogStyle as s} from './catalog-style';

/** The frozen input inventory is read-only; only photographer selection is sent. */
export function CullingReviewSelection({api,origin,tenant,review,onDecided,onAccessDenied}:{api:OwnerAPI;origin:string;tenant:string;review:Review;onDecided:()=>void;onAccessDenied:()=>void}){
 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;

 const [selection,setSelection]=useState<CullingSelection|null>(null),[selected,setSelected]=useState<string[]>([]),[confirmed,setConfirmed]=useState<string[]>([]);
 const [attempt,setAttempt]=useState<CullingDecisionAttempt|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState(''),[accepted,setAccepted]=useState(false),[reload,setReload]=useState(0),[filter,setFilter]=useState<'all'|'recommended'|'eyes'>('all');
 const authority=useMemo(()=>({api,origin,tenant,review:review.id,run:review.run_id,revision:review.revision,candidate:review.candidate_hash}),[api,origin,tenant,review.id,review.run_id,review.revision,review.candidate_hash]);
 const liveAuthority=useRef(authority);liveAuthority.current=authority;
 const draft=useRef({selection,selected,confirmed,attempt,accepted});draft.current={selection,selected,confirmed,attempt,accepted};
 const operation=useRef<AbortController|null>(null);
 const life=useRef<AbortController|null>(null),scope=useRef<{actor:string;store:CullingDecisionAttemptStore;authority:typeof authority}|null>(null),flight=useRef(false);
 function revoke(){operation.current?.abort();operation.current=null;life.current?.abort();scope.current=null;setSelection(null);setSelected([]);setConfirmed([]);setAttempt(null);setAccepted(false);setError('Selection access is no longer available.');onAccessDenied();}
 const access=(e:unknown)=>e instanceof APIError&&[401,403,404,410].includes(e.status)||e instanceof Error&&['CULLING_ACTOR_CHANGED','CULLING_SCOPE_CHANGED'].includes(e.message);
 useEffect(()=>{
  const controller=new AbortController();life.current=controller;scope.current=null;flight.current=false;setSelection(null);setSelected([]);setConfirmed([]);setAttempt(null);setAccepted(false);setBusy(true);setError('');
  void readDeadline(controller.signal,async signal=>{
   const actor=(await api.me(signal)).id;signal.throwIfAborted();
   const store=new CullingDecisionAttemptStore(AsyncStorage,origin,actor,tenant,review.id),pending=await store.read();signal.throwIfAborted();
   const loaded=await loadCullingSelection(api,review,signal);signal.throwIfAborted();
   if((await api.me(signal)).id!==actor)throw new Error('CULLING_ACTOR_CHANGED');signal.throwIfAborted();
   if(pending){const receipt=cullingDecisionReceipt(review,pending.body);if(!receipt&&canonicalJSON(pending.body.selection)!==canonicalJSON(loaded))throw new Error('CULLING_REVIEW_CHANGED');if(receipt){await store.clear(pending.key,receipt);signal.throwIfAborted();}else{setAttempt(pending);setSelected(pending.body.request.selected_item_ids);setConfirmed(pending.body.request.confirmed_eyes_item_ids);}}
   scope.current={actor,store,authority};setSelection(loaded);setAccepted(loaded.state==='decided');
   if(!pending||loaded.state==='decided'){setSelected(loaded.selected_item_ids??loaded.items.filter(item=>item.recommended).map(item=>item.item_id));setConfirmed(loaded.confirmed_eyes_item_ids??[]);}
  }).catch(e=>{if(!controller.signal.aborted){if(access(e))revoke();else setError('The complete selection could not be loaded. No partial selection can be submitted.');}}).finally(()=>{if(!controller.signal.aborted)setBusy(false);});
  return()=>{operation.current?.abort();operation.current=null;controller.abort();if(life.current===controller){life.current=null;scope.current=null;}};
 },[api,origin,tenant,review.id,review.run_id,review.revision,review.candidate_hash,reload]);
 async function submit(){
  const lifetime=life.current,owned=scope.current,currentDraft=draft.current;
  if(liveAuthority.current!==authority||!lifetime||lifetime.signal.aborted||!owned||owned.authority!==authority||!currentDraft.selection||flight.current||currentDraft.accepted)return;
  // Capture the detached current selection synchronously before actor/storage IO.
  const remembered=currentDraft.attempt?structuredClone(currentDraft.attempt):null;
  let fresh:CullingDecisionAttempt|null=null;
  if(!remembered){try{fresh={version:1,key:crypto.randomUUID(),body:cullingDecisionIntent(tenant,owned.actor,currentDraft.selection,[...currentDraft.selected],[...currentDraft.confirmed])};}catch{setError('Confirm the current selection and its eye warnings before submitting.');return;}}
  const controller=new AbortController();operation.current=controller;
  const abort=()=>controller.abort();lifetime.signal.addEventListener('abort',abort,{once:true});
  const owns=()=>liveAuthority.current===authority&&life.current===lifetime&&scope.current===owned&&operation.current===controller&&!lifetime.signal.aborted;
  flight.current=true;setBusy(true);setError('');
  try{
   await readDeadline(controller.signal,async signal=>{
    const current=async()=>{signal.throwIfAborted();if(!owns())throw new Error('CULLING_SCOPE_CHANGED');const user=await api.me(signal);signal.throwIfAborted();if(!owns())throw new Error('CULLING_SCOPE_CHANGED');if(user.id!==owned.actor)throw new Error('CULLING_ACTOR_CHANGED');};
    await current();const saved=await owned.store.read();signal.throwIfAborted();if(!owns())throw new Error('CULLING_SCOPE_CHANGED');
    if(saved&&remembered&&(saved.key!==remembered.key||canonicalJSON(saved.body)!==canonicalJSON(remembered.body)))throw new Error('CULLING_ATTEMPT_CONFLICT');
    const pending=saved??remembered??fresh;if(!pending)throw new Error('CULLING_ATTEMPT_REQUIRED');
    draft.current.attempt=pending;setAttempt(pending);await owned.store.write(pending);await current();
    const scopedAPI={review:async(id:string,requestSignal?:AbortSignal)=>{await current();const value=await api.review(id,requestSignal);await current();return value;},decideCulling:async(...args:Parameters<OwnerAPI['decideCulling']>)=>{await current();const value=await api.decideCulling(...args);await current();return value;}};
    const receipt=await recoverCullingDecision(scopedAPI,pending.body,pending.key,signal);await current();
    await owned.store.clear(pending.key,receipt);await current();draft.current.attempt=null;draft.current.accepted=true;setAttempt(null);setAccepted(true);onDecided();
   },15000);
  }catch(e){if(owns()&&!controller.signal.aborted){if(access(e))revoke();else setError('Selection is not yet confirmed. Retry the saved selection to reconcile the same request; originals are never deleted.');}}
  finally{lifetime.signal.removeEventListener('abort',abort);if(operation.current===controller){operation.current=null;flight.current=false;if(life.current===lifetime&&liveAuthority.current===authority)setBusy(false);}}
 }
 const displayedOperation=operation.current,displayedLife=life.current;
 function cancel(){
  if(liveAuthority.current!==authority||!displayedLife||life.current!==displayedLife||displayedLife.signal.aborted||operation.current!==displayedOperation)return;
  if(displayedOperation){displayedOperation.abort();operation.current=null;flight.current=false;}else{displayedLife.abort();scope.current=null;}
  setBusy(false);setError('Selection is unresolved. Reload or retry the saved selection; originals are never deleted.');
 }
 function editableDraft(){return liveAuthority.current===authority&&scope.current?.authority===authority&&!!life.current&&!life.current.signal.aborted&&!flight.current&&!operation.current&&!draft.current.attempt&&!draft.current.accepted?draft.current:null;}
 function toggleSelected(itemID:string){const value=editableDraft();if(!value?.selection?.items.some(item=>item.item_id===itemID))return;const chosen=value.selected.includes(itemID);value.selected=chosen?value.selected.filter(id=>id!==itemID):[...value.selected,itemID];value.confirmed=value.confirmed.filter(id=>id!==itemID);setSelected(value.selected);setConfirmed(value.confirmed);}
 function toggleEye(itemID:string){const value=editableDraft();if(!value?.selection?.items.some(item=>item.item_id===itemID&&item.eye_review)||!value.selected.includes(itemID))return;value.confirmed=value.confirmed.includes(itemID)?value.confirmed.filter(id=>id!==itemID):[...value.confirmed,itemID];setConfirmed(value.confirmed);}
 const locked=busy||attempt!==null||accepted;
 const chosen=new Set(selected),eyes=new Set(confirmed),missing=selection?.items.filter(item=>chosen.has(item.item_id)&&item.eye_review&&!eyes.has(item.item_id)).length??0;
 const visible=selection?.items.filter(item=>filter==='all'||filter==='recommended'&&item.recommended||filter==='eyes'&&item.eye_review)??[];
 const scenes=new Map<string,number>(),duplicates=new Map<string,number>(),bursts=new Map<string,number>();selection?.items.forEach(item=>{for(const [map,key] of [[scenes,item.scene_id],[duplicates,item.exact_group_id],[bursts,item.burst_group_id]] as const)if(!map.has(key))map.set(key,map.size+1)});
 return <View accessibilityLabel={__uiT('ui_1618eb0bbcff')}>
  <Text style={s.name}>{__uiT('ui_59936ac01493')}</Text><Text style={s.meta}>{__uiT('ui_132355804273')}</Text>
  {busy&&!selection?<ActivityIndicator accessibilityLabel={__uiT('ui_612ead98754f')}/>:null}
  {busy?<Button accessibilityRole="button" onPress={cancel}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_77067d3965b5')}</Button>:null}
  {error?<Text accessibilityRole="alert" style={s.error}>{__uiMessage(error,true)}</Text>:null}
  {!selection&&!busy?<Button accessibilityRole="button" onPress={()=>setReload(v=>v+1)}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_82bff82eb17c')}</Button>:null}
  {selection?<>
   <Text style={s.subtitle}>{selected.length} {__uiT('ui_28391d3bc64e')}{' '}{selection.total} {__uiT('ui_c5e03c911891')}{' '}{missing} {__uiT('ui_73ca359b174c')}</Text>
   {accepted?<Text style={s.name}>{__uiT('ui_c056db23dd5b')}</Text>:null}
   <View style={s.row}>{(['all','recommended','eyes'] as const).map(value=><Button key={value} accessibilityRole="button" onPress={()=>setFilter(value)}    mode="contained" contentStyle={{minHeight:48}}>{value==='all'?__uiT('ui_ca22cb69bbf7'):value==='recommended'?__uiT('ui_aeecbb175a27'):__uiT('ui_28d7a87c50b0')}</Button>)}</View>
   <FlatList data={visible} keyExtractor={item=>item.item_id} initialNumToRender={12} windowSize={5} renderItem={({item,index})=><View>
    <Text style={s.name}>{__uiT('ui_477e5af2fd7e')}{' '}{scenes.get(item.scene_id)} {__uiT('ui_917121e0c2c4')}{' '}{selection.items.indexOf(item)+1} {__uiT('ui_4a7616377c6c')}{' '}{duplicates.get(item.exact_group_id)} {__uiT('ui_aa049044e66e')}{' '}{bursts.get(item.burst_group_id)} {__uiT('ui_401b567380e6')}{' '}{item.rank}</Text>
    <SceneFramePreview api={api} frame={{assetId:item.asset_id,versionId:item.asset_version_id,ordinal:index}} onAccessDenied={revoke}/>
    <Text style={s.meta}>{item.recommended?__uiT('ui_2b37399ae430'):__uiT('ui_d8daa69e6b97')}{item.manual?__uiT('ui_bba86bf0992c'):''} · {item.eye_state==='closed'?__uiT('ui_786fa66b0085'):item.eye_state==='unknown'?__uiT('ui_0e6ad9faf463'):__uiT('ui_c91159c37142')}</Text>
    <TouchableRipple aria-checked={chosen.has(item.item_id)} accessibilityRole="checkbox" accessibilityState={{checked:chosen.has(item.item_id),disabled:locked}} accessibilityLabel={__uiT('ui_630cf0263ada', {p0:(selection.items.indexOf(item)+1)})} disabled={locked} onPress={()=>toggleSelected(item.item_id)} style={s.button}><View style={{alignItems:'center',justifyContent:'center'}}><Text style={s.buttonText}>{chosen.has(item.item_id)?__uiT('ui_57fd7a0cf33f'):__uiT('ui_df12aeba9bb9')}</Text></View></TouchableRipple>
    {item.eye_review&&chosen.has(item.item_id)?<TouchableRipple aria-checked={eyes.has(item.item_id)} accessibilityRole="checkbox" accessibilityState={{checked:eyes.has(item.item_id),disabled:locked}} accessibilityLabel={__uiT('ui_a678a10dc21c', {p0:(selection.items.indexOf(item)+1)})} disabled={locked} onPress={()=>toggleEye(item.item_id)} style={s.button}><View style={{alignItems:'center',justifyContent:'center'}}><Text style={s.buttonText}>{eyes.has(item.item_id)?__uiT('ui_6cd762d8a81f'):__uiT('ui_925922aba951')}</Text></View></TouchableRipple>:null}
   </View>}/>
   {!accepted?<Button accessibilityRole="button" disabled={busy||(!attempt&&(selected.length===0||missing>0))} onPress={()=>void submit()}    mode="contained" contentStyle={{minHeight:48}}>{attempt?__uiT('ui_b38a59870aeb'):__uiT('ui_87dfa6651857')}</Button>:null}
  </>:null}
 </View>;
}
