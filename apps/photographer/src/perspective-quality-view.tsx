import type {PerspectiveApplyRecord} from './perspective-apply-attempt';
import {PerspectiveApplyView} from './perspective-apply-view';
import type {PerspectiveApplied} from './perspective-apply';
import {useEffect,useState} from 'react';
import {Pressable,Text,View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import type {PerspectiveProposal} from './perspective-proposal';
import type {PerspectiveQuality} from './perspective-quality';
import {useLocale} from './locale-context';
const reasonKeys={nonfinite:'perspective_quality_nonfinite',excessive_black_clipping:'perspective_quality_black',excessive_white_clipping:'perspective_quality_white',low_sharpness:'perspective_quality_sharpness'} as const;
export function perspectiveQualityDisplayScope(proposal:PerspectiveProposal):string{return JSON.stringify(proposal);}
export function PerspectiveQualityView({api,proposal,origin,selection,disabled,onApplied}:{api:OwnerAPI;proposal:PerspectiveProposal;origin:string;selection?:PerspectiveApplyRecord['selection'];disabled?:boolean;onApplied?:(value:PerspectiveApplied)=>void}){
 const {t}=useLocale();
 const scope=perspectiveQualityDisplayScope(proposal);
 const [attempt,setAttempt]=useState(0),[state,setState]=useState<{api:OwnerAPI;scope:string;value:PerspectiveQuality|null;failed:boolean}|null>(null);
 useEffect(()=>{
  const controller=new AbortController();let disposed=false;
  setState(null);
  void api.perspectiveQuality(structuredClone(proposal),controller.signal).then(value=>{
   if(!disposed&&!controller.signal.aborted)setState({api,scope,value,failed:false});
  }).catch(()=>{if(!disposed&&!controller.signal.aborted)setState({api,scope,value:null,failed:true});});
  return()=>{disposed=true;controller.abort();};
 },[api,scope,proposal,attempt]);
 const current=state?.api===api&&state.scope===scope?state:null;
 const quality=current?.value?.quality;
 return <View accessibilityLabel={t('perspective_quality_title')}>
  <Text>{t('perspective_quality_title')}</Text>
  {quality?<View><Text>{t(quality.status==='pass'?'perspective_quality_pass':quality.status==='review'?'perspective_quality_review':'perspective_quality_fail')}</Text>
   {quality.reasons.map(reason=><Text key={reason}>{t(reasonKeys[reason as keyof typeof reasonKeys])}</Text>)}
   <Text>{t('perspective_quality_black')}: {(quality.technical.clipped_black_fraction*100).toFixed(2)}%</Text>
   <Text>{t('perspective_quality_white')}: {(quality.technical.clipped_white_fraction*100).toFixed(2)}%</Text>
   <Text>{t('perspective_quality_not_approval')}</Text></View>:<Text>{t(current?.failed?'perspective_quality_unavailable':'perspective_quality_pending')}</Text>}
  {current?.value&&onApplied?<PerspectiveApplyView key={[scope,current.value.quality_result_id,current.value.quality_report.sha256].join(':')} api={api} origin={origin} selection={selection} review={current.value} disabled={disabled} onApplied={onApplied}/>:null}
  <Pressable accessibilityRole="button" onPress={()=>setAttempt(n=>n+1)}><Text>{t('perspective_quality_refresh')}</Text></Pressable>
 </View>;
}
