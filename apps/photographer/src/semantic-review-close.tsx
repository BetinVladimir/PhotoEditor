import {useLocale} from './locale-context';
import type en from '../../../locales/en/common.json';
import {useEffect,useMemo,useRef,useState} from 'react';
import {Pressable,Text,View} from 'react-native';
import {APIError,type ApiClient,type SemanticReviewStatus,type SemanticReviewClose} from '@photoeditor/api-client';
import {catalogStyle as s} from './catalog-style';
type Client=Pick<ApiClient,'semanticReviewStatus'|'closeSemanticReview'>;
type Props={client:Client;tenant:string;intent:string};
export function SemanticReviewCloseControl(props:Props){const scope=useMemo(()=>crypto.randomUUID(),[props.client,props.tenant,props.intent]);return <SemanticCloseScope key={scope} {...props}/>;}
function SemanticCloseScope({client,tenant,intent}:Props) {
 const {t}=useLocale();
 const[status,setStatus]=useState<SemanticReviewStatus|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState<keyof typeof en|''>(''),[retry,setRetry]=useState(false);
 const flight=useRef<AbortController|null>(null);
 const attempt=useRef<{key:string;body:SemanticReviewClose;run:string;workflow:string}|null>(null);
 async function refresh(){
  if(flight.current)return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setStatus(null);setError('');
  try{const current=await client.semanticReviewStatus(tenant,intent,controller.signal);if(controller.signal.aborted)return;setStatus(current);if(current.state==='completed'){attempt.current=null;setRetry(false)}}
  catch(cause){if(!controller.signal.aborted){if(cause instanceof APIError&&[401,403,404].includes(cause.status)){attempt.current=null;setRetry(false);}setError('semantic_close_load_failed')}}
  finally{if(flight.current===controller){flight.current=null;if(!controller.signal.aborted)setBusy(false)}}
 }
 async function close(){
  if(flight.current)return;
  if(!attempt.current){if(!status?.can_close||!status.plan_sha)return;attempt.current={key:crypto.randomUUID(),run:status.run_id,workflow:status.workflow_id,body:{expected_revision:status.revision,plan_sha:status.plan_sha}}}
  const current=attempt.current;
  const controller=new AbortController();flight.current=controller;setBusy(true);setError('');
  try{
   const closed=await client.closeSemanticReview(tenant,intent,current.body,current.key,controller.signal);
   if(controller.signal.aborted)return;
   if(closed.run_id!==current.run||closed.workflow_id!==current.workflow)throw new Error("SEMANTIC_CLOSE_SCOPE_INVALID");
   setStatus({run_id:closed.run_id,workflow_id:closed.workflow_id,revision:closed.revision,state:'completed',plan_sha:null,remaining:0,can_close:false});attempt.current=null;setRetry(false);
  }catch(cause){
   if(controller.signal.aborted)return;
   if(cause instanceof APIError&&[400,401,403,404,409,422].includes(cause.status)){attempt.current=null;setRetry(false);setStatus(null);setError('semantic_close_rejected')}
   else{setRetry(true);setError('semantic_close_unknown')}
  }finally{if(flight.current===controller){flight.current=null;if(!controller.signal.aborted)setBusy(false)}}
 }
 useEffect(()=>{void refresh();return()=>{flight.current?.abort();flight.current=null}},[client,tenant,intent]);
 return <View>{status?.state==='completed'?<Text>{t('semantic_close_completed')}</Text>:status?<Text>{t('semantic_close_remaining').replace('{count}',String(status.remaining))}</Text>:null}{error?<Text accessibilityRole="alert" style={s.error}>{t(error)}</Text>:null}{(status?.can_close||retry)?<Pressable accessibilityRole="button" disabled={busy} onPress={()=>void close()} style={s.button}><Text style={s.buttonText}>{t(retry?'semantic_close_retry':'semantic_close_action')}</Text></Pressable>:null}<Pressable accessibilityRole="button" disabled={busy} onPress={()=>void refresh()} style={s.button}><Text style={s.buttonText}>{t('semantic_close_refresh')}</Text></Pressable></View>;
}
