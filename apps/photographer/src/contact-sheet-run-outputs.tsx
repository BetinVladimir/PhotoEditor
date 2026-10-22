import {useLocale} from './locale-context';
import {ActivityIndicator,Button,Text} from 'react-native-paper';
import {readDeadline} from './read-deadline';
import {contactSheetOutputURL} from './contact-sheet-output-url';
import {readRegionArtifact} from './region-preview-download';
import {contactSheetPages} from './contact-sheet-manifest';
import {useEffect,useRef,useState} from 'react';
import {APIError} from '@photoeditor/api-client';
import {Linking,View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import type {RunOutput} from './catalog-types';
import {catalogStyle as s} from './catalog-style';

/** Only server-authorized, version-bound result URLs; no object-key synthesis. */
export function ContactSheetRunOutputs({api,run,onAccessDenied}:{api:Pick<OwnerAPI,'runOutputs'>;run:string;onAccessDenied?:(error:APIError)=>void}){
 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;

 const [outputs,setOutputs]=useState<RunOutput[]>([]),[busy,setBusy]=useState(true),[error,setError]=useState(''),[reload,setReload]=useState(0);
 const denied=useRef(onAccessDenied);denied.current=onAccessDenied;
 const ownership=useRef({api,run});ownership.current={api,run};
 const lifetime=useRef<AbortController|null>(null),visible=useRef<RunOutput[]>([]);
 useEffect(()=>{
  const controller=new AbortController();lifetime.current=controller;
  let timer:ReturnType<typeof setTimeout>|undefined;
  visible.current=[];setOutputs([]);setBusy(true);setError('');
  void readDeadline(controller.signal,async signal=>{
   const result=await api.runOutputs(run,signal);signal.throwIfAborted();
   if(controller.signal.aborted)return;
   const candidates=result.items.filter(item=>item.role==='contact_page');
   if(!candidates.length)return;
   const manifests=result.items.filter(item=>item.role==='contact_sheet');
   if(manifests.length!==1)throw new Error('CONTACT_MANIFEST_MISSING');
   const bytes=await readRegionArtifact(manifests[0],'contact_manifest',signal);
   signal.throwIfAborted();
   const pages=contactSheetPages(bytes,result.items);
   if(pages.length>128||new Set(pages.map(item=>item.artifact_id)).size!==pages.length)throw new Error('CONTACT_OUTPUT_INVALID');
   for(const item of pages){
    contactSheetOutputURL(item);
   }
   visible.current=pages;setOutputs(pages);
   if(pages.length)timer=setTimeout(()=>{if(!controller.signal.aborted){visible.current=[];setOutputs([]);setError('Contact sheet links expired. Refresh to open results.');}},Math.min(...pages.map(item=>Date.parse(item.expires_at)-Date.now()),2147483647));
  }).catch(error=>{
   if(controller.signal.aborted)return;
   visible.current=[];setOutputs([]);setError('Contact sheet results are unavailable.');
   if(error instanceof APIError&&[401,403,404,410].includes(error.status))denied.current?.(error);
  }).finally(()=>{if(!controller.signal.aborted)setBusy(false);});
  return()=>{visible.current=[];controller.abort();if(timer)clearTimeout(timer);if(lifetime.current===controller)lifetime.current=null;};
 },[api,run,reload]);
 async function open(item:RunOutput,display:RunOutput[]){
  const controller=lifetime.current;if(ownership.current.api!==api||ownership.current.run!==run||!controller||controller.signal.aborted||visible.current!==display||!display.includes(item))return;
  if(Date.parse(item.expires_at)<=Date.now()){visible.current=[];setOutputs([]);setError('Contact sheet links expired. Refresh to open results.');return;}
  try{await Linking.openURL(contactSheetOutputURL(item));}catch{if(!controller.signal.aborted)setError('Unable to open contact sheet.');}
 }
 const displayedController=lifetime.current;
 const displayCurrent=()=>ownership.current.api===api&&ownership.current.run===run&&lifetime.current===displayedController;
 function refresh(){if(!displayCurrent()||busy)return;setReload(value=>value+1);}
 return <View style={{gap:8}}>{busy?<><ActivityIndicator/><Button accessibilityRole="button" onPress={()=>{if(!displayedController||lifetime.current!==displayedController)return;displayedController.abort();lifetime.current=null;visible.current=[];setOutputs([]);setBusy(false);setError('Contact sheet waiting stopped. Refresh to load results.');}}>{__uiT('ui_18ecd1c8ba2b')}</Button></>:null}{outputs.length?<Text style={s.name}>{__uiT('ui_c34c7f273c55')}</Text>:null}
  {outputs.map((item,index)=><Button  key={item.artifact_id} accessibilityRole="button" onPress={()=>void open(item,outputs)}  mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_f72b0e4e4372')}{' '}{index+1}</Button>)}
  {error?<><Text accessibilityRole="alert" style={s.error}>{__uiMessage(error,true)}</Text><Button  accessibilityRole="button" disabled={busy} onPress={refresh}  mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_2d11c2a2cced')}</Button></>:null}
 </View>;
}
