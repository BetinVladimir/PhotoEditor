import {Button,Text} from 'react-native-paper';
import {useEffect,useState} from 'react';
import {View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import type {OwnerAPI} from './owner-api';
import type {MultiApplyRecord} from './multi-apply-attempt';
import {listMultiApplyAttempts} from './multi-apply-store';
import {MultiApplyRecovery} from './multi-apply-recovery';
import {useLocale} from './locale-context';
type Props={api:OwnerAPI;origin:string;tenant:string;project:string;collection:string;onApplied:()=>void};
export function MultiApplyInbox(props:Props){
 return <MultiApplyInboxScope key={JSON.stringify([props.origin,props.tenant,props.project,props.collection])} {...props}/>;
}
function MultiApplyInboxScope({api,origin,tenant,project,collection,onApplied}:Props){
 const {t}=useLocale(),scope=JSON.stringify([origin,tenant,project,collection]);
 const [refresh,setRefresh]=useState(0),[cursor,setCursor]=useState<string|undefined>();
 const request=JSON.stringify([scope,cursor,refresh]);
 const [state,setState]=useState<{api:OwnerAPI;scope:string;request:string;records:MultiApplyRecord[];next:string|null;failed:boolean}|null>(null);
 useEffect(()=>{
  const controller=new AbortController();
  void(async()=>{try{
   const user=await api.me(controller.signal);if(controller.signal.aborted)return;
   const page=await listMultiApplyAttempts(AsyncStorage,{origin,user:user.id,tenant,project,collection,cursor});if(controller.signal.aborted)return;
   setState({api,scope,request,records:page.records,next:page.nextCursor,failed:false});
  }catch{if(!controller.signal.aborted)setState({api,scope,request,records:[],next:null,failed:true});}})();
  return()=>controller.abort();
 },[api,request]);
 const current=state?.api===api&&state.scope===scope&&state.request===request?state:null;
 return <View><Text>{t('multi_recovery_title')}</Text>
  {!current?<Text accessibilityLiveRegion="polite">{t('multi_recovery_loading')}</Text>:null}
  {current&&!current.failed&&current.records.length===0?<Text>{t('multi_recovery_empty')}</Text>:null}
  {current?.failed?<Text accessibilityRole="alert">{t('perspective_apply_failed')}</Text>:null}
  {current?.records.map(record=><View key={record.review.proposal.producer_task_id}>
   <Text>{t(record.review.proposal.operation==='optical.panorama'?'multi_panorama':'multi_focus')}</Text>
   <Text>{t('multi_review_frames').replace('{n}',String(record.review.proposal.sources.length))} · {record.review.proposal.canvas.width} × {record.review.proposal.canvas.height}</Text>
   <MultiApplyRecovery api={api} origin={origin} tenant={tenant} project={project} collection={collection} producer={record.review.proposal.producer_task_id} operation={record.review.proposal.operation} intent={record.intent} onApplied={onApplied}/>
  </View>)}
  <Button accessibilityRole="button" onPress={()=>{setState(null);setCursor(undefined);setRefresh(n=>n+1);}} mode="outlined">{t('perspective_apply_reload')}</Button>
  {current?.next?<Button accessibilityRole="button" onPress={()=>{setState(null);setCursor(current.next!);}} mode="outlined">{t('multi_recovery_next')}</Button>:null}
 </View>;
}
