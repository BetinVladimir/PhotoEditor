import {Button,Text} from 'react-native-paper';
import {useEffect,useMemo,useRef,useState} from 'react';
import {View} from 'react-native';
import type {ApiClient} from '@photoeditor/api-client';
import {CollectionGuestUploadsAPI,type GuestUploadRow} from './collection-guest-uploads-api';
import {useLocale} from './locale-context';
import {useReadResource} from './use-read-resource';
import {readDeadline} from './read-deadline';

export type CollectionGuestUploadsProps={client:ApiClient;origin:string;tenant:string;collectionId:string;state:string;canEdit:boolean;current:()=>boolean;onChanged:()=>void;acquire:()=>symbol|null;release:(token:symbol)=>void};
export function CollectionGuestUploads(props:CollectionGuestUploadsProps){
 // Upload metadata uses the same Editor boundary as the existing upload API.
 // Withdrawing that role unmounts the reader and its owned mutation lifetime.
 const instance=useMemo(()=>crypto.randomUUID(),[props.client,props.origin,props.tenant,props.collectionId]);
 return props.canEdit?<GuestUploadsScope key={instance} {...props}/>:null;
}
function GuestUploadsScope(props:CollectionGuestUploadsProps){
 const __catalogT=useLocale().t;

 const __uiMessage=useLocale().message;

 const {locale}=useLocale();
 const text=(en:string,ru:string,bg:string)=>locale==='ru'?ru:locale==='bg'?bg:en;
 const api=useMemo(()=>new CollectionGuestUploadsAPI(props.client,props.tenant,props.collectionId),[props.client,props.tenant,props.collectionId]);
 const [cursor,setCursor]=useState<string|undefined>(),[busy,setBusy]=useState(false),[message,setMessage]=useState('');
 const scope=JSON.stringify([props.origin,props.tenant,props.collectionId,cursor??null]);
 const read=useReadResource(scope,api,async signal=>{
  if(!props.current())throw new Error('GUEST_UPLOAD_SCOPE_WITHDRAWN');
  const page=await api.list(signal,cursor);
  signal.throwIfAborted();
  if(!props.current())throw new Error('GUEST_UPLOAD_SCOPE_WITHDRAWN');
  return page;
 });
 const live=useRef({props,api,read,scope});live.current={props,api,read,scope};
 const flight=useRef<{controller:AbortController;token:symbol;release:CollectionGuestUploadsProps['release']}|null>(null);
 const alive=useRef(true),cursors=useRef(new Set<string>());
 useEffect(()=>{alive.current=true;return()=>{alive.current=false;const f=flight.current;flight.current=null;f?.controller.abort();if(f)f.release(f.token);};},[api,props.origin,cursor]);
 useEffect(()=>{
  if(read.current()&&props.current()&&['draft','published'].includes(props.state))return;
  const f=flight.current;if(!f)return;
  flight.current=null;f.controller.abort();f.release(f.token);setBusy(false);
  setMessage("The result is uncertain. Refresh before another decision.");
 },[read,props.current,props.state]);
 const current=()=>alive.current&&live.current.api===api&&live.current.scope===scope&&read.current()&&live.current.props.current();
 function stop(displayed:typeof flight.current){
  if(!displayed||flight.current!==displayed||!current())return;
  flight.current=null;displayed.controller.abort();displayed.release(displayed.token);setBusy(false);
  setMessage("The result is uncertain. Reload the list before another decision.");
  read.retry();
 }
 async function moderate(row:GuestUploadRow,decision:'accept'|'reject'){
  if(!current()||flight.current||!live.current.props.canEdit||!['draft','published'].includes(live.current.props.state)||read.phase!=='ready'||!read.data?.items.includes(row))return;
  const token=props.acquire();if(token===null)return;
  const f={controller:new AbortController(),token,release:props.release};flight.current=f;setBusy(true);setMessage('');
  const owns=()=>flight.current===f&&!f.controller.signal.aborted&&current();
  try{
   await readDeadline(f.controller.signal,async signal=>{
    if(!owns()||!live.current.props.canEdit||!['draft','published'].includes(live.current.props.state))throw new Error('GUEST_UPLOAD_SCOPE_WITHDRAWN');
    await api.moderate(row,decision,signal);signal.throwIfAborted();
    if(!owns())throw new Error('GUEST_UPLOAD_SCOPE_WITHDRAWN');
   },15000);
   if(owns()){read.retry();live.current.props.onChanged();}
  }catch{
   if(owns()){setMessage("Reload to reconcile the current result. Decisions are never retried automatically.");read.retry();}
  }finally{if(flight.current===f){flight.current=null;f.release(token);if(alive.current)setBusy(false);}}
 }
 const displayed=flight.current,blocked=busy||!['draft','published'].includes(props.state);
 const uploadState=(state:string)=>({initializing:__catalogT('ui_95eece039a71'),uploading:__catalogT('ui_8b898ca34ce7'),completing:__catalogT('ui_5920fe4c1609'),verifying:__catalogT('ui_daab3b86eace'),verified:__catalogT('ui_27003e8386c5'),rejected:__catalogT('ui_715a8424ae30'),aborted:__catalogT('ui_6c65793474de'),expired:__catalogT('ui_53e54ecccc05')} as Record<string,string>)[state]??__catalogT('ui_7eb5af92e49c');
 const reviewState=(state:string|undefined)=>state?({pending_review:__catalogT('ui_8ea584e0c4aa'),accepted:__catalogT('ui_a00fb0c50741'),rejected:__catalogT('ui_aea4a04a8042')} as Record<string,string>)[state]??'':'';
 return <View><Text accessibilityRole="header">{__catalogT('ui_be7bbee7c3c3')}</Text>
 <Text>{__uiMessage(message)}</Text>
 {busy?<Button onPress={()=>stop(displayed)}>{__catalogT('ui_d4af24be4049')}</Button>:null}
 {read.phase==='loading'?<><Text>{__catalogT('ui_ba3bbbe10d8b')}</Text><Button onPress={read.cancel}>{__catalogT('ui_e7852dcdece5')}</Button></>:null}
 {read.phase!=='loading'?<Button disabled={busy} onPress={()=>{if(!current())return;cursors.current.clear();if(cursor)setCursor(undefined);else read.retry();}}>{__catalogT('ui_e73ecc73592e')}</Button>:null}
 {read.phase==='error'||read.phase==='cancelled'?<Text>{__catalogT('ui_f443de4aefae')}</Text>:null}
 {read.phase==='ready'&&read.data?<>
 {!read.data.items.length?<Text>{__catalogT('ui_16135c0604e9')}</Text>:null}
 {read.data.items.map(row=><View key={row.id}><Text>{row.filename}</Text><Text>{uploadState(row.state)} · {row.actual_size??0}/{row.expected_size} · {reviewState(row.moderation_state)}</Text>
 {row.state==='verified'&&row.moderation_state==='pending_review'?<><Button disabled={blocked||!Number.isSafeInteger(Number(row.revision))} onPress={()=>void moderate(row,'accept')}>{__catalogT('ui_ecc386f24000')}</Button><Button disabled={blocked||!Number.isSafeInteger(Number(row.revision))} onPress={()=>void moderate(row,'reject')}>{__catalogT('ui_1e1ca31b0920')}</Button></>:null}</View>)}
 {read.data.next_cursor?<Button disabled={busy} onPress={()=>{const next=read.data?.next_cursor;if(!current()||!next||cursors.current.has(next)){if(current())setMessage("Refresh the list to continue.");return;}cursors.current.add(next);setCursor(next);}}>{__catalogT('ui_bcf095550c3a')}</Button>:null}
 </>:null}
 </View>;
}
