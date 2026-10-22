import {Button,Text} from 'react-native-paper';
import {SavedVersionNavigation} from './saved-version-navigation';
import {PerspectiveApplyRecovery} from "./perspective-apply-recovery";
import {useReviewForeground} from "./use-review-foreground";
import {useEffect,useMemo,useState} from 'react';
import {View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import {readPerspectiveReplacementResult,type PerspectiveReplacementObservation,type PerspectiveReplacementResultScope} from './perspective-replacement-result';
import {PerspectivePreviewView} from './perspective-preview-view';
import {PerspectiveQualityView} from './perspective-quality-view';
import type {PerspectiveApplied} from './perspective-apply';
import {useLocale} from './locale-context';
import {APIError} from '@photoeditor/api-client';

type Props={api:OwnerAPI;origin:string;tenant:string;project:string;review:string;originalTask:string;decisionHash:string};
export function PerspectiveReplacementReview(props:Props){
 const foreground=useReviewForeground();
 const scope=useMemo(()=>crypto.randomUUID(),[props.api,props.tenant,props.project,props.review,props.originalTask,props.decisionHash,props.origin]);
 return foreground.active?<Scope key={`${scope}:${foreground.generation}`} {...props}/>:null;
}
function Scope({api,origin,tenant,project,review,originalTask,decisionHash}:Props){
 const __uiT=useLocale().t;

 const {t}=useLocale(),[refresh,setRefresh]=useState(0);
 const [state,setState]=useState<{api:OwnerAPI;scope:PerspectiveReplacementResultScope;value:PerspectiveReplacementObservation}|null>(null),[failed,setFailed]=useState(false),[applied,setApplied]=useState<PerspectiveApplied|null>(null);
 const [denied,setDenied]=useState(false);
 function withdrawAccess(){setDenied(true);setState(null);setApplied(null);}
 useEffect(()=>{
  const controller=new AbortController();setState(null);setFailed(false);
  if(denied)return()=>controller.abort();
  void(async()=>{
   const scope=await api.perspectiveReplacementScope(project,review,originalTask,decisionHash,controller.signal);
   if(controller.signal.aborted)return;
   if(scope.tenant!==tenant)throw new Error('PERSPECTIVE_RESULT_SCOPE_CHANGED');
   const value=await readPerspectiveReplacementResult(api,scope,controller.signal);
   if(!controller.signal.aborted)setState({api,scope,value});
  })().catch(error=>{if(!controller.signal.aborted){if(error instanceof APIError&&[401,403,404,410].includes(error.status))withdrawAccess();else setFailed(true);}});
  return()=>controller.abort();
 },[api,tenant,project,review,originalTask,decisionHash,refresh,denied]);
 const current=state?.api===api?state.value:null;
 useEffect(()=>{
  if(denied||applied||failed||current?.state!=='pending')return;
  const timer=setTimeout(()=>setRefresh(value=>value+1),2000);
  return()=>clearTimeout(timer);
 },[current,applied,failed,denied]);
 if(denied)return <Text accessibilityRole="alert">{__uiT('ui_3bd6e2f1e27d')}</Text>;
 return <View>
  <Text accessibilityRole="header">{t('perspective_result_review')}</Text>
  {failed?<Text accessibilityRole="alert">{t('perspective_quality_unavailable')}</Text>:null}
  {!current&&!failed?<Text>{t('perspective_loading')}</Text>:null}
  {current?.state==='pending'?<Text>{t(current.workflow_state==='cancelling'?'perspective_replacement_cancelling':'perspective_quality_pending')}</Text>:null}
  {current?.state==='terminal'?<Text accessibilityRole="alert">{t(current.workflow_state==='cancelled'?'perspective_replacement_cancelled':'perspective_replacement_failed')}</Text>:null}
  {state?.api===api&&!applied?<PerspectiveApplyRecovery api={api} origin={origin} tenant={tenant} project={project} version={state.scope.base.asset_version_id} producer={state.scope.task} collection="" onApplied={setApplied} onAccessDenied={withdrawAccess}/>:null}
  {current?.state==='available'&&!applied?<View>
   <PerspectivePreviewView api={api} proposal={current.proposal} onAccessDenied={withdrawAccess}/>
   <PerspectiveQualityView api={api} origin={origin} proposal={current.proposal} onApplied={setApplied} onAccessDenied={withdrawAccess}/>
  </View>:null}
  {applied?<View><Text>{t('perspective_apply_version_saved')}</Text><Text selectable>{applied.asset_version_id}</Text><SavedVersionNavigation api={api} tenant={tenant} project={project} version={applied.asset_version_id}/></View>:null}
  <Button accessibilityRole="button" onPress={()=>setRefresh(n=>n+1)} mode="outlined">{t('perspective_quality_refresh')}</Button>
 </View>;
}
