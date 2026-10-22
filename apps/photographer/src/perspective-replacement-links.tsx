import {useLocale} from './locale-context';
import {Button,Text} from 'react-native-paper';
import {readDeadline} from './read-deadline';
import {useEffect,useMemo,useRef,useState} from 'react';
import {View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import type {PerspectiveReplacementPage} from './perspective-replacement-page';
type Props={api:OwnerAPI;tenant:string;review:string;decisionHash:string;onOpen:(project:string,originalTask:string)=>void};
export function PerspectiveReplacementLinks(props:Props){
 const key=useMemo(()=>crypto.randomUUID(),[props.api,props.tenant,props.review,props.decisionHash]);
 return <Scope key={key} {...props}/>;
}
function Scope({api,tenant,review,decisionHash,onOpen}:Props){
 const __uiT=useLocale().t;

 const [after,setAfter]=useState<string|null>(null),[refresh,setRefresh]=useState(0),[page,setPage]=useState<PerspectiveReplacementPage|null>(null),[failed,setFailed]=useState(false);
 const currentRead=useRef<AbortController|null>(null),used=useRef(new Set<string>());
 useEffect(()=>{
  const controller=new AbortController();currentRead.current=controller;setPage(null);setFailed(false);
  void readDeadline(controller.signal,signal=>api.perspectiveReplacementPage(review,decisionHash,after,signal)).then(value=>{if(!controller.signal.aborted){if(value.next&&(value.next===after||used.current.has(value.next)))throw new Error('Replacement cursor cycle');if(after)used.current.add(after);setPage(value);}}).catch(()=>{if(!controller.signal.aborted)setFailed(true);});
  return()=>{controller.abort();if(currentRead.current===controller)currentRead.current=null;};
 },[api,tenant,review,decisionHash,after,refresh]);
 return <View accessibilityLabel={__uiT('ui_fe6c6c6c4679')}>
  <Text accessibilityRole="header">{__uiT('ui_fe6c6c6c4679')}</Text>
  {!page&&!failed?<View><Text>{__uiT('ui_d33749bcd15e')}</Text><Button accessibilityRole="button" onPress={()=>{currentRead.current?.abort();currentRead.current=null;setFailed(true);}} mode="outlined">{__uiT('ui_9131e78c0500')}</Button></View>:null}
  {failed?<View><Text accessibilityRole="alert">{__uiT('ui_c70dca2b0887')}</Text><Button accessibilityRole="button" onPress={()=>setRefresh(n=>n+1)} mode="outlined">{__uiT('ui_3873d836c1a5')}</Button></View>:null}
  {page?.items.map(item=><View key={item.originalTask}><Text selectable>{item.originalTask}</Text>{item.scope?<Button accessibilityRole="button" onPress={()=>onOpen(item.project,item.originalTask)} mode="outlined">{__uiT('ui_5da60458009a')}</Button>:<Text>{__uiT('ui_cdb9a0b8048d')}</Text>}</View>)}
  {page?.items.length===0?<Text>{__uiT('ui_f771861e092a')}</Text>:null}
  {page?.next?<Button accessibilityRole="button" onPress={()=>setAfter(page.next)} mode="outlined">{__uiT('ui_5efa068f682d')}</Button>:null}
  <Button accessibilityRole="button" onPress={()=>{used.current.clear();setAfter(null);setRefresh(n=>n+1);}} mode="outlined">{__uiT('ui_e5d82292beb9')}</Button>
 </View>;
}
