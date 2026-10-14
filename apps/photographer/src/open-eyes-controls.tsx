import {useEffect,useRef,useState} from 'react';
import {ActivityIndicator,Pressable,Text,View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import type {OwnerAPI} from './owner-api';
import {OpenEyesAttempt} from './open-eyes-attempt';
import {OpenEyesAttemptStore} from './open-eyes-attempt-store';
import {freezeOpenEyesSelection,readPreparedOpenEyesIntent,type OpenEyesSelection,type OpenEyesTicket} from './open-eyes-intent';

type Props={api:OwnerAPI;origin:string;user:string;tenant:string;project:string;baseVersion:string;selection?:OpenEyesSelection;disabled?:boolean;onSubmitted:(ticket:OpenEyesTicket)=>void};

// Preparation creates no run. Once submission begins, the exact prepared body
// and key are stored before network IO and remain fixed for ambiguous retries.
export function OpenEyesControls(props:Props){
 const selectionScope=JSON.stringify(props.selection??null);
 const [store,setStore]=useState<OpenEyesAttemptStore|null>(null),[attempt,setAttempt]=useState<OpenEyesAttempt|null>(null),[busy,setBusy]=useState(true),[message,setMessage]=useState('');
 const active=useRef(true),flight=useRef<AbortController|null>(null),sending=useRef(false);
 useEffect(()=>{
  active.current=true;const controller=new AbortController();flight.current=controller;
  void(async()=>{try{
   const selection=props.selection?freezeOpenEyesSelection(props.selection):null;
   const journal=new OpenEyesAttemptStore(AsyncStorage,props.origin,props.user,props.tenant,props.project,props.baseVersion);
   const saved=await journal.read();if(!active.current||controller.signal.aborted)return;
   if(saved){
    if(selection)readPreparedOpenEyesIntent(saved.body,selection);
    setAttempt(OpenEyesAttempt.restore(saved,journal));
   }else if(!selection)throw new Error('OPEN_EYES_SELECTION_REQUIRED');
   setStore(journal);
  }catch{if(active.current&&!controller.signal.aborted)setMessage('Open eyes is unavailable for this selection.');}
  finally{if(active.current&&!controller.signal.aborted)setBusy(false);}})();
  return()=>{active.current=false;controller.abort();flight.current?.abort();};
 },[props.api,props.origin,props.user,props.tenant,props.project,selectionScope]);
 async function submit(){
  if(!store||sending.current||props.disabled)return;
  sending.current=true;setBusy(true);setMessage('');const controller=new AbortController();flight.current=controller;
  let current=attempt;
  try{
   if(!current){
    if(!props.selection)throw new Error('OPEN_EYES_SELECTION_REQUIRED');
    const key=crypto.randomUUID();
    const selection=freezeOpenEyesSelection(props.selection);
    const prepared=await props.api.prepareOpenEyes(props.project,selection,key,controller.signal);
    if(!active.current||controller.signal.aborted)return;
    current=new OpenEyesAttempt(props.project,key,prepared,store);
    setAttempt(current);
   }
   const ticket=await current.submit((project,body,key,signal)=>props.api.submitOpenEyes(project,body,key,signal),controller.signal);
   if(!active.current||controller.signal.aborted)return;
   const workflow=await props.api.get<{run_id:string;workflow_id:string}>(`/api/v1/tenants/{t}/v2-workflows/${encodeURIComponent(ticket.workflow_id)}`,controller.signal);
   if(workflow.run_id!==ticket.run_id||workflow.workflow_id!==ticket.workflow_id)throw new Error('OPEN_EYES_TICKET_SCOPE_CHANGED');
   if(active.current&&!controller.signal.aborted)props.onSubmitted(ticket);
  }catch{if(active.current&&!controller.signal.aborted)setMessage(current?'Submission is uncertain. Retry the same request.':'Unable to prepare open eyes.');}
  finally{sending.current=false;if(active.current&&!controller.signal.aborted)setBusy(false);}
 }
 return <View><Text accessibilityRole="header">Open eyes</Text>
  <Text>Use a confirmed donor observation of the same person. The original image remains unchanged until review.</Text>
  {message?<Text accessibilityRole="alert">{message}</Text>:null}
  {busy?<ActivityIndicator accessibilityLabel="Preparing open eyes"/>:null}
  <Pressable accessibilityRole="button" disabled={busy||props.disabled||!store} onPress={()=>void submit()}>
   <Text>{attempt?.state==='accepted'?'Open result':attempt?'Retry submission':'Prepare and submit'}</Text>
  </Pressable>
 </View>;
}
