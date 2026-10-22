import {ActivityIndicator,Button,Text} from 'react-native-paper';
import {useEffect,useMemo,useRef,useState} from 'react';
import {View} from 'react-native';
import type {ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import {linkUUID} from './collection-access-links-api';
import {useReadResource} from './use-read-resource';
import {useLocale} from './locale-context';
/** Preset names are current authorized metadata. Existing selected identities
 * survive paging; no secret, arbitrary object key or guessed preset is used. */
type Props={client:ApiClient;tenant:string;value:string[];onChange:(value:string[])=>void;disabled:boolean};
export function SharePresetPicker(props:Props){
 const instance=useMemo(()=>crypto.randomUUID(),[props.client,props.tenant]);
 return <SharePresetPickerScope key={instance} {...props}/>;
}
function SharePresetPickerScope({client,tenant,value,onChange,disabled}:Props){
 const __catalogT=useLocale().t;

 const {locale}=useLocale(),tr=(en:string,ru:string,bg:string)=>locale==='ru'?ru:locale==='bg'?bg:en,api=useMemo(()=>new OwnerAPI(client,tenant),[client,tenant]);
 const [cursors,setCursors]=useState<(string|undefined)[]>([undefined]),[page,setPage]=useState(0),cursor=cursors[page];
 const resource=useReadResource(`link-presets:${tenant}:${cursor??''}`,api,async signal=>{const result=await api.presets(cursor,signal);signal.throwIfAborted();if(!Array.isArray(result.items)||result.items.length>100||result.items.some(v=>!linkUUID(v.id)||typeof v.name!=='string'||!v.name.trim())||new Set(result.items.map(v=>v.id)).size!==result.items.length||result.next_cursor!==null&&result.next_cursor!==undefined&&(typeof result.next_cursor!=='string'||!result.next_cursor||result.next_cursor.length>4096||cursors.slice(0,page+1).includes(result.next_cursor)||!result.items.length))throw Error('PRESET_PAGE_INVALID');return result;});
 const alive=useRef(true),latest=useRef({resource,value,onChange,disabled});latest.current={resource,value,onChange,disabled};
 useEffect(()=>{alive.current=true;return()=>{alive.current=false;};},[]);
 const current=()=>alive.current&&resource.current()&&latest.current.resource.current()&&resource.phase==='ready';
 function select(id:string){
  const live=latest.current;
  if(!current()||live.disabled||!resource.data?.items.some(item=>item.id===id))return;
  const selected=live.value.includes(id);
  if(!selected&&live.value.length>=100)return;
  live.onChange(selected?live.value.filter(value=>value!==id):[...live.value,id]);
 }
 return <View><Text>{__catalogT('ui_9bb435942aac')} · {value.length} {__catalogT('ui_d7cbbb688b2e')}</Text>{resource.phase==='loading'?<><ActivityIndicator/><Button accessibilityRole="button" onPress={resource.cancel}>{__catalogT('ui_9c66846ecbf4')}</Button></>:resource.phase!=='ready'?<Button accessibilityRole="button" onPress={resource.retry}>{__catalogT('ui_a12704a3faf7')}</Button>:<>{!resource.data!.items.length&&<Text>{__catalogT('ui_58df1e930438')}</Text>}{resource.data!.items.map(preset=><Button accessibilityRole="button" key={preset.id} disabled={disabled||(!value.includes(preset.id)&&value.length>=100)} onPress={()=>select(preset.id)}>{value.includes(preset.id)?'✓ ':''}{preset.name}</Button>)}<Button accessibilityRole="button" disabled={page===0} onPress={()=>{if(current()&&page>0)setPage(page-1);}}>{__catalogT('ui_386db201599c')}</Button><Button accessibilityRole="button" disabled={!resource.data!.next_cursor} onPress={()=>{const next=resource.data!.next_cursor;if(current()&&next){setCursors(old=>[...old.slice(0,page+1),next]);setPage(v=>v+1);}}}>{__catalogT('ui_b83931930ce6')}</Button></>}</View>;
}
