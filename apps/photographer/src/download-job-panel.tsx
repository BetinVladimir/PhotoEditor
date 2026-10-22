import {Button,Text} from 'react-native-paper';
import {readDeadline} from './read-deadline';
import {useLocale} from './locale-context';
import {downloadFailureItems} from './download-failure';
import {APIError} from "@photoeditor/api-client";
import {useEffect,useRef,useState} from 'react';
import {AppState,Linking,View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import {downloadJobTerminal,type DownloadJob} from './download-job';
export function DownloadJobPanel({api,id}:{api:OwnerAPI;id:string}){
 const __r2Value=useLocale().value;const __r2T=useLocale().t;

 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;

 const {t}=useLocale();
 const [job,setJob]=useState<DownloadJob|null>(null),[notice,setNotice]=useState(''),[busy,setBusy]=useState(false),[reload,setReload]=useState(0);
 const denied=useRef(false);
 const inaccessible=(error:unknown)=>error instanceof APIError&&([401,403,404,410].includes(error.status)||['DOWNLOAD_REVOKED','DOWNLOAD_FORBIDDEN'].includes(error.code));
 const refuse=()=>{denied.current=true;pollFlight.current?.abort();flight.current?.abort();flight.current=null;setBusy(false);setJob(null);setNotice('Download is no longer available')};
 const active=useRef(true),flight=useRef<AbortController|null>(null),pollFlight=useRef<AbortController|null>(null);
 useEffect(()=>{active.current=true;const listener=AppState.addEventListener('change',state=>{if(state!=='active'){pollFlight.current?.abort();setJob(null);flight.current?.abort();flight.current=null;setBusy(false)}else setReload(v=>v+1)});return()=>{active.current=false;flight.current?.abort();listener.remove()}},[api,id]);
 useEffect(()=>{
  const controller=new AbortController();pollFlight.current=controller;setJob(null);let timer:ReturnType<typeof setTimeout>|undefined;let failures=0;
  const poll=async()=>{
   if(AppState.currentState!=='active')return;
   try{const value=await readDeadline(controller.signal,signal=>api.downloadJob(id,signal));if(controller.signal.aborted||!active.current||AppState.currentState!=='active')return;denied.current=false;setJob(value);setNotice('');failures=0;if(downloadJobTerminal(value.status))return}
   catch(error){if(controller.signal.aborted||!active.current||AppState.currentState!=='active')return;if(inaccessible(error)){refuse();return}setNotice('Unable to update download status');failures++}
   timer=setTimeout(()=>void poll(),Math.min(15000,1500*2**Math.min(failures,4)));
  };
  void poll();return()=>{controller.abort();if(pollFlight.current===controller)pollFlight.current=null;clearTimeout(timer)};
 },[api,id,reload]);
 async function perform(volume?:number){
  if(denied.current||flight.current||AppState.currentState!=='active')return;
  if(volume!==undefined&&(!job||job.status!=='ready'||!job.volumes.some(v=>v.number===volume&&v.status==='ready')))return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setNotice('');
  try{
   if(volume===undefined){const value=await api.cancelDownloadJob(id,controller.signal);if(active.current&&!controller.signal.aborted){setJob(value);setReload(v=>v+1)}}
   else{const url=await readDeadline(controller.signal,signal=>api.downloadJobVolume(id,volume,signal));if(active.current&&!controller.signal.aborted&&AppState.currentState==='active')await Linking.openURL(url)}
  }catch(error){if(active.current&&!controller.signal.aborted){if(inaccessible(error))refuse();else setNotice('Unable to complete download request')}}
  finally{if(flight.current===controller){flight.current=null;if(active.current)setBusy(false)}}
 }
 function stop(){flight.current?.abort();flight.current=null;setBusy(false);setNotice('Unable to complete download request')}
 useEffect(()=>{if(!busy)return;const timer=setTimeout(stop,15000);return()=>clearTimeout(timer)},[busy]);
 return <View><Text>{__uiT('ui_7502fd33d524')}{' '}{__r2Value(job?.status)||__r2T('ui_b4a070a2d341')}</Text>{job?<Text>{job.files_done}/{job.files_total} · {job.bytes_written} {__uiT('ui_277089d91c0b')}</Text>:null}
  {job?.status==='failed'?downloadFailureItems(job.error).map(item=><Text key={item.ordinal} accessibilityRole="alert">{item.ordinal+1}: {item.code}</Text>):null}
  {busy?<Button accessibilityRole="button" onPress={stop}>{t('read_cancel')}</Button>:null}{notice?<><Text accessibilityRole="alert">{__uiMessage(notice)}</Text><Button disabled={busy} onPress={()=>setReload(v=>v+1)}>{__uiT('ui_942087cc2d41')}</Button></>:null}
  {job?.status==='ready'?job.volumes.filter(v=>v.status==='ready').map(v=><Button key={v.number} disabled={busy} onPress={()=>void perform(v.number)}>{__uiT('ui_d6eafe823591')}{' '}{v.number}</Button>):null}
  {job&&!downloadJobTerminal(job.status)?<Button disabled={busy||job.status==='cancelling'} onPress={()=>void perform()}>{__uiT('ui_de5b23e69cd1')}</Button>:null}
 </View>;
}
