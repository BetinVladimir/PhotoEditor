import {executeInterchangeSnapshot,openInterchangeSnapshot} from './interchange-snapshot-controller';
import {useEffect,useMemo,useRef,useState} from 'react';
import {ActivityIndicator,Pressable,SafeAreaView,Text,View} from 'react-native';
import {APIError,canonicalJSON,type ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import {type InterchangeSnapshotRequest,type InterchangeSnapshotReceipt} from './interchange-export-snapshot';
import type {InterchangeSnapshotAttempt,InterchangeSnapshotAttemptStore} from './interchange-snapshot-attempt-store';
import {catalogStyle as s} from './catalog-style';
import {useLocale} from './locale-context';
type Props={client:ApiClient;tenant:string;project:string;request:InterchangeSnapshotRequest;journal:InterchangeSnapshotAttemptStore;onPrepared:(receipt:InterchangeSnapshotReceipt)=>void};
export function InterchangeSnapshotScreen(props:Props){
 const generation=useMemo(()=>crypto.randomUUID(),[props.client,props.journal]);
 return <Scope key={`${generation}:${props.tenant}:${props.project}:${canonicalJSON(props.request)}`} {...props}/>;
}
function Scope({client,tenant,project,request,journal,onPrepared}:Props){
 const {t}=useLocale(),api=useMemo(()=>new OwnerAPI(client,tenant),[client,tenant]);
 const [ready,setReady]=useState(false),[busy,setBusy]=useState(false),[confirmed,setConfirmed]=useState(false),[denied,setDenied]=useState(false);
 const [record,setRecord]=useState<InterchangeSnapshotAttempt|null>(null),[error,setError]=useState(false);
 const flight=useRef<AbortController|null>(null),alive=useRef(false),attempt=useRef<InterchangeSnapshotAttempt|null>(null),opened=useRef(false);
 function failure(reason:unknown){setError(true);if(reason instanceof APIError&&[401,403,404,410].includes(reason.status))setDenied(true);}
 function stop(){const current=flight.current;if(!current)return;flight.current=null;current.abort();if(alive.current)setBusy(false);}
 async function recover(){
  if(flight.current)return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setError(false);
  try{journal.assertScope(tenant,project,request);const saved=await journal.read();controller.signal.throwIfAborted();
   if(alive.current){attempt.current=saved;setRecord(saved);setReady(true);}
  }catch(reason){if(alive.current&&!controller.signal.aborted)failure(reason);}
  finally{if(flight.current===controller){flight.current=null;if(alive.current)setBusy(false);}}
 }
 useEffect(()=>{alive.current=true;void recover();return()=>{alive.current=false;flight.current?.abort();};},[]);
 async function submit(){
  if(!ready||flight.current||denied||opened.current||(!attempt.current&&!confirmed))return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setError(false);
  try{
   const acknowledged=await executeInterchangeSnapshot({api,journal,tenant,project,request,expectedAttemptKey:attempt.current?.key,signal:controller.signal,newKey:()=>crypto.randomUUID(),onAttempt:owned=>{if(alive.current&&!controller.signal.aborted){attempt.current=owned;setRecord(owned);}}});
   controller.signal.throwIfAborted();if(alive.current){attempt.current=acknowledged;setRecord(acknowledged);}
  }catch(reason){if(alive.current&&!controller.signal.aborted)failure(reason);}
  finally{if(flight.current===controller){flight.current=null;if(alive.current)setBusy(false);}}
 }
 async function open(){
  if(!ready||flight.current||denied||opened.current||!record?.receipt)return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setError(false);
  try{
   await openInterchangeSnapshot({journal,tenant,project,request,expected:record,signal:controller.signal,onPrepared:receipt=>{controller.signal.throwIfAborted();if(alive.current){opened.current=true;onPrepared(receipt);}}});
  }catch(reason){if(alive.current&&!controller.signal.aborted)failure(reason);}
  finally{if(flight.current===controller){flight.current=null;if(alive.current)setBusy(false);}}
 }
 const locked=!ready||busy||denied||record!==null;
 return <SafeAreaView style={s.root}><View style={s.header}>
  <Text style={s.title}>{t('interchange_snapshot_title')}</Text>
  <Text style={s.subtitle}>{t('interchange_snapshot_count')}: {request.items.length}</Text>
  {!record?<Pressable accessibilityRole="checkbox" accessibilityState={{checked:confirmed,disabled:locked}} disabled={locked} onPress={()=>setConfirmed(!confirmed)}><Text style={s.name}>{confirmed?'☑':'☐'} {t('interchange_snapshot_confirm')}</Text></Pressable>:null}
  {busy?<><ActivityIndicator/><Pressable accessibilityRole="button" onPress={stop} style={s.button}><Text style={s.buttonText}>{t('interchange_snapshot_stop')}</Text></Pressable></>:null}{error?<Text accessibilityRole="alert" style={s.error}>{t('interchange_snapshot_failed')}</Text>:null}
  {!ready&&!busy?<Pressable accessibilityRole="button" onPress={()=>void recover()} style={s.button}><Text style={s.buttonText}>{t('interchange_snapshot_restore')}</Text></Pressable>:null}
  {record?.receipt?<Pressable accessibilityRole="button" disabled={busy||denied} onPress={()=>void open()} style={s.button}><Text style={s.buttonText}>{t('interchange_snapshot_open')}</Text></Pressable>:
   <Pressable accessibilityRole="button" disabled={!ready||busy||denied||(!record&&!confirmed)} onPress={()=>void submit()} style={s.button}><Text style={s.buttonText}>{t(record?'interchange_snapshot_retry':'interchange_snapshot_submit')}</Text></Pressable>}
 </View></SafeAreaView>;
}
