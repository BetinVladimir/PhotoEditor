import {Button,Text} from 'react-native-paper';
import {SavedVersionNavigation} from './saved-version-navigation';
import {useReviewForeground} from "./use-review-foreground";
import {useEffect,useMemo,useState} from 'react';
import {View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import type {MultiOperation} from './multi-intent';
import {readMultiReplacementResult,type MultiReplacementResultScope} from './multi-replacement-result';
import type {MultiResultObservation} from './multi-result';
import type {MultiApplied} from './multi-apply';
import {MultiPreviewView} from './multi-preview-view';
import {MultiQualityView} from './multi-quality-view';
import {MultiApplyRecovery} from './multi-apply-recovery';
import {useLocale} from './locale-context';
import {APIError} from '@photoeditor/api-client';

type Props={api:OwnerAPI;origin:string;tenant:string;project:string;review:string;originalTask:string;decisionHash:string;operation:MultiOperation};
export function MultiReplacementReview(props:Props){
 const foreground=useReviewForeground();
 const key=useMemo(()=>crypto.randomUUID(),[props.api,props.origin,props.tenant,props.project,props.review,props.originalTask,props.decisionHash,props.operation]);
 return foreground.active?<Scope key={`${key}:${foreground.generation}`} {...props}/>:null;
}
function Scope({api,origin,tenant,project,review,originalTask,decisionHash,operation}:Props){
 const __uiT=useLocale().t;

 const {t}=useLocale(),[refresh,setRefresh]=useState(0),[failed,setFailed]=useState(false),[applied,setApplied]=useState<MultiApplied|null>(null);
 const [state,setState]=useState<{api:OwnerAPI;scope:MultiReplacementResultScope;value:MultiResultObservation}|null>(null);
 const [denied,setDenied]=useState(false);
 function withdrawAccess(){setDenied(true);setState(null);setApplied(null);}
 useEffect(()=>{
  const controller=new AbortController();setState(null);setFailed(false);
  if(denied)return()=>controller.abort();
  void(async()=>{
   const scope=await api.multiReplacementScope(operation,project,review,originalTask,decisionHash,controller.signal);
   if(controller.signal.aborted)return;
   if(scope.tenant!==tenant)throw new Error('MULTI_RESULT_SCOPE_CHANGED');
   const value=await readMultiReplacementResult(api,scope,controller.signal);
   if(!controller.signal.aborted)setState({api,scope,value});
  })().catch(error=>{if(!controller.signal.aborted){if(error instanceof APIError&&[401,403,404,410].includes(error.status))withdrawAccess();else setFailed(true);}});
  return()=>controller.abort();
 },[api,tenant,project,review,originalTask,decisionHash,operation,refresh,denied]);
 const current=state?.api===api?state:null;
 useEffect(()=>{
  if(denied||applied||failed||current?.value.state!=='pending')return;
  const timer=setTimeout(()=>setRefresh(n=>n+1),2000);
  return()=>clearTimeout(timer);
 },[current,applied,failed,denied]);
 if(denied)return <Text accessibilityRole="alert">{__uiT('ui_3bd6e2f1e27d')}</Text>;
 return <View>
  <Text accessibilityRole="header">{t('multi_review_title')}</Text>
  {failed?<Text accessibilityRole="alert">{t('multi_review_error')}</Text>:null}
  {!current&&!failed?<Text>{t('multi_review_loading')}</Text>:null}
  {current?.value.state==='pending'?<Text>{t(current.value.workflow_state==='cancelling'?'multi_review_cancelling':'multi_review_pending')}</Text>:null}
  {current?.value.state==='terminal'?<Text accessibilityRole="alert">{t(current.value.workflow_state==='cancelled'?'multi_review_cancelled':'multi_review_failed')}</Text>:null}
  {current&&!applied?<MultiApplyRecovery api={api} origin={origin} tenant={tenant} project={project} producer={current.scope.task} collection="" operation={operation} intent={current.scope.intent} onApplied={setApplied} onAccessDenied={withdrawAccess}/>:null}
  {current?.value.state==='available'&&!applied?<View>
   <MultiPreviewView api={api} proposal={current.value.proposal} intent={current.scope.intent} onAccessDenied={withdrawAccess}/>
   <MultiQualityView api={api} proposal={current.value.proposal} intent={current.scope.intent} apply={{api,origin,collection:'',selection:null,onApplied:setApplied}} onAccessDenied={withdrawAccess}/>
  </View>:null}
  {applied?<View><Text>{t('perspective_apply_version_saved')}</Text><Text selectable>{applied.asset_version_id}</Text><SavedVersionNavigation api={api} tenant={tenant} project={project} version={applied.asset_version_id}/></View>:null}
  <Button accessibilityRole="button" onPress={()=>setRefresh(n=>n+1)} mode="outlined">{t('multi_review_refresh')}</Button>
 </View>;
}
