import type {MultiApplyRecord} from './multi-apply-attempt';
import {MultiApplyView} from './multi-apply-view';
import type {MultiApplied} from './multi-apply';
import {useEffect,useState} from 'react';
import {Pressable,Text,View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import type {MultiProposal} from './multi-proposal';
import type {MultiIntent} from './multi-intent';
import type {MultiQuality} from './multi-quality';
import {useLocale} from './locale-context';
const reasonKeys={nonfinite:'perspective_quality_nonfinite',excessive_black_clipping:'perspective_quality_black',excessive_white_clipping:'perspective_quality_white',low_sharpness:'perspective_quality_sharpness'} as const;
export function MultiQualityView({api,proposal,intent,apply}:{apply?:{api:OwnerAPI;origin:string;collection:string;selection:MultiApplyRecord['selection'];disabled?:boolean;onApplied:(value:MultiApplied)=>void};api:Pick<OwnerAPI,'multiQuality'>;proposal:MultiProposal;intent:MultiIntent}){
 const {t}=useLocale();
 const scope=JSON.stringify([proposal,intent]);
 const [attempt,setAttempt]=useState(0),[state,setState]=useState<{api:typeof api;scope:string;value:MultiQuality|null;failed:boolean}|null>(null);
 useEffect(()=>{
  const controller=new AbortController();let disposed=false;
  setState(null);
  void api.multiQuality(structuredClone(proposal),structuredClone(intent),controller.signal).then(value=>{
   if(!disposed&&!controller.signal.aborted)setState({api,scope,value,failed:false});
  }).catch(()=>{if(!disposed&&!controller.signal.aborted)setState({api,scope,value:null,failed:true});});
  return()=>{disposed=true;controller.abort();};
 },[api,scope,attempt]);
 const current=state?.api===api&&state.scope===scope?state:null;
 const quality=current?.value?.quality;
 return <View accessibilityLabel={t('perspective_quality_title')}>
  <Text>{t('perspective_quality_title')}</Text>
  {quality?<View><Text>{t(quality.status==='pass'?'perspective_quality_pass':quality.status==='review'?'perspective_quality_review':'perspective_quality_fail')}</Text>
   {quality.reasons.map(reason=><Text key={reason}>{t(reasonKeys[reason as keyof typeof reasonKeys])}</Text>)}
   <Text>{t('perspective_quality_black')}: {(quality.technical.clipped_black_fraction*100).toFixed(2)}%</Text>
   <Text>{t('perspective_quality_white')}: {(quality.technical.clipped_white_fraction*100).toFixed(2)}%</Text>
   <Text>{t('multi_quality_limits')}</Text></View>:<Text>{t(current?.failed?'perspective_quality_unavailable':'perspective_quality_pending')}</Text>}

  {current?.value&&apply?<MultiApplyView key={JSON.stringify([scope,current.value,apply.origin,apply.selection])} api={apply.api} origin={apply.origin} selection={apply.selection} disabled={apply.disabled} review={current.value} intent={intent} onApplied={apply.onApplied}/>:null}
  <Pressable accessibilityRole="button" onPress={()=>setAttempt(n=>n+1)}><Text>{t('perspective_quality_refresh')}</Text></Pressable>
 </View>;
}
