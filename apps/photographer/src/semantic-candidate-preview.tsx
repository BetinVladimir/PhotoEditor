import {Button,ActivityIndicator,Text} from 'react-native-paper';
import {readDeadline} from './read-deadline';
import {useEffect,useMemo,useRef,useState} from 'react';
import {AppState,Image,View} from 'react-native';
import {APIError,decodeSemanticReviewPreview,type ApiClient,type SemanticPreviewQuery,type SemanticReviewPreview} from '@photoeditor/api-client';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';

type Props={client:Pick<ApiClient,'semanticReviewPreview'>&Partial<Pick<ApiClient,'semanticPreviewAdmission'>>;query:SemanticPreviewQuery;admissionAvailable?:boolean;onReady:(preview:SemanticReviewPreview|null)=>void};
export function SemanticCandidatePreview(props:Props){
 const binding=JSON.stringify(props.query);
 // Keep the in-flight read fence independent of caller-owned nested refs.
 const ownedQuery=useMemo(()=>structuredClone(props.query),[binding]);
 const scope=useMemo(()=>crypto.randomUUID(),[props.client,binding]);
 return <CandidatePreviewScope key={scope} {...props} query={ownedQuery}/>;
}
function CandidatePreviewScope({client,query,admissionAvailable,onReady}:Props){
 const __uiT=useLocale().t;

 const {t}=useLocale();
 const[preview,setPreview]=useState<SemanticReviewPreview|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState(false),[foreground,setForeground]=useState(AppState.currentState===null||AppState.currentState==='active');
 const live=useRef(true),active=useRef(foreground),flight=useRef<AbortController|null>(null),timer=useRef<ReturnType<typeof setTimeout>|null>(null),loaded=useRef(new Set<string>()),current=useRef<SemanticReviewPreview|null>(null),notify=useRef(onReady);
 notify.current=onReady;
 function clear(){current.current=null;loaded.current.clear();if(timer.current!==null)clearTimeout(timer.current);timer.current=null;setPreview(null);notify.current(null);}
 function cancel(){const controller=flight.current;if(!controller)return;controller.abort();flight.current=null;clear();setBusy(false);}
 async function load(){
  if(flight.current||!active.current)return;
  clear();setBusy(true);setError(false);const controller=new AbortController();flight.current=controller;
  const deadline=setTimeout(()=>{if(flight.current===controller){cancel();setError(true);}},10*60*1000);
  try{
   const requested=!!client.semanticPreviewAdmission&&admissionAvailable!==false;
   if(requested){
    try{
     const admitted=await readDeadline(controller.signal,signal=>client.semanticPreviewAdmission!(query,signal),15000);
     if(['failed','cancelled','cancelling'].includes(admitted.status))throw new Error('SEMANTIC_PREVIEW_TERMINAL');
    }catch(error){
     // A known run after a server error is an uncertain commit, not rollback.
     // Read its current proposal result without issuing a second mutation.
     if(!(error instanceof APIError)||error.status<500||!error.previewRunID)throw error;
    }
   }
   let response:SemanticReviewPreview|undefined;
   for(let attempt=0;attempt<75;attempt++){
    controller.signal.throwIfAborted();
    try{response=await readDeadline(controller.signal,signal=>client.semanticReviewPreview(query,signal));break}
    catch(error){
     if(!requested||!(error instanceof APIError)||error.status!==503)throw error;
     await new Promise<void>((resolve,reject)=>{
      const done=()=>{controller.signal.removeEventListener('abort',aborted);resolve()};
      const wait=setTimeout(done,8000);
      const aborted=()=>{clearTimeout(wait);controller.signal.removeEventListener('abort',aborted);reject(new Error('ABORTED'))};
      controller.signal.addEventListener('abort',aborted,{once:true});
      if(controller.signal.aborted)aborted();
     });
    }
   }
   if(!response)throw new Error('SEMANTIC_PREVIEW_TIMEOUT');
   if(controller.signal.aborted||!live.current||flight.current!==controller)return;
   const verified=decodeSemanticReviewPreview(response,query);
   current.current=verified;setPreview(verified);
   const expires=Math.min(...[verified.before,verified.after,verified.affected,verified.protected].map(image=>Date.parse(image.expires_at)));
   timer.current=setTimeout(()=>{if(current.current!==verified||!live.current||!active.current)return;clear();setError(true)},Math.max(1,expires-Date.now()));
  }catch{if(!controller.signal.aborted&&live.current){clear();setError(true)}}
  finally{clearTimeout(deadline);if(flight.current===controller){if(controller.signal.aborted&&live.current&&active.current){clear();setError(true)}flight.current=null;if(live.current)setBusy(false)}}
 }
 function shown(value:SemanticReviewPreview,role:string){
  if(!active.current||!live.current||current.current!==value)return;
  try{decodeSemanticReviewPreview(value,query)}catch{clear();setError(true);return;}
  loaded.current.add(role);
  if(loaded.current.size===4)notify.current(value);
 }
 useEffect(()=>{
  live.current=true;
  const listener=AppState.addEventListener('change',state=>{
   active.current=state==='active';setForeground(active.current);
   if(!active.current){flight.current?.abort();flight.current=null;clear();setBusy(false);setError(false)}
  });
  return()=>{live.current=false;listener.remove();flight.current?.abort();flight.current=null;if(timer.current!==null)clearTimeout(timer.current);current.current=null;loaded.current.clear()};
 },[client]);
 return <View><Button accessibilityRole="button" accessibilityLabel={__uiT('ui_8cbaa55a52ef', {p0:(t('semantic_review_preview_view')),p1:(query.target_id)})} disabled={busy||!foreground} onPress={()=>void load()}    mode="contained" contentStyle={{minHeight:48}}>{t('semantic_review_preview_view')}</Button>{busy?<><ActivityIndicator accessibilityLabel={t('semantic_preview_loading')}/><Button accessibilityRole="button" onPress={cancel} mode="outlined">{t('read_cancel')}</Button></>:null}{error?<Text accessibilityRole="alert" style={s.error}>{t('semantic_review_preview_required')}</Text>:null}{preview?<><Text style={s.meta}>{t(preview.quality_status==='pass'?'semantic_review_quality_pass':'semantic_review_quality_review')}</Text>{preview.limitations.map((value,index)=><Text key={index} style={s.meta}>{value}</Text>)}{(['before','after','affected','protected'] as const).map(role=><View key={role}><Text style={s.meta}>{t(`semantic_review_preview_${role}`)}</Text><Image accessibilityLabel={__uiT('ui_8cbaa55a52ef', {p0:(t(`semantic_review_preview_${role}`)),p1:(query.target_id)})} source={{uri:preview[role].url}} resizeMode="contain" style={{width:280,height:220}} onLoad={()=>shown(preview,role)} onError={()=>{if(current.current===preview){clear();setError(true)}}}/></View>)}</>:<Text style={s.meta}>{t('semantic_review_preview_required')}</Text>}</View>;
}
