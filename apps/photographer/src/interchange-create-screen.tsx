import {executeInterchangeCreate,openInterchangeCreate} from './interchange-create-controller';
import {useEffect,useMemo,useRef,useState} from 'react';
import {ActivityIndicator,Pressable,SafeAreaView,Text,View} from 'react-native';
import {APIError,canonicalJSON,type ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import {type InterchangeCreateRequest,type InterchangeCreateTicket} from './interchange-create';
import type {InterchangeCreateAttempt,InterchangeCreateAttemptStore} from './interchange-create-attempt-store';
import {catalogStyle as s} from './catalog-style';
import {useLocale} from './locale-context';
type Props={client:ApiClient;tenant:string;request:InterchangeCreateRequest;journal:InterchangeCreateAttemptStore;onCreated:(ticket:InterchangeCreateTicket)=>void};
export function InterchangeCreateScreen(props:Props){
 const generation=useMemo(()=>crypto.randomUUID(),[props.client,props.journal]);
 return <Scope key={`${generation}:${props.tenant}:${canonicalJSON(props.request)}`} {...props}/>;
}
function Scope({client,tenant,request,journal,onCreated}:Props){
 const {t}=useLocale(),api=useMemo(()=>new OwnerAPI(client,tenant),[client,tenant]);
 const [ready,setReady]=useState(false),[busy,setBusy]=useState(false),[confirmed,setConfirmed]=useState(false),[denied,setDenied]=useState(false);
 const [record,setRecord]=useState<InterchangeCreateAttempt|null>(null),[error,setError]=useState(false);
 const flight=useRef<AbortController|null>(null),alive=useRef(false),attempt=useRef<InterchangeCreateAttempt|null>(null),opened=useRef(false);
 function failure(reason:unknown){setError(true);if(reason instanceof APIError&&[401,403,404,410].includes(reason.status))setDenied(true);}
 async function recover(){
  if(flight.current)return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setError(false);
  try{journal.assertScope(tenant,request);const saved=await journal.read();controller.signal.throwIfAborted();
   if(alive.current){attempt.current=saved;setRecord(saved);setReady(true);}
  }catch(reason){if(alive.current&&!controller.signal.aborted)failure(reason);}
  finally{if(flight.current===controller)flight.current=null;if(alive.current)setBusy(false);}
 }
 useEffect(()=>{alive.current=true;void recover();return()=>{alive.current=false;flight.current?.abort();};},[]);
 async function submit(){
  if(!ready||flight.current||denied||opened.current||(!attempt.current&&!confirmed))return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setError(false);
  try{
   const acknowledged=await executeInterchangeCreate({api,journal,tenant,request,signal:controller.signal,expectedAttemptKey:attempt.current?.key,newKey:()=>crypto.randomUUID(),onAttempt:owned=>{if(alive.current&&!controller.signal.aborted){attempt.current=owned;setRecord(owned);}}});
   controller.signal.throwIfAborted();
   if(alive.current){attempt.current=acknowledged;setRecord(acknowledged);}
  }catch(reason){if(alive.current&&!controller.signal.aborted)failure(reason);}
  finally{if(flight.current===controller)flight.current=null;if(alive.current)setBusy(false);}
 }
 async function open(){
  if(!ready||flight.current||denied||opened.current||!record?.ticket)return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setError(false);
  try{
   await openInterchangeCreate({journal,tenant,request,expected:record,signal:controller.signal,onCreated:ticket=>{controller.signal.throwIfAborted();if(alive.current){opened.current=true;onCreated(ticket);}}});
  }catch(reason){if(alive.current&&!controller.signal.aborted)failure(reason);}
  finally{if(flight.current===controller)flight.current=null;if(alive.current)setBusy(false);}
 }
 const locked=!ready||busy||denied||record!==null;
 return <SafeAreaView style={s.root}><View style={s.header}>
  <Text style={s.title}>{t('interchange_create_title')}</Text>
  <Text style={s.subtitle}>{t(request.direction==='import'?'interchange_create_import':'interchange_create_export')} · {request.format}</Text>
  {!record?<Pressable accessibilityRole="checkbox" accessibilityState={{checked:confirmed,disabled:locked}} disabled={locked} onPress={()=>setConfirmed(!confirmed)}><Text style={s.name}>{confirmed?'☑':'☐'} {t('interchange_create_confirm')}</Text></Pressable>:null}
  {busy?<><ActivityIndicator/><Pressable accessibilityRole="button" onPress={()=>flight.current?.abort()} style={s.button}><Text style={s.buttonText}>{t('interchange_create_stop')}</Text></Pressable></>:null}{error?<Text accessibilityRole="alert" style={s.error}>{t('interchange_create_failed')}</Text>:null}
  {!ready&&!busy?<Pressable accessibilityRole="button" onPress={()=>void recover()} style={s.button}><Text style={s.buttonText}>{t('interchange_create_restore')}</Text></Pressable>:null}
  {record?.ticket?<Pressable accessibilityRole="button" disabled={busy||denied} onPress={()=>void open()} style={s.button}><Text style={s.buttonText}>{t('interchange_create_open')}</Text></Pressable>:
   <Pressable accessibilityRole="button" disabled={!ready||busy||denied||(!record&&!confirmed)} onPress={()=>void submit()} style={s.button}><Text style={s.buttonText}>{t(record?'interchange_create_retry':'interchange_create_submit')}</Text></Pressable>}
 </View></SafeAreaView>;
}
