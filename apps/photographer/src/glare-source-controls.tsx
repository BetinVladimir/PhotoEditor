import {Button,ActivityIndicator,Text} from 'react-native-paper';
import {useLocale} from './locale-context';
import {useEffect,useMemo,useRef,useState} from 'react';
import {View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import {GlareSourceAttempt,GlareSourceAttemptStore} from './glare-source-attempt';
import {freezeGlareSourcePreparation,readPreparedGlareSourceIntent,type GlareSourcePreparation,type GlareSourceTicket} from './glare-source-intent';
import {useReviewForeground} from './use-review-foreground';
type Props={api:OwnerAPI;origin:string;user:string;tenant:string;project:string;baseVersion:string;selection?:GlareSourcePreparation;disabled?:boolean;operationAvailable?:()=>boolean;onSubmitted:(ticket:GlareSourceTicket)=>void;onAccessDenied?:()=>void};
export function GlareSourceControls(props:Props){
 const __uiT=useLocale().t;
const foreground=useReviewForeground();const scope=useMemo(()=>crypto.randomUUID(),[props.api,props.origin,props.user,props.tenant,props.project,props.baseVersion,JSON.stringify(props.selection??null),foreground.generation]);return foreground.active?<Scoped key={scope} {...props}/>:<Text>{__uiT('ui_5ff2cdf22fd2')}</Text>;}
function Scoped(props:Props){
 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;

 const {t}=useLocale();
 const [setupRetry,setSetupRetry]=useState(0),[displayedFlight,setDisplayedFlight]=useState<AbortController|null>(null);
 const timers=useRef(new Map<AbortController,ReturnType<typeof setTimeout>>());
 const [store,setStore]=useState<GlareSourceAttemptStore|null>(null),[attempt,setAttempt]=useState<GlareSourceAttempt|null>(null),[busy,setBusy]=useState(true),[message,setMessage]=useState('');
 const active=useRef(true),flight=useRef<AbortController|null>(null),sending=useRef(false),disabled=useRef(props.disabled),available=useRef(props.operationAvailable);disabled.current=props.disabled;available.current=props.operationAvailable;
 const unavailable=()=>disabled.current===true||(available.current!==undefined&&available.current()!==true);
 function revokeAccess(){setStore(null);setAttempt(null);setMessage('Access is no longer available. Reopen the editor after access is restored.');props.onAccessDenied?.();}
 function settle(controller:AbortController){const timer=timers.current.get(controller);if(timer!==undefined)clearTimeout(timer);timers.current.delete(controller);}
 function stop(controller:AbortController|null){if(!active.current||!controller||flight.current!==controller||controller.signal.aborted)return;settle(controller);controller.abort();sending.current=false;setBusy(false);setMessage(t('editor_wait_stopped'));}
 function arm(controller:AbortController,milliseconds:number){flight.current=controller;setDisplayedFlight(controller);timers.current.set(controller,setTimeout(()=>stop(controller),milliseconds));}
 useEffect(()=>{active.current=true;const controller=new AbortController();setBusy(true);setMessage('');arm(controller,8000);
  void(async()=>{try{const journal=new GlareSourceAttemptStore(AsyncStorage,props.origin,props.user,props.tenant,props.project,props.baseVersion);const saved=await journal.read();if(!active.current||controller.signal.aborted||disabled.current)return;if(saved){if(props.selection)readPreparedGlareSourceIntent(saved.body,props.selection);setAttempt(GlareSourceAttempt.restore(saved,journal));}else if(!props.selection)throw new Error('SELECTION_REQUIRED');setStore(journal);}catch{if(active.current&&!controller.signal.aborted)setMessage('Generative glare is unavailable for this selection.');}finally{settle(controller);if(active.current&&!controller.signal.aborted)setBusy(false);}})();
  return()=>{active.current=false;controller.abort();flight.current?.abort();for(const timer of timers.current.values())clearTimeout(timer);timers.current.clear();};
 },[props.api,props.origin,props.user,props.tenant,props.project,props.baseVersion,JSON.stringify(props.selection??null),setupRetry]);
 async function submit(){
  if(!active.current||!store||sending.current||disabled.current||!attempt?.canReplay&&unavailable())return;
  sending.current=true;setBusy(true);setMessage('');const controller=new AbortController();arm(controller,15000);const recovery=attempt?.canReplay===true;let current=attempt;
  try{
   const before=await props.api.me(controller.signal);
   if(!active.current||controller.signal.aborted||disabled.current)return;
   if(before.id!==props.user){revokeAccess();return;}
   if(!recovery){
    const enabled=await props.api.glareSourceAvailable(props.project,controller.signal);
    if(!active.current||controller.signal.aborted||disabled.current)return;
    if(!enabled){setStore(null);setMessage('Generative glare is not available with the current qualified installation. Your saved request is retained.');return;}
   }
   if(!current){if(!props.selection)throw new Error('SELECTION_REQUIRED');const key=crypto.randomUUID();const prepared=await props.api.prepareGlareSource(props.project,freezeGlareSourcePreparation(props.selection),key,controller.signal);if(!active.current||controller.signal.aborted||unavailable())return;current=new GlareSourceAttempt(props.project,key,prepared,store);setAttempt(current);}
   const ticket=await current.submit((project,body,key,signal)=>{if(disabled.current||!recovery&&unavailable())throw new Error('OPERATION_UNAVAILABLE');return props.api.submitGlareSource(project,body,key,signal)},controller.signal);
   if(!active.current||controller.signal.aborted||disabled.current)return;
   const workflow=await props.api.get<{run_id:string;workflow_id:string}>(`/api/v1/tenants/{t}/v2-workflows/${encodeURIComponent(ticket.workflow_id)}`,controller.signal);
   if(workflow.run_id!==ticket.run_id||workflow.workflow_id!==ticket.workflow_id)throw new Error('GLARE_SOURCE_TICKET_CHANGED');
   const after=await props.api.me(controller.signal);
   if(!active.current||controller.signal.aborted||disabled.current)return;
   if(after.id!==props.user){revokeAccess();return;}
   if(active.current&&!controller.signal.aborted&&!disabled.current)props.onSubmitted(ticket);
  }catch(e){if(active.current&&!controller.signal.aborted){if(e instanceof APIError&&[401,403,404,410].includes(e.status))revokeAccess();else setMessage(current?'Submission is uncertain. Retry the same request.':'Unable to prepare generative glare.');}}
  finally{settle(controller);if(flight.current===controller){sending.current=false;if(active.current&&!controller.signal.aborted)setBusy(false);}}
 }
 return <View><Text accessibilityRole="header">{__uiT('ui_b241a65d1938')}</Text><Text>{__uiT('ui_2e870a14e50b')}</Text>{message?<Text accessibilityRole="alert">{__uiMessage(message)}</Text>:null}{busy?<ActivityIndicator accessibilityLabel={__uiT('ui_a68ee99ff3fc')}/>:null}<Button accessibilityRole="button" disabled={busy||props.disabled||!store||!attempt?.canReplay&&unavailable()} onPress={()=>void submit()} mode="outlined">{attempt?.state==='accepted'?__uiT('ui_3e74fe245f58'):attempt?__uiT('ui_8fc2e587a1e2'):__uiT('ui_47dcc537d2c3')}</Button>{busy?<Button accessibilityRole="button" onPress={()=>stop(displayedFlight)} mode="outlined">{t('read_cancel')}</Button>:!store?<Button accessibilityRole="button" onPress={()=>{if(active.current&&!sending.current)setSetupRetry(value=>value+1);}} mode="outlined">{t('read_retry')}</Button>:null}</View>;
}
