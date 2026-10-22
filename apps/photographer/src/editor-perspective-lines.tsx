import {Button,Text,TextInput} from 'react-native-paper';
import {perspectiveDetectionAPI} from './perspective-detection-api';
import {restartPerspectiveDetection,isPerspectiveDetectionDraftExpired,PerspectiveDetectionAdmissionRecovered} from './perspective-detection-restart';
import {canLoadDetectedPerspectiveLines,observePerspectiveDetection,type PerspectiveDetectionStatus} from './perspective-detection-status';
import type {PerspectiveLinesSubmissionReceipt} from './perspective-lines-submission';
import {perspectiveDetectionJournalKey,submitPerspectiveDetection,recoverPerspectiveDetectionReceipt} from './perspective-detection-submit';
import {loadDetectedPerspectiveLines} from './perspective-detected-lines-load';
import {PerspectiveLinesCanvas} from './perspective-lines-canvas';
import {useEffect,useRef,useState} from 'react';
import {View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import type {OwnerAPI} from './owner-api';
import type {PerspectiveIntent} from './perspective-intent';
import type {PerspectiveAttempt} from './perspective-attempt';
import type {PerspectiveAttemptStore} from './perspective-attempt-store';
import {PerspectiveLinesAttempt} from './perspective-lines-attempt';
import {PerspectiveLinesAttemptStore} from './perspective-lines-attempt-store';
import {canPublishPerspectiveLines,freezePerspectiveLinesIntent,type PerspectiveLinesIntent} from './perspective-lines';
import {handoffPerspectiveLines} from './perspective-lines-handoff';
import {useLocale} from './locale-context';
type Props={operationAvailable?:()=>boolean;detectionAvailable?:boolean;detectedProducer?:string;url?:string;aspect?:number;api:OwnerAPI;origin:string;tenant:string;project:string;version:string;body:PerspectiveIntent;jobs:PerspectiveAttemptStore;disabled:boolean;onSnapshot:(snapshot:string)=>void;onPrepared:(attempt:PerspectiveAttempt)=>void;onPending:(pending:boolean)=>void};
export function EditorPerspectiveLines(props:Props){
 const __uiT=useLocale().t;

 const {t}=useLocale();const [request,setRequest]=useState<PerspectiveLinesIntent>({schema_version:1,snapshot_id:props.body.snapshot_id,base:props.body.params.base,line_segments:{schema_version:1,lines:[{a:{x:0,y:.2},b:{x:1,y:.2},family:'horizontal'},{a:{x:0,y:.8},b:{x:1,y:.8},family:'horizontal'},{a:{x:.2,y:0},b:{x:.2,y:1},family:'vertical'},{a:{x:.8,y:0},b:{x:.8,y:1},family:'vertical'}]}});
 const [store,setStore]=useState<PerspectiveLinesAttemptStore|null>(null),[attempt,setAttempt]=useState<PerspectiveLinesAttempt|null>(null),[busy,setBusy]=useState(true),[error,setError]=useState(false);
 const [authored,setAuthored]=useState(false);
 const [detectionReceipt,setDetectionReceipt]=useState<PerspectiveLinesSubmissionReceipt|null>(null),[detectionStatus,setDetectionStatus]=useState<PerspectiveDetectionStatus|'paused'|'draft_expired'|null>(null);
 const [detectedTask,setDetectedTask]=useState(props.detectedProducer);const detectionJournal=useRef<string|null>(null);
 const live=useRef(true),flight=useRef<AbortController|null>(null),locked=useRef(false);
 const disabledNow=useRef(props.disabled);disabledNow.current=props.disabled;
 const availability=useRef(false);availability.current=!props.disabled&&props.detectionAvailable===true;
 const liveAvailability=useRef(props.operationAvailable);liveAvailability.current=props.operationAvailable;
 const detectionAvailable=()=>availability.current&&(liveAvailability.current===undefined||liveAvailability.current()===true);
 const detectionAPI=perspectiveDetectionAPI(props.api,()=>live.current&&detectionAvailable());
 useEffect(()=>{const controller=new AbortController();flight.current=controller;live.current=true;props.onPending(true);
 void(async()=>{try{const user=await props.api.me(controller.signal);if(controller.signal.aborted)return;detectionJournal.current=perspectiveDetectionJournalKey(props.origin,user.id,props.tenant,props.project,props.version);const recovered=await recoverPerspectiveDetectionReceipt(AsyncStorage,detectionJournal.current,props.tenant,props.project,{schema_version:1,snapshot_id:props.body.snapshot_id,base:props.body.params.base},controller.signal);if(controller.signal.aborted)return;if(recovered){setDetectedTask(recovered.task_id);setDetectionReceipt(recovered);}const journal=new PerspectiveLinesAttemptStore(AsyncStorage,props.origin,user.id,props.tenant,props.project,props.version);const saved=await journal.read();if(controller.signal.aborted)return;setStore(journal);if(saved){setRequest(saved.body);setAttempt(PerspectiveLinesAttempt.restore(saved,journal));props.onPending(true);}else props.onPending(false);}catch{if(!controller.signal.aborted)setError(true);}finally{if(!controller.signal.aborted)setBusy(false);}})();
 return()=>{live.current=false;controller.abort();flight.current?.abort();};},[]);
 const currentScope=JSON.stringify([props.tenant,props.project,props.version,props.body.snapshot_id,props.body.params.base,props.detectedProducer]);
 const latestScope=useRef(currentScope);latestScope.current=currentScope;
 useEffect(()=>{
  if(!detectionReceipt||attempt||props.disabled)return;
  const controller=new AbortController();const scope=currentScope;let timer:ReturnType<typeof setTimeout>|undefined;let count=0;let timedOut=false;
  const deadline=setTimeout(()=>{timedOut=true;controller.abort();if(live.current&&latestScope.current===scope)setDetectionStatus('paused');},600_000);
  const active=()=>live.current&&!timedOut&&!controller.signal.aborted&&latestScope.current===scope;
  const check=async()=>{try{
   const status=await observePerspectiveDetection(props.api,props.tenant,props.project,{schema_version:1,snapshot_id:request.snapshot_id,base:request.base},detectionReceipt,controller.signal);
   if(!active())return;setDetectionStatus(status);
   if(status==='pending'){count++;if(count<120)timer=setTimeout(()=>void check(),5000);else{clearTimeout(deadline);setDetectionStatus('paused');}}else clearTimeout(deadline);
  }catch{if(active()){clearTimeout(deadline);setDetectionStatus('paused');setError(true);}}};
  void check();return()=>{controller.abort();clearTimeout(deadline);if(timer!==undefined)clearTimeout(timer);};
 },[detectionReceipt,currentScope,attempt,props.disabled]);
 async function detect(){
  if(!detectionJournal.current||attempt||busy||!detectionAvailable()||locked.current)return;
  const scope=currentScope;locked.current=true;setBusy(true);setError(false);
  const controller=new AbortController();flight.current=controller;
  try{const receipt=await submitPerspectiveDetection(detectionAPI,AsyncStorage,detectionJournal.current,props.tenant,props.project,{schema_version:1,snapshot_id:request.snapshot_id,base:request.base},()=>crypto.randomUUID(),controller.signal);
   if(!live.current||controller.signal.aborted||latestScope.current!==scope)return;
   setDetectedTask(receipt.task_id);setDetectionReceipt(receipt);
  }catch(error){if(live.current&&!controller.signal.aborted&&latestScope.current===scope){setError(true);if(isPerspectiveDetectionDraftExpired(error))setDetectionStatus('draft_expired');}}
  finally{locked.current=false;if(live.current)setBusy(false);}
 }
 async function restart(){
  if(!detectionJournal.current||attempt||busy||!detectionAvailable()||locked.current)return;
  const scope=currentScope;locked.current=true;setBusy(true);setError(false);
  const controller=new AbortController();flight.current=controller;
  try{const body=await restartPerspectiveDetection(detectionAPI,AsyncStorage,detectionJournal.current,props.tenant,props.project,{schema_version:1,snapshot_id:request.snapshot_id,base:request.base},()=>crypto.randomUUID(),controller.signal);
   if(!live.current||controller.signal.aborted||latestScope.current!==scope)return;
   setRequest(old=>({...old,snapshot_id:body.snapshot_id,base:body.base}));setAuthored(false);setDetectedTask(undefined);setDetectionReceipt(null);setDetectionStatus(null);props.onSnapshot(body.snapshot_id);
  }catch(error){if(live.current&&!controller.signal.aborted&&latestScope.current===scope){
   if(error instanceof PerspectiveDetectionAdmissionRecovered){setDetectedTask(error.receipt.task_id);setDetectionReceipt(error.receipt);setDetectionStatus('pending');setError(false);}
   else setError(true);
  }}
  finally{locked.current=false;if(live.current)setBusy(false);}
 }
 async function loadDetected(){
  if(!canLoadDetectedPerspectiveLines(detectedTask,detectionReceipt,detectionStatus,props.detectedProducer)||attempt||busy||props.disabled||locked.current)return;
  const scope=currentScope;locked.current=true;setBusy(true);setError(false);
  const controller=new AbortController();flight.current=controller;
  try{const next=await loadDetectedPerspectiveLines(props.api,props.project,detectedTask!,request,controller.signal);
   if(!live.current||controller.signal.aborted||latestScope.current!==scope)return;
   setRequest(next);setAuthored(true);
  }catch{if(live.current&&!controller.signal.aborted&&latestScope.current===scope)setError(true);}
  finally{locked.current=false;if(live.current)setBusy(false);}
 }
 function change(index:number,endpoint:'a'|'b',axis:'x'|'y',text:string){if(attempt||busy||props.disabled||locked.current)return;const value=Number(text);if(!text.trim()||!Number.isFinite(value)||value<0||value>1){setError(true);return;}setError(false);setAuthored(true);setRequest(old=>({...old,line_segments:{...old.line_segments,lines:old.line_segments.lines.map((line,i)=>i===index?{...line,[endpoint]:{...line[endpoint],[axis]:value}}:line)}}));}
 async function publish(){if(!store||!canPublishPerspectiveLines(authored,!!attempt)||locked.current||props.disabled)return;locked.current=true;setBusy(true);setError(false);const controller=new AbortController();flight.current=controller;
 try{const current=attempt??new PerspectiveLinesAttempt(props.tenant,props.project,crypto.randomUUID(),freezePerspectiveLinesIntent(request),store);setAttempt(current);props.onPending(true);await current.submit((project,body,key,signal)=>{if(!live.current||signal?.aborted||disabledNow.current)throw new Error("OPERATION_UNAVAILABLE");return props.api.publishPerspectiveLines(project,body,key,signal)},controller.signal);if(!live.current||controller.signal.aborted)return;const job=await handoffPerspectiveLines(current.snapshot,props.jobs,store,props.body.params.strength,props.body.params.max_crop_fraction,controller.signal);if(!live.current||controller.signal.aborted)return;props.onPrepared(job);}catch{if(live.current&&!controller.signal.aborted)setError(true);}finally{locked.current=false;if(live.current)setBusy(false);}}
 return <View><Text>{t('perspective_lines_title')}</Text><Text>{t('perspective_lines_hint')}</Text>{detectionStatus?<Text accessibilityLiveRegion="polite">{t(`perspective_detection_${detectionStatus==='draft_expired'?'cancelled':detectionStatus}`)}</Text>:null}<Button accessibilityRole="button" disabled={!!attempt||busy||props.disabled||!props.detectionAvailable} onPress={()=>void detect()} mode="outlined">{t('perspective_lines_detect')}</Button>{['failed','cancelled','unsupported','draft_expired'].includes(detectionStatus??'')?<Button accessibilityRole="button" disabled={!!attempt||busy||props.disabled||!props.detectionAvailable} onPress={()=>void restart()} mode="outlined">{t('perspective_detection_restart')}</Button>:null}{canLoadDetectedPerspectiveLines(detectedTask,detectionReceipt,detectionStatus,props.detectedProducer)?<Button accessibilityRole="button" disabled={!!attempt||busy||props.disabled} onPress={()=>void loadDetected()} mode="outlined">{t('perspective_lines_load_detected')}</Button>:null}{props.url?<PerspectiveLinesCanvas url={props.url} aspect={props.aspect??1} lines={request.line_segments.lines} disabled={!!attempt||busy||props.disabled} horizontalLabel={t('perspective_lines_horizontal')} verticalLabel={t('perspective_lines_vertical')} onChange={lines=>{if(!attempt&&!busy&&!props.disabled&&!locked.current){setError(false);setAuthored(true);setRequest(old=>({...old,line_segments:{...old.line_segments,lines}}));}}}/>:null}{request.line_segments.lines.map((line,index)=><View key={index}><Text>{t(line.family==='horizontal'?'perspective_lines_horizontal':'perspective_lines_vertical')} {index%2+1}</Text>{(['a','b'] as const).map(endpoint=><View key={endpoint}>{(['x','y'] as const).map(axis=><TextInput key={axis} accessibilityLabel={__uiT('ui_efbde6c4fc1b', {p0:(line.family),p1:(index+1),p2:(endpoint),p3:(axis)})} keyboardType="decimal-pad" editable={!attempt&&!busy&&!props.disabled} value={String(line[endpoint][axis])} onChangeText={text=>change(index,endpoint,axis,text)}/>)}</View>)}</View>)}{error?<Text accessibilityRole="alert">{t('perspective_lines_error')}</Text>:null}<Button accessibilityRole="button" disabled={!store||busy||props.disabled||!canPublishPerspectiveLines(authored,!!attempt)} onPress={()=>void publish()} mode="outlined">{t(attempt?'perspective_lines_retry':'perspective_lines_prepare')}</Button></View>;
}
