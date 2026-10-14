import {MultiApplyRecovery} from './multi-apply-recovery';
import type {MultiApplyRecord} from './multi-apply-attempt';
import type {MultiApplied} from './multi-apply';
import {MultiQualityView} from './multi-quality-view';
import {MultiPreviewView} from './multi-preview-view';
import {useEffect,useState} from 'react';
import {ActivityIndicator,Pressable,Text,View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import type {MultiAttemptRecord} from './multi-attempt';
import {multiTicketProducer,readMultiAttemptResult,type MultiResultObservation} from './multi-result';
import {useLocale} from './locale-context';

export function MultiReviewView({api,tenant,attempt,apply}:{apply?:{api:OwnerAPI;origin:string;collection:string;selection:MultiApplyRecord['selection'];disabled?:boolean;onApplied:(value:MultiApplied)=>void};api:Pick<OwnerAPI,'get'|'multiProposal'|'multiPreview'|'multiQuality'>;tenant:string;attempt:MultiAttemptRecord}){
 const {t}=useLocale();
 const scope=JSON.stringify([tenant,attempt.operation,attempt.project,attempt.body,attempt.ticket]);
 const [refresh,setRefresh]=useState(0),[state,setState]=useState<{api:typeof api;scope:string;value:MultiResultObservation|null;failed:boolean}|null>(null);
 useEffect(()=>{
  const controller=new AbortController();let disposed=false;let timer:ReturnType<typeof setTimeout>|undefined;setState(null);
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
  }).catch(()=>{if(!disposed&&!controller.signal.aborted)setState({api,scope,value:null,failed:true});});
  return()=>{disposed=true;if(timer!==undefined)clearTimeout(timer);controller.abort();};
 },[api,scope,refresh]);
 const current=state?.api===api&&state.scope===scope?state:null;
 const proposal=current?.value?.state==='available'?current.value.proposal:null;
 let recoveryProducer:string|null=null;
 if(apply&&attempt.ticket){try{recoveryProducer=multiTicketProducer(attempt.operation,attempt.ticket);}catch{/* Invalid acknowledgement never selects a journal. */}}
 const fraction=proposal?Number(proposal.reason.split('motion_fraction=')[1].split(';')[0]):null;
 return <View accessibilityLabel={t('multi_review_title')}>
  <Text accessibilityRole="header">{t('multi_review_title')}</Text>
  {!current?<View><ActivityIndicator/><Text>{t('multi_review_loading')}</Text></View>:null}
  {current?.failed?<Text accessibilityRole="alert">{t('multi_review_error')}</Text>:null}
  {current?.value?.state==='pending'?<Text>{t(current.value.workflow_state==='cancelling'?'multi_review_cancelling':'multi_review_pending')}</Text>:null}
  {current?.value?.state==='terminal'?<Text accessibilityRole="alert">{t(current.value.workflow_state==='failed'?'multi_review_failed':'multi_review_cancelled')}</Text>:null}
  {apply&&recoveryProducer?<MultiApplyRecovery key={JSON.stringify([apply.origin,tenant,attempt.project,recoveryProducer,apply.collection,attempt.body])} api={apply.api} origin={apply.origin} tenant={tenant} project={attempt.project} producer={recoveryProducer} collection={apply.collection} operation={attempt.operation} intent={attempt.body} onApplied={apply.onApplied}/>:null}
  {proposal?<View>
   <MultiPreviewView api={api} proposal={proposal} intent={attempt.body}/>
   <MultiQualityView api={api} proposal={proposal} intent={attempt.body} apply={apply}/>
   <Text>{t('multi_review_required')}</Text>
   <Text>{t('multi_review_frames').replace('{n}',String(proposal.sources.length))}</Text>
   <Text>{t('multi_review_reference').replace('{n}',String(proposal.reference_ordinal+1))}</Text>
   <Text>{t('multi_review_dimensions').replace('{w}',String(proposal.canvas.width)).replace('{h}',String(proposal.canvas.height))}</Text>
   <Text>{t('multi_review_motion').replace('{n}',((fraction??0)*100).toFixed(2))}</Text>
   {proposal.reason.endsWith(';REGISTRATION_INCONSISTENT')?<Text>{t('multi_review_registration')}</Text>:null}
   <Text>{t('multi_review_not_applied')}</Text>
  </View>:null}
  <Pressable accessibilityRole="button" disabled={!current} onPress={()=>setRefresh(n=>n+1)}><Text>{t('multi_review_refresh')}</Text></Pressable>
 </View>;
}
