import {useEffect,useMemo,useState} from 'react';
import {Pressable,Text,View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import type {PerspectiveReplacementPage} from './perspective-replacement-page';
type Props={api:OwnerAPI;tenant:string;review:string;decisionHash:string;onOpen:(project:string,originalTask:string)=>void};
export function PerspectiveReplacementLinks(props:Props){
 const key=useMemo(()=>crypto.randomUUID(),[props.api,props.tenant,props.review,props.decisionHash]);
 return <Scope key={key} {...props}/>;
}
function Scope({api,tenant,review,decisionHash,onOpen}:Props){
 const [after,setAfter]=useState<string|null>(null),[refresh,setRefresh]=useState(0),[page,setPage]=useState<PerspectiveReplacementPage|null>(null),[failed,setFailed]=useState(false);
 useEffect(()=>{
  const controller=new AbortController();setPage(null);setFailed(false);
  void api.perspectiveReplacementPage(review,decisionHash,after,controller.signal).then(value=>{if(!controller.signal.aborted)setPage(value);}).catch(()=>{if(!controller.signal.aborted)setFailed(true);});
  return()=>controller.abort();
 },[api,tenant,review,decisionHash,after,refresh]);
 return <View accessibilityLabel="Perspective replacements">
  <Text accessibilityRole="header">Perspective replacements</Text>
  {!page&&!failed?<Text>Loading replacements…</Text>:null}
  {failed?<Text accessibilityRole="alert">Replacements are unavailable.</Text>:null}
  {page?.items.map(item=><View key={item.originalTask}><Text selectable>{item.originalTask}</Text>{item.scope?<Pressable accessibilityRole="button" onPress={()=>onOpen(item.project,item.originalTask)}><Text>Review perspective replacement</Text></Pressable>:<Text>Replacement is not available yet.</Text>}</View>)}
  {page?.items.length===0?<Text>No perspective replacements on this page.</Text>:null}
  {page?.next?<Pressable accessibilityRole="button" onPress={()=>setAfter(page.next)}><Text>Next replacements</Text></Pressable>:null}
  <Pressable accessibilityRole="button" onPress={()=>{setAfter(null);setRefresh(n=>n+1);}}><Text>Refresh replacements</Text></Pressable>
 </View>;
}
