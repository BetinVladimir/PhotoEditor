import {Button,Text} from 'react-native-paper';
import {readDeadline} from './read-deadline';
import {useEffect,useMemo,useRef,useState} from 'react';
import {AppState,View} from 'react-native';
import {APIError,type ApiClient,type SemanticReviewStatus} from '@photoeditor/api-client';
import type en from '../../../locales/en/common.json';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
type Props={client:Pick<ApiClient,'semanticReviewStatus'>;tenant:string;intent:string;onReady:()=>void;onAccessLost?:()=>void};
const processing=(state:SemanticReviewStatus['state'])=>['queued','running','cancelling'].includes(state);
const labels:Record<SemanticReviewStatus['state'],keyof typeof en>={queued:'semantic_progress_queued',running:'semantic_progress_running',waiting_review:'semantic_progress_waiting_review',cancelling:'semantic_progress_cancelling',cancelled:'semantic_progress_cancelled',completed:'semantic_progress_completed',failed:'semantic_progress_failed_state'};
export function SemanticParentProgress(props:Props){
 const scope=useMemo(()=>crypto.randomUUID(),[props.client,props.tenant,props.intent]);
 return <SemanticParentProgressScope key={scope} {...props}/>;
}
function SemanticParentProgressScope({client,tenant,intent,onReady,onAccessLost}:Props){
 const {t}=useLocale();
 const[busy,setBusy]=useState(false),stopRead=useRef<(()=>void)|null>(null);
 const[status,setStatus]=useState<SemanticReviewStatus|null>(null),[failed,setFailed]=useState(false),[epoch,setEpoch]=useState(0);
 const callback=useRef(onReady),retained=useRef<SemanticReviewStatus|null>(null),notified=useRef(false);
 callback.current=onReady;
 const accessLost=useRef(onAccessLost);accessLost.current=onAccessLost;
 useEffect(()=>{
  let active=true,foreground=(AppState.currentState??'active')==='active',flight:AbortController|null=null,timer:ReturnType<typeof setTimeout>|undefined;
  const stop=()=>{if(timer!==undefined){clearTimeout(timer);timer=undefined}flight?.abort();flight=null;setBusy(false)};
  stopRead.current=()=>{stop();setFailed(true)};
  async function poll(){
   if(!active||!foreground||flight)return;
   const controller=new AbortController();flight=controller;setBusy(true);
   try{
    const value=await readDeadline(controller.signal,signal=>client.semanticReviewStatus(tenant,intent,signal));
    if(!active||controller.signal.aborted)return;
    const old=retained.current;
    // The route ID is semantic_intents.id, independently generated from the
    // workflow UUID. Pin the server-returned pair across later observations.
    if(old&&(value.workflow_id!==old.workflow_id||value.run_id!==old.run_id))throw new Error('SEMANTIC_PARENT_SCOPE_INVALID');
    if(old&&BigInt(value.revision)<BigInt(old.revision))throw new Error('SEMANTIC_PARENT_REVISION_REGRESSED');
    retained.current=structuredClone(value);setStatus(value);setFailed(false);
    if(value.state==='waiting_review'&&!notified.current){notified.current=true;callback.current()}
    if(processing(value.state))timer=setTimeout(()=>void poll(),3000);
   }catch(cause){if(active&&!controller.signal.aborted){setFailed(true);if(cause instanceof APIError&&[401,403,404,410].includes(cause.status)){setStatus(null);notified.current=false;accessLost.current?.()}}}
   finally{if(flight===controller){flight=null;setBusy(false)}}
  }
  const listener=AppState.addEventListener('change',state=>{
   foreground=state==='active';
   if(!foreground)stop();
   else void poll();
  });
  void poll();
  return()=>{active=false;stopRead.current=null;stop();listener.remove()};
 },[client,tenant,intent,epoch]);
 return <View>{busy?<Button accessibilityRole="button" onPress={()=>stopRead.current?.()} mode="outlined">{t('read_cancel')}</Button>:null}{status?<Text accessibilityLiveRegion="polite">{t('semantic_parent_status').replace('{state}',t(labels[status.state]))}</Text>:null}{failed?<><Text accessibilityRole="alert" style={s.error}>{t('semantic_progress_failed')}</Text><Button accessibilityRole="button" onPress={()=>{setFailed(false);setEpoch(value=>value+1)}}    mode="contained" contentStyle={{minHeight:48}}>{t('semantic_results_refresh')}</Button></>:null}</View>;
}
