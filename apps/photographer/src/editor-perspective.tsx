import {perspectiveDetectionJournalKey,recoverPerspectiveDetectionSnapshot} from './perspective-detection-submit';
import {PerspectiveApplyRecovery} from './perspective-apply-recovery';
import type {PerspectiveApplyRecord} from './perspective-apply-attempt';
import type {PerspectiveApplied} from './perspective-apply';
import {PerspectiveQualityView} from './perspective-quality-view';
import {PerspectivePreviewView} from './perspective-preview-view';
import {readPerspectiveAttemptResult,type PerspectiveResultObservation} from './perspective-result';
import {EditorPerspectiveLines} from './editor-perspective-lines';
import {PerspectiveCanvas} from './perspective-canvas';
import {prepareNextPerspective} from './perspective-next';
import {useEffect,useMemo,useRef,useState} from 'react';
import {AppState,Pressable,Text,TextInput,View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import type {OwnerAPI} from './owner-api';
import {PerspectiveAttempt} from './perspective-attempt';
import {PerspectiveAttemptStore} from './perspective-attempt-store';
import {freezePerspectiveIntent,type PerspectiveIntent} from './perspective-intent';
import type {RegionJobTicket} from './region-preview';
import {useLocale} from './locale-context';
type Props={detectedProducer?:string;url?:string;aspect?:number;api:OwnerAPI;origin:string;tenant:string;project:string;asset:string;version:string;selection?:PerspectiveApplyRecord['selection'];disabled?:boolean;onApplied:(value:PerspectiveApplied)=>void;onSubmitted:(ticket:RegionJobTicket)=>void};
export function EditorPerspective(props:Props){
 const scope=useMemo(()=>crypto.randomUUID(),[props.api,props.origin,props.tenant,props.project,props.asset,props.version]);
 const [generation,setGeneration]=useState(0),[foreground,setForeground]=useState((AppState?.currentState??'active')==='active');
 useEffect(()=>{if(!AppState?.addEventListener)return;const listener=AppState.addEventListener('change',state=>{setForeground(state==='active');if(state==='active')setGeneration(v=>v+1);});return()=>listener.remove();},[]);
 return foreground?<Scope key={`${scope}:${generation}`} {...props}/>:null;
}
function Scope(props:Props){
 const {t}=useLocale();
 const [body,setBody]=useState<PerspectiveIntent|null>(null),[attempt,setAttempt]=useState<PerspectiveAttempt|null>(null),[ticket,setTicket]=useState<RegionJobTicket|null>(null),[store,setStore]=useState<PerspectiveAttemptStore|null>(null),[busy,setBusy]=useState(true),[message,setMessage]=useState('');
 const [linesPending,setLinesPending]=useState(true);
 const [result,setResult]=useState<PerspectiveResultObservation|null>(null);
 const live=useRef(true),flight=useRef<AbortController|null>(null),locked=useRef(false);
 useEffect(()=>{
  const controller=new AbortController();flight.current=controller;live.current=true;
  const active=()=>live.current&&!controller.signal.aborted;
  void(async()=>{try{
   const user=await props.api.me(controller.signal);if(!active())return;
   const journal=new PerspectiveAttemptStore(AsyncStorage,props.origin,user.id,props.tenant,props.project,props.version);
   const saved=await journal.read();if(!active())return;setStore(journal);
   if(saved){setBody(saved.body);setAttempt(PerspectiveAttempt.restore(saved,journal));setTicket(saved.ticket);return;}
   const base=await props.api.v2Binding(props.asset,controller.signal);if(!active())return;
   if(base.asset_version_id!==props.version)throw new Error('PERSPECTIVE_SOURCE_CHANGED');
   const detectionKey=perspectiveDetectionJournalKey(props.origin,user.id,props.tenant,props.project,props.version);
   const recoveredSnapshot=await recoverPerspectiveDetectionSnapshot(AsyncStorage,detectionKey,props.tenant,props.project,base,controller.signal);if(!active())return;
   const snapshot=recoveredSnapshot?{id:recoveredSnapshot}:await props.api.createRenderInputSnapshot(props.project,{sources:[base]},crypto.randomUUID(),controller.signal);if(!active())return;
   setBody(freezePerspectiveIntent({schema_version:1,snapshot_id:snapshot.id,params:{schema_version:1,base,mode:'manual',strength:1,corners:[{x:0,y:0},{x:1,y:0},{x:1,y:1},{x:0,y:1}],lines:null,max_crop_fraction:.5}}));
  }catch{if(active())setMessage(t('perspective_unavailable'));}finally{if(active())setBusy(false);}})();
  return()=>{live.current=false;controller.abort();flight.current?.abort();};
 },[]);
 const canEdit=!!body&&!attempt&&!linesPending&&!busy&&!props.disabled;
 function numeric(text:string,change:(body:PerspectiveIntent,value:number)=>PerspectiveIntent){
  if(!canEdit||!body)return;
  if(text.trim()===''||!Number.isFinite(Number(text))||Number(text)<0||Number(text)>1){setMessage(t('perspective_invalid'));return;}
  setMessage('');setBody(change(body,Number(text)));
 }
 async function submit(){
  if(!body||!store||locked.current||props.disabled||!attempt&&linesPending)return;
  locked.current=true;setBusy(true);setMessage('');const controller=new AbortController();flight.current=controller;let staged=!!attempt;
  try{
   const current=attempt??new PerspectiveAttempt(props.project,crypto.randomUUID(),freezePerspectiveIntent(body),store);
   setAttempt(current);staged=true;
   const accepted=await current.submit((project,request,key,signal)=>props.api.submitPerspective(project,request,key,signal),controller.signal);
   if(!live.current||controller.signal.aborted)return;
   // A cached acknowledgement must still belong to a currently authorized workflow.
   const status=await props.api.get<{run_id:string;workflow_id:string}>(`/api/v1/tenants/{t}/v2-workflows/${encodeURIComponent(accepted.workflow_id)}`,controller.signal);
   if(status.run_id!==accepted.run_id||status.workflow_id!==accepted.workflow_id)throw new Error('PERSPECTIVE_TICKET_SCOPE_CHANGED');
   if(!live.current||controller.signal.aborted)return;
   setTicket(accepted);props.onSubmitted(accepted);
  }catch{if(live.current&&!controller.signal.aborted)setMessage(t(staged?'perspective_unknown':'perspective_invalid'));}
  finally{locked.current=false;if(live.current)setBusy(false);}
 }
 async function refreshResult(){
  if(!ticket||!body||locked.current||props.disabled)return;
  locked.current=true;setBusy(true);setResult(null);setMessage('');const controller=new AbortController();flight.current=controller;
  try{const observation=await readPerspectiveAttemptResult(props.api,props.tenant,props.project,body,ticket,controller.signal);
   if(live.current&&!controller.signal.aborted)setResult(observation);
  }catch{if(live.current&&!controller.signal.aborted)setMessage(t('perspective_result_unavailable'));}
  finally{locked.current=false;if(live.current)setBusy(false);}
 }
 async function another(){
  if(!ticket||!attempt||!store||locked.current||props.disabled)return;
  locked.current=true;setBusy(true);setMessage('');const controller=new AbortController();flight.current=controller;
  try{
   const next=await prepareNextPerspective(props.api,props.asset,attempt.snapshot,store,controller.signal);
   if(!live.current||controller.signal.aborted)return;
   setResult(null);setLinesPending(true);setBody(next);setAttempt(null);setTicket(null);
  }catch{if(live.current&&!controller.signal.aborted)setMessage(t('perspective_unavailable'));}
  finally{locked.current=false;if(live.current)setBusy(false);}
 }
 return <View><PerspectiveApplyRecovery api={props.api} origin={props.origin} tenant={props.tenant} project={props.project} version={props.version} disabled={props.disabled} onApplied={props.onApplied}/><Text>{t('perspective_title')}</Text><Text>{t('perspective_hint')}</Text>
 {body?.params.corners&&props.url?<PerspectiveCanvas url={props.url} aspect={props.aspect??1} corners={body.params.corners} disabled={!canEdit} label={t('perspective_corner')} onChange={corners=>{if(canEdit){setMessage('');setBody(old=>old?{...old,params:{...old.params,corners}}:old);}}}/>:null}
 {body?<><Text>{t('perspective_strength')}</Text><TextInput accessibilityLabel={t('perspective_strength')} editable={canEdit} keyboardType="decimal-pad" value={String(body.params.strength)} onChangeText={text=>numeric(text,(b,v)=>({...b,params:{...b.params,strength:v}}))}/>
 <Text>{t('perspective_crop')}</Text><TextInput accessibilityLabel={t('perspective_crop')} editable={canEdit} keyboardType="decimal-pad" value={String(body.params.max_crop_fraction)} onChangeText={text=>numeric(text,(b,v)=>({...b,params:{...b.params,max_crop_fraction:v}}))}/>
 {body.params.corners?.map((point,index)=><View key={index}><Text>{t('perspective_corner')} {index+1}</Text>{(['x','y'] as const).map(axis=><TextInput key={axis} accessibilityLabel={`${t('perspective_corner')} ${index+1} ${axis}`} editable={canEdit} keyboardType="decimal-pad" value={String(point[axis])} onChangeText={text=>numeric(text,(b,v)=>({...b,params:{...b.params,corners:b.params.corners!.map((p,i)=>i===index?{...p,[axis]:v}:p)}}))}/>)}</View>)}</>:null}
 {body&&store&&!attempt?<EditorPerspectiveLines detectedProducer={props.detectedProducer} url={props.url} aspect={props.aspect} api={props.api} origin={props.origin} tenant={props.tenant} project={props.project} version={props.version} body={body} jobs={store} disabled={busy||!!props.disabled} onPending={setLinesPending} onSnapshot={snapshot=>setBody(old=>old?freezePerspectiveIntent({...old,snapshot_id:snapshot}):old)} onPrepared={job=>{setBody(job.snapshot.body);setAttempt(job);setLinesPending(false);setMessage('');}}/>:null}
 {message?<Text accessibilityRole="alert">{message}</Text>:null}
 {ticket?<Pressable accessibilityRole="button" disabled={busy||props.disabled} onPress={()=>void refreshResult()}><Text>{t('perspective_result_refresh')}</Text></Pressable>:null}
 {result?.state==='pending'?<Text>{t('perspective_result_pending')}</Text>:null}
 {result?.state==='available'?<View><PerspectivePreviewView api={props.api} proposal={result.proposal}/><PerspectiveQualityView origin={props.origin} selection={props.selection} api={props.api} proposal={result.proposal} disabled={props.disabled||busy} onApplied={props.onApplied}/><Text>{t('perspective_result_canvas')}: {result.proposal.canvas.width} × {result.proposal.canvas.height}</Text><Text>{t(result.proposal.requires_review?'perspective_result_review':'perspective_result_identity')}</Text></View>:null}
 {ticket?<Pressable accessibilityRole="button" disabled={busy||props.disabled} onPress={()=>void another()}><Text>{t('perspective_another')}</Text></Pressable>:null}
 {busy?<Text>{t('perspective_loading')}</Text>:null}
 <Pressable accessibilityRole="button" disabled={!body||!store||busy||props.disabled||!attempt&&linesPending||message===t('perspective_invalid')} onPress={()=>void submit()}><Text>{t(ticket?'perspective_open':attempt?'perspective_retry':'perspective_submit')}</Text></Pressable>
 </View>;
}
