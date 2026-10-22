import {Button,ActivityIndicator,Text} from 'react-native-paper';
import {MultiApplyRecovery} from './multi-apply-recovery';
import type {MultiApplyRecord} from './multi-apply-attempt';
import type {MultiApplied} from './multi-apply';
import {MultiQualityView} from './multi-quality-view';
import {MultiPreviewView} from './multi-preview-view';
import {useEffect,useState} from 'react';
import {View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import type {MultiAttemptRecord} from './multi-attempt';
import {multiTicketProducer,readMultiAttemptResult,type MultiResultObservation} from './multi-result';
import {useLocale} from './locale-context';
import {APIError} from '@photoeditor/api-client';

export function MultiReviewView({api,tenant,attempt,apply,onAccessDenied}:{onAccessDenied?:()=>void;apply?:{api:OwnerAPI;origin:string;collection:string;selection:MultiApplyRecord['selection'];disabled?:boolean;onApplied:(value:MultiApplied)=>void};api:Pick<OwnerAPI,'get'|'multiProposal'|'multiPreview'|'multiQuality'>;tenant:string;attempt:MultiAttemptRecord}){
 const __uiT=useLocale().t;

 const {t}=useLocale();
 const scope=JSON.stringify([tenant,attempt.operation,attempt.project,attempt.body,attempt.ticket]);
 const [refresh,setRefresh]=useState(0),[state,setState]=useState<{api:typeof api;scope:string;value:MultiResultObservation|null;failed:boolean}|null>(null);
 const [denied,setDenied]=useState(false);
 function withdrawAccess(){setDenied(true);setState(null);onAccessDenied?.();}
 useEffect(()=>{
  const controller=new AbortController();let disposed=false;let timer:ReturnType<typeof setTimeout>|undefined;setState(null);
  if(denied)return()=>controller.abort();
  if(!attempt.ticket){setState({api,scope,value:null,failed:true});return()=>controller.abort();}
  void readMultiAttemptResult(api,tenant,attempt.project,attempt.operation,attempt.body,attempt.ticket,controller.signal).then(value=>{
   if(!disposed&&!controller.signal.aborted){
    setState({api,scope,value,failed:false});
    // Only a confirmed pending observation schedules another read. Errors
    // remain explicit/manual; no admission or Apply mutation is retried here.
    if(value.state==='pending')timer=setTimeout(()=>{
     if(!disposed&&!controller.signal.aborted)setRefresh(n=>n+1);
    },2000);
   }
  }).catch(error=>{if(!disposed&&!controller.signal.aborted){if(error instanceof APIError&&[401,403,404,410].includes(error.status))withdrawAccess();else setState({api,scope,value:null,failed:true});}});
  return()=>{disposed=true;if(timer!==undefined)clearTimeout(timer);controller.abort();};
 },[api,scope,refresh,denied]);
 const current=state?.api===api&&state.scope===scope?state:null;
 const proposal=current?.value?.state==='available'?current.value.proposal:null;
 let recoveryProducer:string|null=null;
 if(apply&&attempt.ticket){try{recoveryProducer=multiTicketProducer(attempt.operation,attempt.ticket);}catch{/* Invalid acknowledgement never selects a journal. */}}
 const fraction=proposal?Number(proposal.reason.split('motion_fraction=')[1].split(';')[0]):null;
 if(denied)return <Text accessibilityRole="alert">{__uiT('ui_3bd6e2f1e27d')}</Text>;
 return <View accessibilityLabel={t('multi_review_title')}>
  <Text accessibilityRole="header">{t('multi_review_title')}</Text>
  {!current?<View><ActivityIndicator/><Text>{t('multi_review_loading')}</Text></View>:null}
  {current?.failed?<Text accessibilityRole="alert">{t('multi_review_error')}</Text>:null}
  {current?.value?.state==='pending'?<Text>{t(current.value.workflow_state==='cancelling'?'multi_review_cancelling':'multi_review_pending')}</Text>:null}
  {current?.value?.state==='terminal'?<Text accessibilityRole="alert">{t(current.value.workflow_state==='cancelled'?'multi_review_cancelled':'multi_review_failed')}</Text>:null}
  {apply&&recoveryProducer?<MultiApplyRecovery key={JSON.stringify([apply.origin,tenant,attempt.project,recoveryProducer,apply.collection,attempt.body])} api={apply.api} origin={apply.origin} tenant={tenant} project={attempt.project} producer={recoveryProducer} collection={apply.collection} operation={attempt.operation} intent={attempt.body} onApplied={apply.onApplied} onAccessDenied={withdrawAccess}/>:null}
  {proposal?<View>
   <MultiPreviewView api={api} proposal={proposal} intent={attempt.body} onAccessDenied={withdrawAccess}/>
   <MultiQualityView api={api} proposal={proposal} intent={attempt.body} apply={apply} onAccessDenied={withdrawAccess}/>
   <Text>{t('multi_review_required')}</Text>
   <Text>{t('multi_review_frames').replace('{n}',String(proposal.sources.length))}</Text>
   <Text>{t('multi_review_reference').replace('{n}',String(proposal.reference_ordinal+1))}</Text>
   <Text>{t('multi_review_dimensions').replace('{w}',String(proposal.canvas.width)).replace('{h}',String(proposal.canvas.height))}</Text>
   <Text>{t('multi_review_motion').replace('{n}',((fraction??0)*100).toFixed(2))}</Text>
   {proposal.reason.endsWith(';REGISTRATION_INCONSISTENT')?<Text>{t('multi_review_registration')}</Text>:null}
   <Text>{t('multi_review_not_applied')}</Text>
  </View>:null}
  <Button accessibilityRole="button" disabled={!current} onPress={()=>setRefresh(n=>n+1)} mode="outlined">{t('multi_review_refresh')}</Button>
 </View>;
}
