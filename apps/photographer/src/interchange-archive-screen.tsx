import {ActivityIndicator,Button,Text,TouchableRipple} from 'react-native-paper';
import {releaseArchiveSource} from './interchange-archive-source-lifecycle';
import {useEffect,useMemo,useRef,useState} from 'react';
import {SafeAreaView,View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {APIError,type ApiClient,type UploadSource} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import {InterchangeArchiveAttemptStore} from './interchange-archive-attempt-store';
import {interchangeArchiveSource} from './interchange-archive-source';
import {runArchiveUpload,checkArchiveAttempt,abortArchiveUpload,resetAbortedArchiveUpload,openReadyArchiveUpload} from './interchange-archive-controller';
import type {ArchiveStatus,ArchiveUploadRequest} from './interchange-archive-upload';
import {catalogStyle as s} from './catalog-style';
import {useLocale} from './locale-context';
import {useReviewForeground} from './use-review-foreground';
export type ArchivePicker=(signal:AbortSignal,onHash:(done:number,total:number)=>void)=>Promise<UploadSource|null>;
export const pickWebArchive:ArchivePicker=async(signal,onHash)=>{
 signal.throwIfAborted();if(typeof document==='undefined')throw new Error('INTERCHANGE_ARCHIVE_PICKER_UNAVAILABLE');
 const file=await new Promise<File|null>((resolve,reject)=>{
  const input=document.createElement('input');input.type='file';input.accept='.zip,application/zip';input.multiple=false;
  const done=(file:File|null)=>{signal.removeEventListener('abort',abort);resolve(file);};
  const abort=()=>{input.onchange=null;input.oncancel=null;reject(signal.reason);};signal.addEventListener('abort',abort,{once:true});
  input.onchange=()=>done(input.files?.[0]??null);input.oncancel=()=>done(null);input.click();
 });signal.throwIfAborted();return file?interchangeArchiveSource(file,signal,onHash):null;
};
type Props={client:ApiClient;origin:string;user:string;tenant:string;project:string;picker?:ArchivePicker;onReady:(artifact:{artifact_id:string;sha256:string})=>void};
export function InterchangeArchiveScreen(props:Props){
 const foreground=useReviewForeground(),generation=useMemo(()=>crypto.randomUUID(),[props.client,props.picker]);
 if(!foreground.active)return null;
 return <Scope key={JSON.stringify([generation,foreground.generation,props.origin,props.user,props.tenant,props.project])} {...props}/>;
}
function Scope({client,origin,user,tenant,project,picker=pickWebArchive,onReady}:Props){
 const {t}=useLocale(),api=useMemo(()=>new OwnerAPI(client,tenant),[client,tenant]);
 const [selected,setSelected]=useState<{source:UploadSource;request:ArchiveUploadRequest;journal:InterchangeArchiveAttemptStore}|null>(null);
 const [confirmAbort,setConfirmAbort]=useState(false);
 const [hasAttempt,setHasAttempt]=useState(false);
 const [hasReceipt,setHasReceipt]=useState(false);
 const [busy,setBusy]=useState(false),[error,setError]=useState(false),[reconciliation,setReconciliation]=useState(false),[denied,setDenied]=useState(false),[progress,setProgress]=useState(''),[status,setStatus]=useState<ArchiveStatus|null>(null);
 const ownedSource=useRef<UploadSource|null>(null);
 const releaseSelected=()=>{if(ownedSource.current){releaseArchiveSource(ownedSource.current);ownedSource.current=null;}};
 const flight=useRef<AbortController|null>(null),alive=useRef(true),opened=useRef(false);
 useEffect(()=>{alive.current=true;return()=>{alive.current=false;flight.current?.abort();try{releaseSelected();}catch{/* OS cache eviction may retry on the next selection. */}};},[]);
 function stop(showError=false){const controller=flight.current;if(!controller)return;controller.abort();flight.current=null;setBusy(false);setError(showError);}
 // Bound inactivity, rather than total upload duration: progress keeps large transfers alive.
 useEffect(()=>{if(!busy)return;const timer=setTimeout(()=>stop(true),30000);return()=>clearTimeout(timer);},[busy,progress]);
 const failed=(reason:unknown)=>{setError(true);setReconciliation(reason instanceof APIError&&reason.code==='ARCHIVE_BEGIN_RECONCILIATION_REQUIRED');if(reason instanceof APIError&&[401,403,404,410].includes(reason.status))setDenied(true);};
 async function pick(){
  if(flight.current||denied)return;const c=new AbortController();flight.current=c;setBusy(true);setError(false);setReconciliation(false);
  let pickedSource:UploadSource|null=null;
  try{const source=await picker(c.signal,(done,total)=>{if(alive.current&&!c.signal.aborted)setProgress(`${done} / ${total}`);});pickedSource=source;c.signal.throwIfAborted();
   if(source){const request={filename:source.name,bytes:source.size,sha256:source.sha256},journal=new InterchangeArchiveAttemptStore(AsyncStorage,origin,user,tenant,project,request);const existing=await journal.read();c.signal.throwIfAborted();releaseSelected();ownedSource.current=source;pickedSource=null;setSelected({source,request,journal});setHasAttempt(existing!==null);setHasReceipt(existing?.checkpoint!==null&&existing!==null);setConfirmAbort(false);setStatus(null);setProgress('');}
  }catch(reason){if(pickedSource){try{releaseArchiveSource(pickedSource);}catch{/* Preserve the original preparation error. */}}if(alive.current&&!c.signal.aborted)failed(reason);}finally{if(flight.current===c){flight.current=null;if(alive.current)setBusy(false);}}
 }
 async function upload(){
  if(!selected||flight.current||denied||opened.current)return;const c=new AbortController();flight.current=c;setBusy(true);setError(false);setReconciliation(false);
  try{const next=await runArchiveUpload({api,journal:selected.journal,tenant,project,request:selected.request,source:selected.source,signal:c.signal,newKey:()=>crypto.randomUUID(),onProgress:(done,total)=>{if(alive.current&&!c.signal.aborted)setProgress(`${done} / ${total}`);}});c.signal.throwIfAborted();setHasAttempt(true);setHasReceipt(true);setStatus(next);
  }catch(reason){if(alive.current&&!c.signal.aborted){try{const saved=await selected.journal.read();c.signal.throwIfAborted();if(flight.current!==c)return;setHasAttempt(saved!==null);setHasReceipt(saved?.checkpoint!==null&&saved!==null);}catch{/* Preserve the upload error. */}if(alive.current&&!c.signal.aborted&&flight.current===c)failed(reason);}}finally{if(flight.current===c){flight.current=null;if(alive.current)setBusy(false);}}
 }
 async function check(){
  if(!selected||!hasAttempt||flight.current||denied||opened.current)return;const c=new AbortController();flight.current=c;setBusy(true);setError(false);
  try{const next=await checkArchiveAttempt({api,journal:selected.journal,user,tenant,project,request:selected.request,signal:c.signal});c.signal.throwIfAborted();setStatus(next);setReconciliation(next.begin_recovery==='reconciliation_required');}
  catch(reason){if(alive.current&&!c.signal.aborted)failed(reason);}finally{if(flight.current===c){flight.current=null;if(alive.current)setBusy(false);}}
 }
 async function cancelUpload(){
  if(!selected||!confirmAbort||flight.current||denied||opened.current)return;const c=new AbortController();flight.current=c;setBusy(true);setError(false);
  try{const next=await abortArchiveUpload({api,journal:selected.journal,tenant,project,request:selected.request,signal:c.signal});c.signal.throwIfAborted();setStatus(next);setConfirmAbort(false);
  }catch(reason){if(alive.current&&!c.signal.aborted)failed(reason);}finally{if(flight.current===c){flight.current=null;if(alive.current)setBusy(false);}}
 }
 async function resetAborted(){
  if(!selected||status?.state!=='aborted'||flight.current||denied||opened.current)return;const c=new AbortController();flight.current=c;setBusy(true);setError(false);
  try{await resetAbortedArchiveUpload({api,journal:selected.journal,tenant,project,request:selected.request,signal:c.signal});c.signal.throwIfAborted();setSelected(null);setHasAttempt(false);setHasReceipt(false);setStatus(null);setConfirmAbort(false);setProgress('');releaseSelected();}
  catch(reason){if(alive.current&&!c.signal.aborted)failed(reason);}finally{if(flight.current===c){flight.current=null;if(alive.current)setBusy(false);}}
 }
 async function open(){
  if(!selected||!status?.ready||flight.current||denied||opened.current)return;const c=new AbortController();flight.current=c;setBusy(true);setError(false);
  try{const fresh=await openReadyArchiveUpload({api,journal:selected.journal,tenant,project,request:selected.request,signal:c.signal,onStatus:fresh=>{if(alive.current&&!c.signal.aborted)setStatus(fresh);},onReady:artifact=>{c.signal.throwIfAborted();if(!alive.current)throw new Error('INTERCHANGE_ARCHIVE_JOURNAL_CONFLICT');opened.current=true;onReady(artifact);}});c.signal.throwIfAborted();if(alive.current)setStatus(fresh);
  }catch(reason){if(alive.current&&!c.signal.aborted)failed(reason);}finally{if(flight.current===c){flight.current=null;if(alive.current)setBusy(false);}}
 }
 return <SafeAreaView style={s.root}><View style={s.header}>
  <Text style={s.title}>{t('interchange_archive_title')}</Text>
  <Button  accessibilityRole="button" disabled={busy||denied} onPress={()=>void pick()}  mode="contained" contentStyle={{minHeight:48}}>{t('interchange_archive_pick')}</Button>
  {selected?<Text style={s.subtitle}>{selected.source.name}</Text>:null}{progress?<Text>{progress}</Text>:null}
  {busy?<><ActivityIndicator/><Button  accessibilityRole="button" onPress={()=>stop()}  mode="contained" contentStyle={{minHeight:48}}>{t('interchange_archive_stop')}</Button></>:null}
  {error?<Text accessibilityRole="alert" style={s.error}>{t(reconciliation?'interchange_archive_reconciliation':'interchange_archive_failed')}</Text>:null}
  {status?<Text>{t(status.begin_recovery==='reconciliation_required'?'interchange_archive_reconciliation':status.ready?'interchange_archive_ready':status.state==='aborted'?'interchange_archive_aborted':status.state==='aborting'?'interchange_archive_aborting':status.state==='failed'?'interchange_archive_failed':'interchange_archive_pending')}</Text>:null}
  {selected?<Button  accessibilityRole="button" disabled={busy||denied||opened.current} onPress={()=>void upload()}  mode="contained" contentStyle={{minHeight:48}}>{t('interchange_archive_upload')}</Button>:null}
  {selected&&hasAttempt?<Button  accessibilityRole="button" disabled={busy||denied||opened.current} onPress={()=>void check()}  mode="contained" contentStyle={{minHeight:48}}>{t('interchange_archive_check')}</Button>:null}
  {selected&&hasReceipt&&!status?.ready&&status?.state!=='aborted'?<>
   <TouchableRipple accessibilityRole="checkbox" accessibilityState={{checked:confirmAbort,disabled:busy||denied}} aria-checked={confirmAbort} disabled={busy||denied} onPress={()=>setConfirmAbort(!confirmAbort)}><Text>{confirmAbort?'☑':'☐'} {t('interchange_archive_abort_confirm')}</Text></TouchableRipple>
   <Button  accessibilityRole="button" disabled={busy||denied||!confirmAbort} onPress={()=>void cancelUpload()}  mode="contained" contentStyle={{minHeight:48}}>{t('interchange_archive_abort')}</Button>
  </>:null}
  {status?.state==='aborted'&&hasReceipt?<Button  accessibilityRole="button" disabled={busy||denied} onPress={()=>void resetAborted()}  mode="contained" contentStyle={{minHeight:48}}>{t('interchange_archive_restart')}</Button>:null}
  {status?.ready&&hasReceipt?<Button  accessibilityRole="button" disabled={busy||denied||opened.current} onPress={()=>void open()}  mode="contained" contentStyle={{minHeight:48}}>{t('interchange_archive_open')}</Button>:null}
 </View></SafeAreaView>;
}
