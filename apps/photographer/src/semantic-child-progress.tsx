import {subscribeSemanticChild,retrySemanticChild} from './semantic-child-channel';
import {useLocale} from './locale-context';
import type en from '../../../locales/en/common.json';
import {useEffect,useMemo,useRef,useState} from 'react';
import {AppState,Pressable,Text,View} from 'react-native';
import type {ApiClient,SemanticChildStatus} from '@photoeditor/api-client';
type Props={client:Pick<ApiClient,'semanticChildStatus'>;tenant:string;run:string;onTerminal:()=>void};
export function SemanticChildProgress(props:Props){const scope=useMemo(()=>crypto.randomUUID(),[props.client,props.tenant,props.run]);return <SemanticChildProgressScope key={scope} {...props}/>;}
function SemanticChildProgressScope({client,tenant,run,onTerminal}:Props) {
 const {t}=useLocale();
 const[status,setStatus]=useState<SemanticChildStatus|null>(null),[error,setError]=useState('');
 const callback=useRef(onTerminal);callback.current=onTerminal;
 useEffect(()=>{
  let unsubscribe:(()=>void)|undefined;
  const change=(state:string)=>{
   if(state==='active'){
    if(!unsubscribe)unsubscribe=subscribeSemanticChild(client,tenant,run,current=>{setStatus(current.status);setError(current.failed?'semantic_progress_failed':'');},()=>callback.current);
   }else{unsubscribe?.();unsubscribe=undefined;setStatus(null);setError('');}
  };
  change(AppState.currentState??'active');
  const listener=AppState.addEventListener('change',change);
  return()=>{listener.remove();unsubscribe?.();};
 },[client,tenant,run]);
 return <View>{status?<Text>{t('semantic_progress_summary').replace('{state}',t(progressKeys[status.status])).replace('{percent}',String(Math.round(status.progress*100)))}</Text>:null}{error?<View><Text accessibilityRole="alert">{t('semantic_progress_failed')}</Text><Pressable accessibilityRole="button" onPress={()=>retrySemanticChild(client,tenant,run)}><Text>{t('retry')}</Text></Pressable></View>:null}</View>;
}

const progressKeys:Record<SemanticChildStatus['status'],keyof typeof en>={"draft": "semantic_progress_draft", "queued": "semantic_progress_queued", "running": "semantic_progress_running", "waiting_review": "semantic_progress_waiting_review", "cancelling": "semantic_progress_cancelling", "cancelled": "semantic_progress_cancelled", "completed": "semantic_progress_completed", "completed_with_errors": "semantic_progress_completed_with_errors", "failed": "semantic_progress_failed_state"};
