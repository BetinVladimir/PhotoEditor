import {useEffect,useRef,useState} from 'react';
import {ActivityIndicator,Pressable,Text,View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import type {OwnerAPI} from './owner-api';
import {GlareDonorAttempt} from './glare-donor-attempt';
import {GlareDonorAttemptStore} from './glare-donor-attempt-store';
import {freezeGlareDonorPreparation,readPreparedGlareDonorIntent,type GlareDonorPreparation,type GlareDonorTicket} from './glare-donor-intent';

type Props={api:OwnerAPI;origin:string;user:string;tenant:string;project:string;baseVersion:string;selection?:GlareDonorPreparation;disabled?:boolean;onSubmitted:(ticket:GlareDonorTicket)=>void};

// Preparation creates no run. Once submission begins, the exact prepared body
// and key are stored before network IO and remain fixed for ambiguous retries.
export function GlareDonorControls(props:Props){
 const selectionScope=JSON.stringify(props.selection??null);
 const [store,setStore]=useState<GlareDonorAttemptStore|null>(null),[attempt,setAttempt]=useState<GlareDonorAttempt|null>(null),[busy,setBusy]=useState(true),[message,setMessage]=useState('');
 const active=useRef(true),flight=useRef<AbortController|null>(null),sending=useRef(false);
 useEffect(()=>{
  active.current=true;const controller=new AbortController();flight.current=controller;
  void(async()=>{try{
   const selection=props.selection?freezeGlareDonorPreparation(props.selection):null;
   const journal=new GlareDonorAttemptStore(AsyncStorage,props.origin,props.user,props.tenant,props.project,props.baseVersion);
   const saved=await journal.read();if(!active.current||controller.signal.aborted)return;
   if(saved){
    if(selection)readPreparedGlareDonorIntent(saved.body,selection);
    setAttempt(GlareDonorAttempt.restore(saved,journal));
   }else if(!selection)throw new Error('GLARE_DONOR_SELECTION_REQUIRED');
   setStore(journal);
  }catch{if(active.current&&!controller.signal.aborted)setMessage('Donor glare is unavailable for this selection.');}
  finally{if(active.current&&!controller.signal.aborted)setBusy(false);}})();
  return()=>{active.current=false;controller.abort();flight.current?.abort();};
 },[props.api,props.origin,props.user,props.tenant,props.project,selectionScope]);
 async function submit(){
  if(!store||sending.current||props.disabled)return;
  sending.current=true;setBusy(true);setMessage('');const controller=new AbortController();flight.current=controller;
  let current=attempt;
  try{
   if(!current){
    if(!props.selection)throw new Error('GLARE_DONOR_SELECTION_REQUIRED');
    const key=crypto.randomUUID();
    const selection=freezeGlareDonorPreparation(props.selection);
    const prepared=await props.api.prepareGlareDonor(props.project,selection,key,controller.signal);
    if(!active.current||controller.signal.aborted)return;
    current=new GlareDonorAttempt(props.project,key,prepared,store);
    setAttempt(current);
   }
   const ticket=await current.submit((project,body,key,signal)=>props.api.submitGlareDonor(project,body,key,signal),controller.signal);
   if(!active.current||controller.signal.aborted)return;
   const workflow=await props.api.get<{run_id:string;workflow_id:string}>(`/api/v1/tenants/{t}/v2-workflows/${encodeURIComponent(ticket.workflow_id)}`,controller.signal);
   if(workflow.run_id!==ticket.run_id||workflow.workflow_id!==ticket.workflow_id)throw new Error('GLARE_DONOR_TICKET_SCOPE_CHANGED');
   if(active.current&&!controller.signal.aborted)props.onSubmitted(ticket);
  }catch{if(active.current&&!controller.signal.aborted)setMessage(current?'Submission is uncertain. Retry the same request.':'Unable to prepare donor glare.');}
  finally{sending.current=false;if(active.current&&!controller.signal.aborted)setBusy(false);}
 }
 return <View><Text accessibilityRole="header">Donor glare</Text>
  <Text>Use a confirmed donor observation from the same person. The original image remains unchanged until review.</Text>
  {message?<Text accessibilityRole="alert">{message}</Text>:null}
  {busy?<ActivityIndicator accessibilityLabel="Preparing donor glare"/>:null}
  <Pressable accessibilityRole="button" disabled={busy||props.disabled||!store} onPress={()=>void submit()}>
   <Text>{attempt?.state==='accepted'?'Open result':attempt?'Retry submission':'Prepare and submit'}</Text>
  </Pressable>
 </View>;
}
