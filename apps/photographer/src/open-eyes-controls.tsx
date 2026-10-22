import {Button,ActivityIndicator,Text} from 'react-native-paper';
import {useLocale} from './locale-context';
import {useEffect,useMemo,useRef,useState} from 'react';
import {View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import {OpenEyesAttempt} from './open-eyes-attempt';
import {OpenEyesAttemptStore} from './open-eyes-attempt-store';
import {freezeOpenEyesSelection,readPreparedOpenEyesIntent,type OpenEyesSelection,type OpenEyesTicket} from './open-eyes-intent';
import {useReviewForeground} from './use-review-foreground';

type Props={api:OwnerAPI;origin:string;user:string;tenant:string;project:string;baseVersion:string;selection?:OpenEyesSelection;disabled?:boolean;operationAvailable?:()=>boolean;onSubmitted:(ticket:OpenEyesTicket)=>void;onAccessDenied?:()=>void};

// Preparation creates no run. Once submission begins, the exact prepared body
// and key are stored before network IO and remain fixed for ambiguous retries.
export function OpenEyesControls(props:Props){
 const __uiT=useLocale().t;

 const foreground=useReviewForeground();
 const scope=useMemo(()=>crypto.randomUUID(),[props.api,props.origin,props.user,props.tenant,props.project,props.baseVersion,JSON.stringify(props.selection??null),foreground.generation]);
 return foreground.active?<ScopedOpenEyesControls key={scope} {...props}/>:<Text>{__uiT('ui_70636885f8ff')}</Text>;
}
function ScopedOpenEyesControls(props:Props){
 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;

 const selectionScope=JSON.stringify(props.selection??null);
 const {t}=useLocale();
 const [setupRetry,setSetupRetry]=useState(0),[displayedFlight,setDisplayedFlight]=useState<AbortController|null>(null);
 const timers=useRef(new Map<AbortController,ReturnType<typeof setTimeout>>());
 const [store,setStore]=useState<OpenEyesAttemptStore|null>(null),[attempt,setAttempt]=useState<OpenEyesAttempt|null>(null),[busy,setBusy]=useState(true),[message,setMessage]=useState('');
 const disabledNow=useRef(props.disabled);disabledNow.current=props.disabled;
 const liveAvailability=useRef(props.operationAvailable);liveAvailability.current=props.operationAvailable;
 const unavailable=()=>disabledNow.current===true||(liveAvailability.current!==undefined&&liveAvailability.current()!==true);
 const active=useRef(true),flight=useRef<AbortController|null>(null),sending=useRef(false);
 function revokeAccess(){setStore(null);setAttempt(null);setMessage('Access is no longer available. Reopen the editor after access is restored.');props.onAccessDenied?.();}
 function settle(controller:AbortController){const timer=timers.current.get(controller);if(timer!==undefined)clearTimeout(timer);timers.current.delete(controller);}
 function stop(controller:AbortController|null){if(!active.current||!controller||flight.current!==controller||controller.signal.aborted)return;settle(controller);controller.abort();sending.current=false;setBusy(false);setMessage(t('editor_wait_stopped'));}
 function arm(controller:AbortController,milliseconds:number){flight.current=controller;setDisplayedFlight(controller);timers.current.set(controller,setTimeout(()=>stop(controller),milliseconds));}
 useEffect(()=>{
  active.current=true;const controller=new AbortController();setBusy(true);setMessage('');arm(controller,8000);
  void(async()=>{try{
   const selection=props.selection?freezeOpenEyesSelection(props.selection):null;
   const journal=new OpenEyesAttemptStore(AsyncStorage,props.origin,props.user,props.tenant,props.project,props.baseVersion);
   const saved=await journal.read();if(!active.current||controller.signal.aborted||disabledNow.current)return;
   if(saved){
    if(selection)readPreparedOpenEyesIntent(saved.body,selection);
    setAttempt(OpenEyesAttempt.restore(saved,journal));
   }else if(!selection)throw new Error('OPEN_EYES_SELECTION_REQUIRED');
   setStore(journal);
  }catch{if(active.current&&!controller.signal.aborted)setMessage('Open eyes is unavailable for this selection.');}
  finally{settle(controller);if(active.current&&!controller.signal.aborted)setBusy(false);}})();
  return()=>{active.current=false;controller.abort();flight.current?.abort();for(const timer of timers.current.values())clearTimeout(timer);timers.current.clear();};
 },[props.api,props.origin,props.user,props.tenant,props.project,props.baseVersion,selectionScope,setupRetry]);
 async function submit(){
  if(!active.current||!store||sending.current||disabledNow.current||!attempt?.canReplay&&unavailable())return;
  sending.current=true;setBusy(true);setMessage('');const controller=new AbortController();arm(controller,15000);
  const recovery=attempt?.canReplay===true;let current=attempt;
  try{
   const before=await props.api.me(controller.signal);
   if(!active.current||controller.signal.aborted||disabledNow.current)return;
   if(before.id!==props.user){revokeAccess();return;}
   if(!current){
    if(!props.selection)throw new Error('OPEN_EYES_SELECTION_REQUIRED');
    const key=crypto.randomUUID();
    const selection=freezeOpenEyesSelection(props.selection);
    const prepared=await props.api.prepareOpenEyes(props.project,selection,key,controller.signal);
    if(!active.current||controller.signal.aborted||unavailable())return;
    current=new OpenEyesAttempt(props.project,key,prepared,store);
    setAttempt(current);
   }
   const ticket=await current.submit((project,body,key,signal)=>{if(disabledNow.current||!recovery&&unavailable())throw new Error("OPERATION_UNAVAILABLE");return props.api.submitOpenEyes(project,body,key,signal)},controller.signal);
   if(!active.current||controller.signal.aborted||disabledNow.current)return;
   const workflow=await props.api.get<{run_id:string;workflow_id:string}>(`/api/v1/tenants/{t}/v2-workflows/${encodeURIComponent(ticket.workflow_id)}`,controller.signal);
   if(workflow.run_id!==ticket.run_id||workflow.workflow_id!==ticket.workflow_id)throw new Error('OPEN_EYES_TICKET_SCOPE_CHANGED');
   const after=await props.api.me(controller.signal);
   if(!active.current||controller.signal.aborted||disabledNow.current)return;
   if(after.id!==props.user){revokeAccess();return;}
   if(active.current&&!controller.signal.aborted&&!disabledNow.current)props.onSubmitted(ticket);
  }catch(error){if(active.current&&!controller.signal.aborted){if(error instanceof APIError&&[401,403,404,410].includes(error.status))revokeAccess();else setMessage(current?'Submission is uncertain. Retry the same request.':'Unable to prepare open eyes.');}}
  finally{settle(controller);if(flight.current===controller){sending.current=false;if(active.current&&!controller.signal.aborted)setBusy(false);}}
 }
 return <View><Text accessibilityRole="header">{__uiT('ui_1a09a77b90b2')}</Text>
  <Text>{__uiT('ui_7b1497a1750b')}</Text>
  {message?<Text accessibilityRole="alert">{__uiMessage(message)}</Text>:null}
  {busy?<ActivityIndicator accessibilityLabel={__uiT('ui_bbf954fa41af')}/>:null}
  <Button accessibilityRole="button" disabled={busy||props.disabled||!store||!attempt?.canReplay&&unavailable()} onPress={()=>void submit()} mode="outlined">
   {attempt?.state==='accepted'?__uiT('ui_3e74fe245f58'):attempt?__uiT('ui_8fc2e587a1e2'):__uiT('ui_47dcc537d2c3')}
  </Button>
 {busy?<Button accessibilityRole="button" onPress={()=>stop(displayedFlight)} mode="outlined">{t('read_cancel')}</Button>:!store?<Button accessibilityRole="button" onPress={()=>{if(active.current&&!sending.current)setSetupRetry(value=>value+1);}} mode="outlined">{t('read_retry')}</Button>:null}</View>;
}
