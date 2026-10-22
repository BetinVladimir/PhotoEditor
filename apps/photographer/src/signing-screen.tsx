import {ActivityIndicator,Button,Text,TouchableRipple} from 'react-native-paper';
import {useEffect,useMemo,useRef,useState} from 'react';
import {SafeAreaView,View} from 'react-native';
import {APIError,freezeSignCreate,type ApiClient,type SigningJobTicket,type SigningPolicyChoice} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import type {SigningAttempt,SigningAttemptStore} from './signing-attempt-store';
import {submitSigning} from './signing-submit';
import {readDeadline} from './read-deadline';
import {useReviewForeground} from './use-review-foreground';
import {catalogStyle as s} from './catalog-style';
import {useLocale} from './locale-context';
type Props={client:ApiClient;tenant:string;journal:SigningAttemptStore;onCreated:(ticket:SigningJobTicket)=>void};
export function SigningScreen(props:Props){
 const foreground=useReviewForeground(),generation=useMemo(()=>crypto.randomUUID(),[props.client,props.journal]);
 if(!foreground.active)return null;
 return <Scope key={`${generation}:${props.tenant}:${props.journal.key}:${foreground.generation}`} {...props}/>;
}
function Scope({client,tenant,journal,onCreated}:Props){
 const {t}=useLocale(),api=useMemo(()=>new OwnerAPI(client,tenant),[client,tenant]);
 const [choices,setChoices]=useState<SigningPolicyChoice[]>([]),[selected,setSelected]=useState('');
 const [saved,setSaved]=useState<SigningAttempt|null>(null),[loading,setLoading]=useState(true),[busy,setBusy]=useState(false),[error,setError]=useState(false),[denied,setDenied]=useState(false),[epoch,setEpoch]=useState(0);
 const flight=useRef<AbortController|null>(null),read=useRef<AbortController|null>(null),alive=useRef(true),attempt=useRef<SigningAttempt|null>(null);
 const current=(controller:AbortController)=>alive.current&&flight.current===controller&&!controller.signal.aborted;
 function refusal(cause:unknown){return cause instanceof APIError&&[401,403,404,410].includes(cause.status)||cause instanceof Error&&['SIGNING_JOURNAL_EXPIRED','SIGNING_JOURNAL_INVALID','SIGNING_JOURNAL_CONFLICT'].includes(cause.message)}
 useEffect(()=>{alive.current=true;return()=>{alive.current=false;read.current?.abort();flight.current?.abort();read.current=null;flight.current=null;};},[]);
 useEffect(()=>{
  const controller=new AbortController();read.current=controller;setLoading(true);setError(false);setDenied(false);setChoices([]);
  void readDeadline(controller.signal,async signal=>{
   const prior=await journal.read();signal.throwIfAborted();
   if(!alive.current||read.current!==controller)return;
   // A locally frozen command whose first durable write failed is still the
   // retry command. A refresh must not replace it with another policy/key.
   const owned=prior??attempt.current;attempt.current=owned;setSaved(owned);setSelected(owned?.body.signing_policy_id??'');
   if(owned?.ticket)return;
   const policies=await api.signingPolicies(journal.exportID,signal);signal.throwIfAborted();
   if(alive.current&&read.current===controller)setChoices(policies);
  }).catch(cause=>{if(alive.current&&read.current===controller&&!controller.signal.aborted){setError(true);if(refusal(cause)){setDenied(true);setChoices([]);}}})
   .finally(()=>{if(alive.current&&read.current===controller){read.current=null;setLoading(false);}});
  return()=>{controller.abort();if(read.current===controller)read.current=null;};
 },[api,journal,epoch]);
 function cancel(){const controller=flight.current;if(controller){controller.abort();flight.current=null;setBusy(false);}const initial=read.current;if(initial){initial.abort();read.current=null;setLoading(false);}setError(true);}
 async function submit(){
  if(flight.current||loading||denied||!selected||saved===null&&!choices.some(choice=>choice.id===selected))return;
  // Freeze before any storage or transport await, including the first read.
  const frozen=attempt.current??saved??{version:1 as const,key:crypto.randomUUID(),body:freezeSignCreate({signing_policy_id:selected}),ticket:null};
  attempt.current=frozen;setSaved(frozen);setSelected(frozen.body.signing_policy_id);
  const controller=new AbortController();flight.current=controller;setBusy(true);setError(false);
  const timer=setTimeout(()=>{if(current(controller)){controller.abort();flight.current=null;setBusy(false);setError(true);}},15000);
  try{
   const ticket=await readDeadline(controller.signal,signal=>submitSigning(journal,api,frozen.body,frozen.key,signal),15000);
   if(!current(controller))return;
   const accepted={...frozen,ticket};attempt.current=accepted;setSaved(accepted);onCreated(ticket);
  }catch(cause){
   if(!current(controller))return;
   setError(true);
   if(refusal(cause)){setDenied(true);setChoices([]);return;}
   // Recovery is readonly and independently bounded. Missing storage never
   // erases the already frozen in-memory request after an uncertain write.
   try{const prior=await readDeadline(controller.signal,signal=>journal.read().then(value=>{signal.throwIfAborted();return value}));if(current(controller)&&prior){attempt.current=prior;setSaved(prior);setSelected(prior.body.signing_policy_id);}}
   catch(recovery){if(current(controller)&&refusal(recovery)){setDenied(true);setChoices([]);}}
  }finally{clearTimeout(timer);if(flight.current===controller){flight.current=null;if(alive.current)setBusy(false);}}
 }
 const permitted=saved!==null||choices.some(choice=>choice.id===selected);
 return <SafeAreaView style={s.root}><View style={s.header}>
  <Text style={s.title}>{t('signing_title')}</Text><Text style={s.meta}>{t('signing_description')}</Text>
  {loading||busy?<><ActivityIndicator/><Button accessibilityRole="button" onPress={cancel}>{t('read_cancel')}</Button></>:null}
  {error?<Text accessibilityRole="alert" style={s.error}>{t(denied?'signing_denied':'signing_failed')}</Text>:null}
  {!loading&&!busy&&error?<Button  accessibilityRole="button"  onPress={()=>setEpoch(value=>value+1)} mode="contained" contentStyle={{minHeight:48}}>{t('read_retry')}</Button>:null}
  {!loading&&!denied&&saved?.ticket?<Button  accessibilityRole="button" disabled={busy}  onPress={()=>void submit()} mode="contained" contentStyle={{minHeight:48}}>{t('signing_open_job')}</Button>:null}
  {!loading&&!denied&&!saved?.ticket?<>
   {!saved?choices.map(choice=><TouchableRipple  key={choice.id} accessibilityRole="radio" accessibilityState={{checked:selected===choice.id,disabled:busy}} aria-checked={selected===choice.id} disabled={busy} onPress={()=>setSelected(choice.id)} style={s.button}><Text style={s.buttonText}>{t('signing_policy')} {choice.id.slice(0,8)} · {choice.revision}</Text></TouchableRipple>):<Text style={s.meta}>{t('signing_pending')}</Text>}
   {!saved&&choices.length===0?<Text style={s.meta}>{t('signing_unavailable')}</Text>:null}
   <Button  accessibilityRole="button" disabled={busy||!permitted||!selected}  onPress={()=>void submit()} mode="contained" contentStyle={{minHeight:48}}>{t(saved?'signing_retry':'signing_submit')}</Button>
   {!busy?<Button  accessibilityRole="button"  onPress={()=>setEpoch(value=>value+1)} mode="contained" contentStyle={{minHeight:48}}>{t('signing_refresh')}</Button>:null}
  </>:null}
 </View></SafeAreaView>;
}
