import {useEffect,useMemo,useState} from 'react';
import {Pressable,Text,View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import {readPerspectiveReplacementResult} from './perspective-replacement-result';
import type {PerspectiveResultObservation} from './perspective-result';
import {PerspectivePreviewView} from './perspective-preview-view';
import {PerspectiveQualityView} from './perspective-quality-view';
import type {PerspectiveApplied} from './perspective-apply';
import {useLocale} from './locale-context';

type Props={api:OwnerAPI;origin:string;tenant:string;project:string;review:string;originalTask:string;decisionHash:string};
export function PerspectiveReplacementReview(props:Props){
 const scope=useMemo(()=>crypto.randomUUID(),[props.api,props.tenant,props.project,props.review,props.originalTask,props.decisionHash,props.origin]);
 return <Scope key={scope} {...props}/>;
}
function Scope({api,origin,tenant,project,review,originalTask,decisionHash}:Props){
 const {t}=useLocale(),[refresh,setRefresh]=useState(0);
 const [state,setState]=useState<{api:OwnerAPI;value:PerspectiveResultObservation}|null>(null),[failed,setFailed]=useState(false),[applied,setApplied]=useState<PerspectiveApplied|null>(null);
 useEffect(()=>{
  const controller=new AbortController();setState(null);setFailed(false);setApplied(null);
  void(async()=>{
   const scope=await api.perspectiveReplacementScope(project,review,originalTask,decisionHash,controller.signal);
   if(controller.signal.aborted)return;
   if(scope.tenant!==tenant)throw new Error('PERSPECTIVE_RESULT_SCOPE_CHANGED');
   const value=await readPerspectiveReplacementResult(api,scope,controller.signal);
   if(!controller.signal.aborted)setState({api,value});
  })().catch(()=>{if(!controller.signal.aborted)setFailed(true);});
  return()=>controller.abort();
 },[api,tenant,project,review,originalTask,decisionHash,refresh]);
 const current=state?.api===api?state.value:null;
 return <View>
  <Text accessibilityRole="header">{t('perspective_result_review')}</Text>
  {failed?<Text accessibilityRole="alert">{t('perspective_quality_unavailable')}</Text>:null}
  {!current&&!failed?<Text>{t('perspective_loading')}</Text>:null}
  {current?.state==='pending'?<Text>{t('perspective_quality_pending')}</Text>:null}
  {current?.state==='available'?<View>
   <PerspectivePreviewView api={api} proposal={current.proposal}/>
   {!applied?<PerspectiveQualityView api={api} origin={origin} proposal={current.proposal} onApplied={setApplied}/>:<View><Text>{t('perspective_apply_version_saved')}</Text><Text selectable>{applied.asset_version_id}</Text></View>}
  </View>:null}
  <Pressable accessibilityRole="button" onPress={()=>setRefresh(n=>n+1)}><Text>{t('perspective_quality_refresh')}</Text></Pressable>
 </View>;
}
