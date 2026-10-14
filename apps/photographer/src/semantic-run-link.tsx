import {useEffect,useMemo,useRef,useState} from 'react';
import {AppState,Pressable,Text,View} from 'react-native';
import type {ApiClient,SemanticRunIntent} from '@photoeditor/api-client';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
type Props={client:Pick<ApiClient,'semanticRunIntent'>;tenant:string;run:string;workflow:string;onOpen:(intent:string)=>void};
export function SemanticRunLink(props:Props){
 const scope=useMemo(()=>crypto.randomUUID(),[props.client,props.tenant,props.run,props.workflow]);
 return <Scope key={scope} {...props}/>;
}
function Scope({client,tenant,run,workflow,onOpen}:Props){
 const {t}=useLocale();
 const[value,setValue]=useState<SemanticRunIntent|null>(null),[failed,setFailed]=useState(false),[epoch,setEpoch]=useState(0);
 const retained=useRef<SemanticRunIntent|null>(null);
 useEffect(()=>{
  let active=true,foreground=(AppState.currentState??'active')==='active',flight:AbortController|null=null,timer:ReturnType<typeof setTimeout>|undefined;
  const stop=()=>{if(timer!==undefined){clearTimeout(timer);timer=undefined}flight?.abort();flight=null};
  async function poll(){
   if(!active||!foreground||flight)return;
   const controller=new AbortController();flight=controller;
   try{
    const next=await client.semanticRunIntent(tenant,run,controller.signal);
    if(!active||controller.signal.aborted)return;
    const old=retained.current;
    if(next.run_id!==run||next.workflow_id!==workflow||old&&(BigInt(next.revision)<BigInt(old.revision)||old.intent_id!==null&&next.intent_id!==old.intent_id))throw new Error('SEMANTIC_RUN_SCOPE_INVALID');
    retained.current=structuredClone(next);setValue(next);setFailed(false);
    if(!['completed','failed','cancelled'].includes(next.state))timer=setTimeout(()=>void poll(),3000);
   }catch{if(active&&!controller.signal.aborted){setFailed(true);setValue(null)}}
   finally{if(flight===controller)flight=null}
  }
  const listener=AppState.addEventListener('change',state=>{foreground=state==='active';if(!foreground){stop();setValue(null)}else void poll()});
  void poll();
  return()=>{active=false;stop();listener.remove()};
 },[client,tenant,run,workflow,epoch]);
 const message=value?.state==='failed'?t('semantic_progress_failed_state'):value?.state==='cancelled'?t('semantic_progress_cancelled'):value?.state==='completed'?t('semantic_progress_completed'):t('semantic_review_pending');
 return <View>{value?.intent_id?<Pressable accessibilityRole="button" style={s.button} onPress={()=>onOpen(value.intent_id!)}><Text style={s.buttonText}>{t('semantic_open_review')}</Text></Pressable>:null}{failed?<><Text accessibilityRole="alert" style={s.error}>{t('semantic_progress_failed')}</Text><Pressable accessibilityRole="button" style={s.button} onPress={()=>{setFailed(false);setEpoch(x=>x+1)}}><Text style={s.buttonText}>{t('semantic_results_refresh')}</Text></Pressable></>:!value?.intent_id?<Text accessibilityLiveRegion="polite">{message}</Text>:null}</View>;
}
