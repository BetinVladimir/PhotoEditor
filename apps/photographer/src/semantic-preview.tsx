import {Button,Text,ActivityIndicator} from 'react-native-paper';
import {readDeadline} from './read-deadline';
import {useLocale} from './locale-context';
import type en from '../../../locales/en/common.json';
import {useEffect,useMemo,useRef,useState} from 'react';
import {AppState,Image,View} from 'react-native';
import {APIError,type ApiClient,type SemanticAppliedPreview} from '@photoeditor/api-client';

type Props={client:Pick<ApiClient,'semanticAppliedPreview'>;tenant:string;target:string;run:string;onAccessLost?:()=>void};
export function SemanticPreview(props:Props){const scope=useMemo(()=>crypto.randomUUID(),[props.client,props.tenant,props.target,props.run]);return <SemanticPreviewScope key={scope} {...props}/>;}
function SemanticPreviewScope({client,tenant,target,run,onAccessLost}:Props){
 const {t}=useLocale();
 const[image,setImage]=useState<SemanticAppliedPreview['after']|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState<keyof typeof en|''>('');
 const active=useRef((AppState.currentState??'active')==='active'),[foreground,setForeground]=useState(active.current);
 const denied=useRef(false),[authorityDenied,setAuthorityDenied]=useState(false);
 const currentImage=useRef<SemanticAppliedPreview['after']|null>(null),flight=useRef<AbortController|null>(null),expiry=useRef<ReturnType<typeof setTimeout>|null>(null);
 const clear=()=>{currentImage.current=null;setImage(null);if(expiry.current!==null)clearTimeout(expiry.current);expiry.current=null};
 function cancel(){const controller=flight.current;if(!controller)return;controller.abort();flight.current=null;clear();setBusy(false);}
 async function load(){
  if(flight.current||!active.current||denied.current)return;
  clear();setError('');setBusy(true);const controller=new AbortController();flight.current=controller;
  try{
   const page=await readDeadline(controller.signal,signal=>client.semanticAppliedPreview(tenant,target,signal));
   if(controller.signal.aborted||!active.current)return;
   if(page.tenant_id!==tenant||page.target_id!==target||page.child_run_id!==run){setError('semantic_preview_unavailable');return}
   const output=page.after;
   if(Date.parse(output.expires_at)<=Date.now()){setError('semantic_preview_expired');return}
   currentImage.current=output;setImage(output);expiry.current=setTimeout(()=>{if(currentImage.current!==output)return;currentImage.current=null;setImage(null);setError('semantic_preview_expired');expiry.current=null},Math.min(2147483647,Date.parse(output.expires_at)-Date.now()));
  }catch(error){if(!controller.signal.aborted&&active.current){clear();setError('semantic_preview_failed');if(error instanceof APIError&&[401,403,404,410].includes(error.status)){denied.current=true;setAuthorityDenied(true);onAccessLost?.()}}}
  finally{if(flight.current===controller){flight.current=null;if(!controller.signal.aborted)setBusy(false)}}
 }
 useEffect(()=>{
  clear();setBusy(false);setError('');
  const listener=AppState.addEventListener('change',state=>{
   active.current=state==='active';setForeground(active.current);
   if(!active.current){flight.current?.abort();flight.current=null;clear();setBusy(false);setError('');}
  });
  return()=>{currentImage.current=null;listener.remove();flight.current?.abort();flight.current=null;if(expiry.current!==null)clearTimeout(expiry.current);expiry.current=null};
 },[client,tenant,target,run]);
 return <View><Button accessibilityRole="button" disabled={busy||!foreground||authorityDenied} onPress={()=>void load()} mode="outlined">{t(image?'semantic_preview_reload':'semantic_preview_view')}</Button>{busy?<><ActivityIndicator accessibilityLabel={t('semantic_preview_loading')}/><Button accessibilityRole="button" onPress={cancel} mode="outlined">{t('read_cancel')}</Button></>:null}{error?<Text accessibilityRole="alert">{t(error)}</Text>:null}{image?<Image accessibilityLabel={t('semantic_preview_image')} source={{uri:image.url}} resizeMode="contain" style={{width:280,height:220}} onError={()=>{if(currentImage.current!==image)return;clear();setError('semantic_preview_display_failed')}}/>:null}</View>;
}
