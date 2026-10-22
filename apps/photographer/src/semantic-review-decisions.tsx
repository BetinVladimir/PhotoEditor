import {Button,Text} from 'react-native-paper';
import {readDeadline} from './read-deadline';
import type {SemanticActionAvailability} from './semantic-kind-availability';
import type {SemanticApplyAttemptStore} from './semantic-apply-attempt-store';
import {SemanticCandidatePreview} from './semantic-candidate-preview';
import {toggleSemanticDecision} from './semantic-decision-selection';
import {useLocale} from './locale-context';
import type en from '../../../locales/en/common.json';
import {useCallback,useEffect,useMemo,useRef,useState} from 'react';
import {AppState,FlatList,View} from 'react-native';
import {APIError,semanticPreviewQuery,semanticPreviewStillCurrent,type SemanticReviewPreview,type ApiClient,type SemanticTargetResource,type SemanticApplyRequest,type SemanticApplyResult,type SemanticReviewStatus} from '@photoeditor/api-client';
import {catalogStyle as s} from './catalog-style';
type Client=Pick<ApiClient,'semanticReviewStatus'|'applySemanticReview'>&Partial<Pick<ApiClient,'semanticReviewPreview'>>;
type Props={client:Client;tenant:string;intent:string;items:SemanticTargetResource[];onSubmitted:()=>void;onPendingChange?:(pending:boolean)=>void;actionAvailable?:SemanticActionAvailability;journalFactory?:(signal:AbortSignal)=>Promise<SemanticApplyAttemptStore>};
export function SemanticReviewDecisions(props:Props){const scope=useMemo(()=>crypto.randomUUID(),[props.client,props.tenant,props.intent,props.journalFactory]);return <SemanticDecisionScope key={scope} {...props}/>;}
function SemanticDecisionScope({client,tenant,intent,items,onSubmitted,onPendingChange,actionAvailable,journalFactory}:Props) {
 const __uiT=useLocale().t;

 const {t}=useLocale();
 const liveAvailability=useRef(actionAvailable);liveAvailability.current=actionAvailable;
 const[choices,setChoices]=useState<Record<string,'approve'|'reject'>>({}),[busy,setBusy]=useState(false),[error,setError]=useState<keyof typeof en|''>(''),[retry,setRetry]=useState(false),[status,setStatus]=useState<SemanticReviewStatus|null>(null);
 const [authorityDenied,setAuthorityDenied]=useState(false);
 const active=useRef((AppState.currentState??'active')==='active');
 const [foreground,setForeground]=useState(active.current);
 const journal=useRef<SemanticApplyAttemptStore|null>(null);
 const acceptedReceipt=useRef<{key:string;result:SemanticApplyResult}|null>(null);
 const [journalReady,setJournalReady]=useState(!journalFactory),[journalFailed,setJournalFailed]=useState(false),[journalRevision,setJournalRevision]=useState(0);
 const liveChoices=useRef(choices);liveChoices.current=choices;
 const[proofs,setProofs]=useState<Record<string,SemanticReviewPreview>>({});
 const liveProofs=useRef(proofs);liveProofs.current=proofs;
 const reviewed=useCallback((id:string,value:SemanticReviewPreview|null)=>{
  const current=liveProofs.current;
  if(value||current[id]){
   const next={...current};
   if(value){
    if(!next[id]&&Object.keys(next).length>=100){const evict=Object.keys(next).find(key=>liveChoices.current[key]!=='approve');if(!evict){setError('semantic_review_limit');return}delete next[evict]}
    next[id]=value;
   }else delete next[id];
   liveProofs.current=next;setProofs(next);
  }
  if(!value)setChoices(current=>{if(current[id]!=='approve')return current;const next={...current};delete next[id];return next});
 },[]);
 const liveItems=useRef(items);liveItems.current=items;
 function canApprove(item:SemanticTargetResource,current:SemanticReviewStatus|null=status){
  try{return !!current&&liveAvailability.current?.(current.kind,'apply')!==false&&semanticPreviewStillCurrent(liveProofs.current[item.id],semanticPreviewQuery(tenant,intent,item,current))}catch{return false;}
 }
 function preview(item:SemanticTargetResource){
  if(!client.semanticReviewPreview||!status)return <Text style={s.meta}>{t('semantic_review_preview_required')}</Text>;
  try{const query=semanticPreviewQuery(tenant,intent,item,status);return <SemanticCandidatePreview client={client as Pick<ApiClient,'semanticReviewPreview'>} query={query} admissionAvailable={actionAvailable?.(status.kind,'preview')} onReady={value=>reviewed(item.id,value)}/>}catch{return <Text style={s.meta}>{t('semantic_review_preview_required')}</Text>}
 }
 const freshAttempt=useRef<{key:string;kind:SemanticReviewStatus['kind']}|null>(null);
 const bootstrap=useRef<AbortController|null>(null),flight=useRef<AbortController|null>(null),attempt=useRef<{key:string;body:SemanticApplyRequest;workflow:string}|null>(null);
 function cancel(){bootstrap.current?.abort();bootstrap.current=null;if(!journalReady){setJournalFailed(true);setError('semantic_review_load_failed');}const controller=flight.current;if(controller){controller.abort();flight.current=null;setBusy(false);setRetry(attempt.current!==null);}}
 function deadline(controller:AbortController){const timer=setTimeout(()=>{if(flight.current===controller){cancel();setError(attempt.current?'semantic_review_unknown':'semantic_review_load_failed');}},15000);const clear=()=>clearTimeout(timer);controller.signal.addEventListener('abort',clear,{once:true});return()=>{clear();controller.signal.removeEventListener('abort',clear);};}
 async function refresh(){
  if(!active.current||flight.current)return;const controller=new AbortController();flight.current=controller;const finishDeadline=deadline(controller);setBusy(true);setError('');setStatus(null);setProofs({});liveProofs.current={};setChoices(current=>Object.fromEntries(Object.entries(current).filter(([,decision])=>decision==='reject')));
  try{const current=await readDeadline(controller.signal,signal=>client.semanticReviewStatus(tenant,intent,signal));if(!controller.signal.aborted){setAuthorityDenied(false);setStatus(current)}}catch(cause){if(!controller.signal.aborted){if(cause instanceof APIError&&[401,403,404,410].includes(cause.status)){setAuthorityDenied(true);if(!journal.current){attempt.current=null;setRetry(false);}setChoices({});}setError('semantic_review_load_failed')}}
  finally{finishDeadline();if(flight.current===controller){flight.current=null;setBusy(false)}}
 }
 useEffect(()=>{
  const controller=new AbortController();bootstrap.current=controller;setJournalFailed(false);setJournalReady(!journalFactory);if(!active.current)return()=>{controller.abort();if(bootstrap.current===controller)bootstrap.current=null;};
  void(async()=>{try{
   if(journalFactory){
    const {store,saved}=await readDeadline(controller.signal,async signal=>{const store=await journalFactory(signal);signal.throwIfAborted();store.assertScope(tenant,intent);const saved=await store.read();signal.throwIfAborted();return {store,saved};});controller.signal.throwIfAborted();journal.current=store;
    if(saved){attempt.current={key:saved.key,body:saved.body,workflow:saved.workflow};setRetry(true);}
   }
   setJournalReady(true);void refresh();
  }catch{if(!controller.signal.aborted){setError('semantic_review_load_failed');setJournalReady(false);setJournalFailed(true);}}finally{if(bootstrap.current===controller)bootstrap.current=null;}})();
  return()=>{controller.abort();if(bootstrap.current===controller)bootstrap.current=null;flight.current?.abort();flight.current=null;journal.current=null;};
 },[client,tenant,intent,journalFactory,journalRevision]);
 useEffect(()=>{
  const valid=new Set(items.filter(item=>item.data.state==='proposed').map(item=>item.id));
  setChoices(current=>Object.fromEntries(Object.entries(current).filter(([id,decision])=>valid.has(id)&&(decision==='reject'||canApprove(items.find(item=>item.id===id)!)))));
  setProofs(current=>{const next=Object.fromEntries(Object.entries(current).filter(([id])=>valid.has(id)&&canApprove(items.find(item=>item.id===id)!)));if(Object.keys(next).length===Object.keys(current).length)return current;liveProofs.current=next;return next});
 },[items,status]);
 useEffect(()=>{
  const expires=Object.values(proofs).flatMap(proof=>[proof.before,proof.after,proof.affected,proof.protected].map(image=>Date.parse(image.expires_at)));
  if(!expires.length)return;
  const timer=setTimeout(()=>{
   const next=Object.fromEntries(Object.entries(liveProofs.current).filter(([,proof])=>[proof.before,proof.after,proof.affected,proof.protected].every(image=>Date.parse(image.expires_at)>Date.now())));
   liveProofs.current=next;setProofs(next);setChoices(current=>Object.fromEntries(Object.entries(current).filter(([id,decision])=>decision==='reject'||!!next[id])));
  },Math.max(1,Math.min(...expires)-Date.now()));
  return()=>clearTimeout(timer);
 },[proofs]);
 useEffect(()=>{
  const subscription=AppState.addEventListener('change',state=>{active.current=state==='active';setForeground(active.current);if(state!=='active'){bootstrap.current?.abort();bootstrap.current=null;journal.current=null;setJournalReady(!journalFactory);setJournalFailed(false);flight.current?.abort();flight.current=null;setBusy(false);setStatus(null);liveProofs.current={};setProofs({});setChoices(current=>Object.fromEntries(Object.entries(current).filter(([,decision])=>decision==='reject')))}else{if(journalFactory)setJournalRevision(value=>value+1);else void refresh()}});
  return()=>subscription.remove();
 },[]);
 function select(id:string,decision:'approve'|'reject'){
  if(!active.current||!journalReady||busy||flight.current||attempt.current)return;
  if(decision==='approve'){const item=liveItems.current.find(value=>value.id===id);if(!item||!canApprove(item))return;}
  if(!choices[id]&&Object.keys(choices).length>=100){setError('semantic_review_limit');return}
  setChoices(current=>toggleSemanticDecision(current,id,decision));
 }
 async function submit(){
  if(!active.current||!journalReady||authorityDenied||flight.current)return;
  const controller=new AbortController();flight.current=controller;const finishDeadline=deadline(controller);setBusy(true);setError('');
  try{
   if(!attempt.current){
    const current=await readDeadline(controller.signal,signal=>client.semanticReviewStatus(tenant,intent,signal));
    if(controller.signal.aborted)return;
    if(current.state!=='waiting_review'||!current.plan_sha)throw new APIError(409,'REVISION_CONFLICT');
    const proposed=new Set(liveItems.current.filter(item=>item.data.state==='proposed').map(item=>item.id));
    const target_decisions:SemanticApplyRequest['target_decisions']=Object.entries(choices).filter(([id])=>proposed.has(id)).map(([target_id,decision])=>{
     if(decision==='reject')return {target_id,decision};
     const item=liveItems.current.find(value=>value.id===target_id),proof=liveProofs.current[target_id];
     if(!item||!proof||!canApprove(item,current))throw new APIError(409,'REVISION_CONFLICT');
     return {target_id,decision,preview:{render:{...proof.render},evidence:{...proof.evidence}}};
    });
    if(!target_decisions.length)return;
    if(target_decisions.some(decision=>decision.decision==='approve'&&!canApprove(liveItems.current.find(item=>item.id===decision.target_id)!,current)))throw new APIError(409,'REVISION_CONFLICT');
    attempt.current={key:crypto.randomUUID(),workflow:current.workflow_id,body:{expected_revision:current.revision,plan_sha:current.plan_sha,target_decisions}};
    freshAttempt.current={key:attempt.current.key,kind:current.kind};
   }
   const current=attempt.current;setRetry(true);
   if(journal.current&&acceptedReceipt.current?.key!==current.key)await journal.current.write({version:1,...current});
   controller.signal.throwIfAborted();if(!active.current)throw new Error('SEMANTIC_APPLY_BACKGROUND');
   // Durable journal IO may outlive the displayed media. Before the first
   // transport, retain the exact still-visible photographer-approved refs.
   // Already-sent uncertain attempts must keep their original replay body/key.
   if(freshAttempt.current?.key===current.key&&current.body.target_decisions.some(value=>{
    if(value.decision!=='approve')return false;
    const item=liveItems.current.find(item=>item.id===value.target_id),proof=liveProofs.current[value.target_id];
    return !item||!proof||!canApprove(item)||!value.preview
     ||proof.render.artifact_id!==value.preview.render.artifact_id||proof.render.sha256!==value.preview.render.sha256
     ||proof.evidence.artifact_id!==value.preview.evidence.artifact_id||proof.evidence.sha256!==value.preview.evidence.sha256;
   }))throw new APIError(409,'REVISION_CONFLICT');
   if(freshAttempt.current?.key===current.key&&current.body.target_decisions.some(value=>value.decision==='approve')
       &&liveAvailability.current?.(freshAttempt.current.kind,'apply')===false)throw new APIError(503,'DEPENDENCY_UNAVAILABLE');
   // Once transport starts the outcome may be uncertain; retries must preserve
   // the same body/key even if deployment availability subsequently changes.
   freshAttempt.current=null;
   const result=acceptedReceipt.current?.key===current.key?acceptedReceipt.current.result
    :await client.applySemanticReview(tenant,intent,current.body,current.key,controller.signal);
   if(controller.signal.aborted)return;
   if(result.workflow_id!==current.workflow)throw new Error('SEMANTIC_APPLY_SCOPE_INVALID');
   // Acknowledgement-marker persistence may succeed before record deletion fails.
   // Retry cleanup with this receipt; never rewrite or resend an acknowledged key.
   acceptedReceipt.current={key:current.key,result};
   if(journal.current)await journal.current.clear(current.key,result);
   // Keep the acknowledged attempt if background interrupts cleanup: foreground
   // retry must still deliver completion to the result screen without resending.
   controller.signal.throwIfAborted();
   attempt.current=null;acceptedReceipt.current=null;setRetry(false);
   setChoices({});setStatus(null);onSubmitted();
  }catch(cause){
   if(controller.signal.aborted)return;
   if(cause instanceof APIError&&[401,403,404,410].includes(cause.status)){setAuthorityDenied(true);setStatus(null);liveProofs.current={};setProofs({});setChoices({});}
   if(!journal.current&&cause instanceof APIError&&[400,401,403,404,409,410,422].includes(cause.status)){attempt.current=null;setRetry(false);setStatus(null);setChoices({});setError('semantic_review_rejected')}
   else{setRetry(attempt.current!==null);setError(attempt.current?'semantic_review_unknown':'semantic_review_read_failed')}
  }finally{finishDeadline();if(flight.current===controller){flight.current=null;setBusy(false)}}
 }
 useEffect(()=>{onPendingChange?.(!foreground||!journalReady||journalFailed||busy||retry||Object.keys(choices).length>0)},[onPendingChange,foreground,journalReady,journalFailed,busy,retry,choices]);
 const editable=foreground&&!authorityDenied&&journalReady&&status?.state==='waiting_review'&&status.plan_sha!==null;
 return <View>{(busy||!journalReady&&!journalFailed)?<Button accessibilityRole="button" onPress={cancel} mode="outlined">{t('read_cancel')}</Button>:null}{editable?<FlatList data={items.filter(item=>item.data.state==='proposed')} keyExtractor={item=>item.id} initialNumToRender={10} maxToRenderPerBatch={10} windowSize={5} style={{maxHeight:320}} renderItem={({item})=><View><Text>{t('semantic_review_photo')} {item.data.asset_version_id.slice(0,8)}</Text>{preview(item)}{(['approve','reject'] as const).map(decision=><Button key={decision} accessibilityRole="button" accessibilityLabel={__uiT('ui_8cbaa55a52ef', {p0:(t(decision==='approve'?'semantic_review_approve_access':'semantic_review_reject_access')),p1:(item.id)})} disabled={busy||retry||decision==='approve'&&!canApprove(item)} onPress={()=>select(item.id,decision)}    mode="contained" contentStyle={{minHeight:48}}>{choices[item.id]===decision?t('semantic_review_selected'):''}{t(decision==='approve'?'semantic_review_approve':'semantic_review_reject')}</Button>)}</View>}/>:null}{journalFailed?<Button accessibilityRole="button" disabled={busy||!foreground} onPress={()=>setJournalRevision(value=>value+1)}    mode="contained" contentStyle={{minHeight:48}}>{t('semantic_review_reload_journal')}</Button>:null}{error?<Text accessibilityRole="alert" style={s.error}>{t(error)}</Text>:null}{(editable&&Object.keys(choices).length>0||retry)?<Button accessibilityRole="button" disabled={busy||!foreground||!journalReady||authorityDenied} onPress={()=>void submit()}    mode="contained" contentStyle={{minHeight:48}}>{t(retry?'semantic_review_retry':'semantic_review_submit')}</Button>:null}<Button accessibilityRole="button" disabled={busy||!foreground||!journalReady} onPress={()=>void refresh()}    mode="contained" contentStyle={{minHeight:48}}>{t('semantic_review_refresh')}</Button></View>;
}
