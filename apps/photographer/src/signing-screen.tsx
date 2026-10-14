import {useEffect,useMemo,useRef,useState} from 'react';
import {ActivityIndicator,Pressable,SafeAreaView,Text,View} from 'react-native';
import {APIError,type ApiClient,type SigningJobTicket,type SigningPolicyChoice} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import type {SigningAttempt,SigningAttemptStore} from './signing-attempt-store';
import {submitSigning} from './signing-submit';
import {catalogStyle as s} from './catalog-style';
import {useLocale} from './locale-context';
type Props={client:ApiClient;tenant:string;journal:SigningAttemptStore;onCreated:(ticket:SigningJobTicket)=>void};

export function SigningScreen(props:Props){
 const generation=useMemo(()=>crypto.randomUUID(),[props.client,props.journal]);
 return <Scope key={`${generation}:${props.tenant}:${props.journal.key}`} {...props}/>;
}
function Scope({client,tenant,journal,onCreated}:Props){
 const {t}=useLocale(),api=useMemo(()=>new OwnerAPI(client,tenant),[client,tenant]);
 const [choices,setChoices]=useState<SigningPolicyChoice[]>([]),[selected,setSelected]=useState('');
 const [saved,setSaved]=useState<SigningAttempt|null>(null),[loading,setLoading]=useState(true),[busy,setBusy]=useState(false),[error,setError]=useState(false),[denied,setDenied]=useState(false),[epoch,setEpoch]=useState(0);
 const flight=useRef<AbortController|null>(null),alive=useRef(true);
 useEffect(()=>{alive.current=true;return()=>{alive.current=false;flight.current?.abort();};},[]);
 useEffect(()=>{
  const controller=new AbortController();let live=true;setLoading(true);setError(false);setChoices([]);
  void (async()=>{
   // Restore first: an acknowledged attempt can reopen its job even when the
   // operator is temporarily offline or its policy has since rotated.
   const prior=await journal.read();if(!live||controller.signal.aborted)return;
   setSaved(prior);setSelected(prior?.body.signing_policy_id??'');
   if(prior?.ticket)return;
   const policies=await api.signingPolicies(journal.exportID,controller.signal);
   if(!live||controller.signal.aborted)return;
   setChoices(policies);
  })().catch(cause=>{if(live&&!controller.signal.aborted){setError(true);if(cause instanceof APIError&&[401,403,404].includes(cause.status)){setDenied(true);setChoices([]);}}})
   .finally(()=>{if(live&&!controller.signal.aborted)setLoading(false);});
  return()=>{live=false;controller.abort();};
 },[api,journal,epoch]);
 async function submit(){
  if(flight.current||loading||denied||!selected)return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setError(false);
  try{
   const ticket=await submitSigning(journal,api,{signing_policy_id:selected},saved?.key??crypto.randomUUID(),controller.signal);
   if(alive.current&&!controller.signal.aborted)onCreated(ticket);
  }catch(cause){
   if(alive.current&&!controller.signal.aborted){
    setError(true);
    if(cause instanceof APIError&&[401,403,404].includes(cause.status)){setDenied(true);setChoices([]);}
    try{const prior=await journal.read();if(alive.current&&!controller.signal.aborted){setSaved(prior);if(prior)setSelected(prior.body.signing_policy_id);}}catch{if(alive.current)setDenied(true);}
   }
  }finally{if(flight.current===controller)flight.current=null;if(alive.current&&!controller.signal.aborted)setBusy(false);}
 }
 const permitted=saved!==null||choices.some(choice=>choice.id===selected);
 return <SafeAreaView style={s.root}><View style={s.header}>
  <Text style={s.title}>{t('signing_title')}</Text><Text style={s.meta}>{t('signing_description')}</Text>
  {loading||busy?<ActivityIndicator/>:null}
  {error?<Text accessibilityRole="alert" style={s.error}>{t(denied?'signing_denied':'signing_failed')}</Text>:null}
  {!loading&&!denied&&saved?.ticket?<Pressable accessibilityRole="button" disabled={busy} style={s.button} onPress={()=>void submit()}><Text style={s.buttonText}>{t('signing_open_job')}</Text></Pressable>:null}
  {!loading&&!denied&&!saved?.ticket?<>
   {!saved?choices.map(choice=><Pressable key={choice.id} accessibilityRole="radio" accessibilityState={{checked:selected===choice.id,disabled:busy}} disabled={busy} onPress={()=>setSelected(choice.id)} style={s.button}><Text style={s.buttonText}>{t('signing_policy')} {choice.id.slice(0,8)} · {choice.revision}</Text></Pressable>):<Text style={s.meta}>{t('signing_pending')}</Text>}
   {!saved&&choices.length===0?<Text style={s.meta}>{t('signing_unavailable')}</Text>:null}
   <Pressable accessibilityRole="button" disabled={busy||!permitted||!selected} style={s.button} onPress={()=>void submit()}><Text style={s.buttonText}>{t(saved?'signing_retry':'signing_submit')}</Text></Pressable>
   {!busy?<Pressable accessibilityRole="button" style={s.button} onPress={()=>setEpoch(value=>value+1)}><Text style={s.buttonText}>{t('signing_refresh')}</Text></Pressable>:null}
  </>:null}
 </View></SafeAreaView>;
}
