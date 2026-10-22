import {Button,Text} from 'react-native-paper';
import {readDeadline} from './read-deadline';
import {useEffect,useMemo,useRef,useState} from 'react';
import {AppState,View} from 'react-native';
import {APIError,type ApiClient,type SemanticRunIntent} from '@photoeditor/api-client';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
type Props={client:Pick<ApiClient,'semanticRunIntent'>;tenant:string;run:string;workflow?:string;onOpen:(intent:string)=>void};
export function SemanticRunLink(props:Props){
 const scope=useMemo(()=>crypto.randomUUID(),[props.client,props.tenant,props.run,props.workflow]);
 return <Scope key={scope} {...props}/>;
}
function Scope({client,tenant,run,workflow,onOpen}:Props){
 const __uiMessage=useLocale().message;

 const {t}=useLocale();
 const[value,setValue]=useState<SemanticRunIntent|null>(null),[failed,setFailed]=useState(false),[nonSemantic,setNonSemantic]=useState(false),[epoch,setEpoch]=useState(0);
 const visible=useRef<SemanticRunIntent|null>(null),observation=useRef<AbortController|null>(null);
 const retained=useRef<SemanticRunIntent|null>(null),stopObservation=useRef<(()=>void)|null>(null);
 const[pending,setPending]=useState(false);
 useEffect(()=>{
  let active=true,foreground=(AppState.currentState??'active')==='active',flight:AbortController|null=null,timer:ReturnType<typeof setTimeout>|undefined;
  const stop=()=>{if(timer!==undefined){clearTimeout(timer);timer=undefined}flight?.abort();flight=null};
  async function poll(){
   if(!active||!foreground||flight)return;
   const controller=new AbortController();flight=controller;observation.current=controller;setPending(true);
   try{
    const next=await readDeadline(controller.signal,signal=>client.semanticRunIntent(tenant,run,signal));
    if(!active||controller.signal.aborted)return;
    const old=retained.current;
    if(next.run_id!==run||workflow!==undefined&&next.workflow_id!==workflow||old&&(next.workflow_id!==old.workflow_id||BigInt(next.revision)<BigInt(old.revision)||old.intent_id!==null&&next.intent_id!==old.intent_id))throw new Error('SEMANTIC_RUN_SCOPE_INVALID');
    const owned=structuredClone(next);retained.current=structuredClone(owned);visible.current=owned;setValue(owned);setFailed(false);setNonSemantic(false);
    if(!['completed','failed','cancelled'].includes(next.state))timer=setTimeout(()=>void poll(),3000);
   }catch(error){if(active&&!controller.signal.aborted){
    if(workflow===undefined&&retained.current===null&&error instanceof APIError&&error.status===404){setFailed(false);visible.current=null;setValue(null);setNonSemantic(true)}
    else{setFailed(true);visible.current=null;setValue(null);setNonSemantic(false)}
   }}
   finally{if(flight===controller){flight=null;if(active)setPending(false)}}
  }
  const stopLocal=()=>{observation.current=null;stop();setPending(false);visible.current=null;setValue(null);setFailed(true);setNonSemantic(false);};stopObservation.current=stopLocal;
  const listener=AppState.addEventListener('change',state=>{foreground=state==='active';if(!foreground){observation.current=null;stop();setPending(false);visible.current=null;setValue(null)}else void poll()});
  void poll();
  return()=>{active=false;visible.current=null;observation.current=null;stop();if(stopObservation.current===stopLocal)stopObservation.current=null;listener.remove()};
 },[client,tenant,run,workflow,epoch]);
 const displayedStop=stopObservation.current,displayedObservation=observation.current;
 const message=value?.state==='failed'?t('semantic_progress_failed_state'):value?.state==='cancelled'?t('semantic_progress_cancelled'):value?.state==='completed'?t('semantic_progress_completed'):t('semantic_review_pending');
 if(nonSemantic)return null;
 return <View>{pending||value&&!(['completed','failed','cancelled'].includes(value.state))?<Button accessibilityRole="button" onPress={()=>{if(displayedStop&&stopObservation.current===displayedStop&&displayedObservation&&observation.current===displayedObservation)displayedStop()}} mode="outlined">{t('read_cancel')}</Button>:null}{value?.intent_id?<Button accessibilityRole="button"  onPress={()=>{if(visible.current===value)onOpen(value.intent_id!)}}   mode="contained" contentStyle={{minHeight:48}}>{t('semantic_open_review')}</Button>:null}{failed?<><Text accessibilityRole="alert" style={s.error}>{t('semantic_progress_failed')}</Text><Button accessibilityRole="button"  onPress={()=>{visible.current=null;observation.current=null;stopObservation.current=null;setFailed(false);setEpoch(x=>x+1)}}   mode="contained" contentStyle={{minHeight:48}}>{t('semantic_results_refresh')}</Button></>:!value?.intent_id?<Text accessibilityLiveRegion="polite">{__uiMessage(message)}</Text>:null}</View>;
}
