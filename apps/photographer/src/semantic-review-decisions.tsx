import {toggleSemanticDecision} from './semantic-decision-selection';
import {useLocale} from './locale-context';
import type en from '../../../locales/en/common.json';
import {useEffect,useMemo,useRef,useState} from 'react';
import {FlatList,Pressable,Text,View} from 'react-native';
import {APIError,type ApiClient,type SemanticTargetResource,type SemanticApplyRequest,type SemanticReviewStatus} from '@photoeditor/api-client';
import {catalogStyle as s} from './catalog-style';
type Client=Pick<ApiClient,'semanticReviewStatus'|'applySemanticReview'>;
type Props={client:Client;tenant:string;intent:string;items:SemanticTargetResource[];onSubmitted:()=>void};
export function SemanticReviewDecisions(props:Props){const scope=useMemo(()=>crypto.randomUUID(),[props.client,props.tenant,props.intent]);return <SemanticDecisionScope key={scope} {...props}/>;}
function SemanticDecisionScope({client,tenant,intent,items,onSubmitted}:Props) {
 const {t}=useLocale();
 const[choices,setChoices]=useState<Record<string,'approve'|'reject'>>({}),[busy,setBusy]=useState(false),[error,setError]=useState<keyof typeof en|''>(''),[retry,setRetry]=useState(false),[status,setStatus]=useState<SemanticReviewStatus|null>(null);
 const liveItems=useRef(items);liveItems.current=items;
 const flight=useRef<AbortController|null>(null),attempt=useRef<{key:string;body:SemanticApplyRequest;workflow:string}|null>(null);
 async function refresh(){
  if(flight.current)return;const controller=new AbortController();flight.current=controller;setBusy(true);setError('');setStatus(null);
  try{const current=await client.semanticReviewStatus(tenant,intent,controller.signal);if(!controller.signal.aborted)setStatus(current)}catch(cause){if(!controller.signal.aborted){if(cause instanceof APIError&&[401,403,404].includes(cause.status)){attempt.current=null;setRetry(false);setChoices({});}setError('semantic_review_load_failed')}}
  finally{if(flight.current===controller){flight.current=null;if(!controller.signal.aborted)setBusy(false)}}
 }
 useEffect(()=>{void refresh();return()=>{flight.current?.abort();flight.current=null}},[client,tenant,intent]);
 useEffect(()=>{const valid=new Set(items.filter(item=>item.data.state==='proposed').map(item=>item.id));setChoices(current=>Object.fromEntries(Object.entries(current).filter(([id])=>valid.has(id))))},[items]);
 function select(id:string,decision:'approve'|'reject'){
  if(busy||flight.current||attempt.current)return;
  if(!choices[id]&&Object.keys(choices).length>=100){setError('semantic_review_limit');return}
  setChoices(current=>toggleSemanticDecision(current,id,decision));
 }
 async function submit(){
  if(flight.current)return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setError('');
  try{
   if(!attempt.current){
    const current=await client.semanticReviewStatus(tenant,intent,controller.signal);
    if(controller.signal.aborted)return;
    if(current.state!=='waiting_review'||!current.plan_sha)throw new APIError(409,'REVISION_CONFLICT');
    const proposed=new Set(liveItems.current.filter(item=>item.data.state==='proposed').map(item=>item.id));
    const target_decisions=Object.entries(choices).filter(([id])=>proposed.has(id)).map(([target_id,decision])=>({target_id,decision}));
    if(!target_decisions.length)return;
    attempt.current={key:crypto.randomUUID(),workflow:current.workflow_id,body:{expected_revision:current.revision,plan_sha:current.plan_sha,target_decisions}};
   }
   const current=attempt.current;
   const result=await client.applySemanticReview(tenant,intent,current.body,current.key,controller.signal);
   if(controller.signal.aborted)return;
   if(result.workflow_id!==current.workflow)throw new Error('SEMANTIC_APPLY_SCOPE_INVALID');
   attempt.current=null;setRetry(false);setChoices({});setStatus(null);onSubmitted();
  }catch(cause){
   if(controller.signal.aborted)return;
   if(cause instanceof APIError&&[400,401,403,404,409,422].includes(cause.status)){attempt.current=null;setRetry(false);setStatus(null);setChoices({});setError('semantic_review_rejected')}
   else{setRetry(attempt.current!==null);setError(attempt.current?'semantic_review_unknown':'semantic_review_read_failed')}
  }finally{if(flight.current===controller){flight.current=null;if(!controller.signal.aborted)setBusy(false)}}
 }
 const editable=status?.state==='waiting_review'&&status.plan_sha!==null;
 return <View>{editable?<FlatList data={items.filter(item=>item.data.state==='proposed')} keyExtractor={item=>item.id} initialNumToRender={10} maxToRenderPerBatch={10} windowSize={5} style={{maxHeight:320}} renderItem={({item})=><View><Text>{t('semantic_review_photo')} {item.data.asset_version_id.slice(0,8)}</Text>{(['approve','reject'] as const).map(decision=><Pressable key={decision} accessibilityRole="button" accessibilityLabel={`${t(decision==='approve'?'semantic_review_approve_access':'semantic_review_reject_access')} ${item.id}`} disabled={busy||retry} onPress={()=>select(item.id,decision)} style={s.button}><Text style={s.buttonText}>{choices[item.id]===decision?t('semantic_review_selected'):''}{t(decision==='approve'?'semantic_review_approve':'semantic_review_reject')}</Text></Pressable>)}</View>}/>:null}{error?<Text accessibilityRole="alert" style={s.error}>{t(error)}</Text>:null}{(editable&&Object.keys(choices).length>0||retry)?<Pressable accessibilityRole="button" disabled={busy} onPress={()=>void submit()} style={s.button}><Text style={s.buttonText}>{t(retry?'semantic_review_retry':'semantic_review_submit')}</Text></Pressable>:null}<Pressable accessibilityRole="button" disabled={busy} onPress={()=>void refresh()} style={s.button}><Text style={s.buttonText}>{t('semantic_review_refresh')}</Text></Pressable></View>;
}
