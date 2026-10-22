import {Button,Text} from 'react-native-paper';
import {useEffect,useRef,useState} from 'react';
import {View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import type {RegionJobTicket} from './region-preview';
import {RelightAnalysisAttempt} from './relight-analysis-attempt';
import type {RelightAnalysisAttemptStore} from './relight-analysis-attempt-store';
import type {RelightAnalysisIntent} from './relight-analysis';
import {RelightAnalysisProgress} from './relight-analysis-progress';
type ProgressLabels={queued:string;running:string;waiting_review:string;cancelling:string;cancelled:string;completed:string;failed:string;unavailable:string;refresh:string};
type Props={api:OwnerAPI;project:string;intent:RelightAnalysisIntent;store:RelightAnalysisAttemptStore;disabled?:boolean;capabilityAvailable?:boolean;operationAvailable?:()=>boolean;available:boolean;labels:{create:string;retry:string;open:string;loading:string;unknown:string;unavailable:string};progressLabels?:ProgressLabels;onRefresh?:()=>void;verifyTicket:(ticket:RegionJobTicket,signal:AbortSignal)=>Promise<void>;onSubmitted:(ticket:RegionJobTicket)=>void};
/** Accepted journals are authenticated again before navigation. Unknown attempts
 * always retain their stored snapshot/source/body/key across foreground remount. */
export function RelightAnalysisAction(props:Props){

 const [attempt,setAttempt]=useState<RelightAnalysisAttempt|null>(null),[loading,setLoading]=useState(true),[busy,setBusy]=useState(false),[blocked,setBlocked]=useState(false),[error,setError]=useState('');
 const current=useRef<RelightAnalysisAttempt|null>(null),live=useRef(true),flight=useRef<AbortController|null>(null),sending=useRef(false),generation=useRef(0);
 const disabledNow=useRef(props.disabled);disabledNow.current=props.disabled;
 const capabilityNow=useRef(props.capabilityAvailable);capabilityNow.current=props.capabilityAvailable;
 const availabilityNow=useRef(props.operationAvailable);availabilityNow.current=props.operationAvailable;
 const capabilityUnavailable=()=>capabilityNow.current===false||(availabilityNow.current!==undefined&&availabilityNow.current()!==true);
 useEffect(()=>{
  const epoch=++generation.current;live.current=true;current.current=null;sending.current=false;setAttempt(null);setLoading(true);setBusy(false);setBlocked(false);setError('');const controller=new AbortController();flight.current=controller;
  void props.store.read().then(saved=>{if(!live.current||generation.current!==epoch||controller.signal.aborted)return;if(saved){const restored=RelightAnalysisAttempt.restore(saved,props.store);current.current=restored;setAttempt(restored);}}).catch(()=>{if(live.current&&generation.current===epoch&&!controller.signal.aborted){setBlocked(true);setError(props.labels.unavailable);}}).finally(()=>{if(live.current&&generation.current===epoch&&!controller.signal.aborted)setLoading(false);});
  return()=>{live.current=false;controller.abort();flight.current?.abort();};
 },[props.store]);
 async function submit(){
  if(loading||blocked||sending.current||props.disabled||!current.current&&capabilityUnavailable()||(!current.current&&props.available))return;
  const recovering=current.current?.state==='unknown',recoveryOnly=recovering&&capabilityUnavailable();
  const epoch=generation.current;sending.current=true;setBusy(true);setError('');const controller=new AbortController();flight.current=controller;
  try{
   const action=current.current??new RelightAnalysisAttempt(props.project,crypto.randomUUID(),props.intent,props.store);current.current=action;setAttempt(action);
   const ticket=await action.submit((project,body,key,signal)=>{if(!live.current||generation.current!==epoch||signal?.aborted||disabledNow.current||capabilityUnavailable()&&!recovering)throw new Error("RELIGHT_ANALYSIS_CAPABILITY_UNAVAILABLE");const replayOnly=recoveryOnly||recovering&&capabilityUnavailable();return props.api.submitRelightAnalysis(project,body,key,signal,replayOnly)},controller.signal);
   if(!live.current||generation.current!==epoch||controller.signal.aborted)return;
   await props.verifyTicket(ticket,controller.signal);
   if(live.current&&generation.current===epoch&&!controller.signal.aborted&&!disabledNow.current)props.onSubmitted(ticket);
  }catch{if(live.current&&generation.current===epoch&&!controller.signal.aborted)setError(props.labels.unknown);}
  finally{if(generation.current===epoch)sending.current=false;if(live.current&&generation.current===epoch&&!controller.signal.aborted)setBusy(false);}
 }
 const label=loading||busy?props.labels.loading:attempt?.state==='accepted'?props.labels.open:attempt?props.labels.retry:props.labels.create;
 const ticket=attempt?.state==='accepted'?attempt.snapshot.ticket:null;
 return <View>{error?<Text accessibilityRole="alert">{error}</Text>:null}<Button disabled={loading||blocked||busy||props.disabled||!attempt&&capabilityUnavailable()||(!attempt&&props.available)} onPress={()=>void submit()} mode="outlined">{label}</Button>
  {ticket&&props.progressLabels&&props.onRefresh?<RelightAnalysisProgress api={props.api} ticket={ticket} labels={props.progressLabels} onRefresh={props.onRefresh}/>:null}</View>;
}
