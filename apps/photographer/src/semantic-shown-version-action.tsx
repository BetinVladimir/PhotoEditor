import {Button,Text} from 'react-native-paper';
import {readDeadline} from './read-deadline';
import {useEffect,useMemo,useRef,useState} from 'react';
import {AppState,View} from 'react-native';
import {APIError,type ApiClient} from '@photoeditor/api-client';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
type Props={client:Pick<ApiClient,'materializeSemanticShownVersion'>;tenant:string;target:string;onOpen:(version:string,asset:string)=>void;onAccessLost?:()=>void};
export function SemanticShownVersionAction(props:Props){const scope=useMemo(()=>crypto.randomUUID(),[props.client,props.tenant,props.target]);return <ScopedAction key={scope} {...props}/>;}
function ScopedAction({client,tenant,target,onOpen,onAccessLost}:Props){
 const {t}=useLocale(),[busy,setBusy]=useState(false),[failed,setFailed]=useState(false),[denied,setDenied]=useState(false),[active,setActive]=useState((AppState.currentState??'active')==='active');
 const flight=useRef<AbortController|null>(null),alive=useRef(false),deniedAuthority=useRef(false),foreground=useRef(active),callback=useRef(onOpen);callback.current=onOpen;
 useEffect(()=>{alive.current=true;const sub=AppState.addEventListener('change',state=>{foreground.current=state==='active';setActive(foreground.current);if(!foreground.current){flight.current?.abort();flight.current=null;setBusy(false);}});return()=>{alive.current=false;sub.remove();flight.current?.abort();flight.current=null;};},[]);
 const displayed=flight.current;
 function cancel(){if(!alive.current||!displayed||flight.current!==displayed)return;displayed.abort();flight.current=null;setBusy(false);}
 async function open(){
  if(!alive.current||!foreground.current||deniedAuthority.current||flight.current)return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setFailed(false);
  try{const result=await readDeadline(controller.signal,signal=>client.materializeSemanticShownVersion(tenant,target,signal),15000);if(alive.current&&flight.current===controller&&!controller.signal.aborted&&foreground.current)callback.current(result.asset_version_id,result.asset_id);}
  catch(error){if(!controller.signal.aborted){setFailed(true);if(error instanceof APIError&&[401,403,404,410].includes(error.status)){deniedAuthority.current=true;setDenied(true);onAccessLost?.();}}}
  finally{if(flight.current===controller){flight.current=null;if(!controller.signal.aborted)setBusy(false);}}
 }
 return <View><Button accessibilityRole="button" disabled={busy||!active||denied} onPress={()=>void open()}    mode="contained" contentStyle={{minHeight:48}}>{t(busy?'semantic_version_opening':'semantic_version_open')}</Button>{busy?<Button accessibilityRole="button" onPress={cancel} mode="outlined">{t('read_cancel')}</Button>:null}{failed?<Text accessibilityRole="alert" style={s.error}>{t('semantic_version_failed')}</Text>:null}</View>;
}
