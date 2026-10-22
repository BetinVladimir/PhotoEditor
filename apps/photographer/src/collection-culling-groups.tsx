import {ActivityIndicator,Button,Text} from 'react-native-paper';
import {useEffect,useMemo,useRef,useState} from 'react';
import {FlatList,View} from 'react-native';
import {router} from './localized-router';
import type {ApiClient} from '@photoeditor/api-client';
import type {Collection,CollectionItem} from './catalog-types';
import {collectionGroups,type CollectionGroupItem} from './collection-culling-groups-api';
import {useReadResource} from './use-read-resource';
import {useLocale} from './locale-context';
import {SceneFramePreview} from './scene-frame-preview';
import {OwnerAPI} from './owner-api';
type Props={client:ApiClient;tenant:string;collection:Collection;items:CollectionItem[];current:()=>boolean};
export function CollectionCullingGroups(props:Props){const key=useMemo(()=>crypto.randomUUID(),[props.client,props.tenant,props.collection.id,props.collection.revision]);return <GroupScope key={key} {...props}/>;}
function GroupScope(props:Props){
 const __catalogT=useLocale().t;

 const {locale}=useLocale(),tr=(en:string,ru:string,bg:string)=>locale==='ru'?ru:locale==='bg'?bg:en;
 const [cursor,setCursor]=useState<string|undefined>(),[history,setHistory]=useState<Array<string|undefined>>([]),[selected,setSelected]=useState<string|undefined>(),[group,setGroup]=useState(''),[mode,setMode]=useState<'scene_id'|'exact_group_id'|'burst_group_id'>('burst_group_id');
 const alive=useRef(true),live=useRef(props);live.current=props;useEffect(()=>{alive.current=true;return()=>{alive.current=false}},[]);const current=()=>alive.current&&live.current.current();
 const api=useMemo(()=>new OwnerAPI(props.client,props.tenant),[props.client,props.tenant]);
 const connection=useMemo(()=>({client:props.client,cursor}),[props.client,cursor]);
 const page=useReadResource(`${props.tenant}:${props.collection.id}:groups`,connection,signal=>collectionGroups(props.client,props.tenant,props.collection.id,props.collection.revision,signal,{cursor}));
 const selectedConnection=useMemo(()=>({client:props.client,selected}),[props.client,selected]);
 const detail=useReadResource(`${props.tenant}:${props.collection.id}:group`,selectedConnection,signal=>selected?collectionGroups(props.client,props.tenant,props.collection.id,props.collection.revision,signal,{review:selected}):Promise.resolve(null));
 const liveMode=useRef(mode);liveMode.current=mode;
 const context=detail.phase==='ready'?detail.data?.contexts[0]:undefined;
 const members=new Map(props.items.map(item=>[item.id,item]));
 const exact=(item:CollectionGroupItem)=>{const m=members.get(item.collection_item_id);return m?.asset_id===item.asset_id&&m.asset_version_id===item.asset_version_id;};
 const invalid=context?.items?.some(item=>!exact(item));
 const choices=new Map<string,number>();context?.items?.forEach(item=>choices.set(item[mode],(choices.get(item[mode])??0)+1));
 const items=invalid?[]:context?.items?.filter(item=>!group||item[mode]===group)??[];
 const choose=(review:string)=>{if(!current()||page.phase!=='ready'||!page.current()||!page.data?.contexts.some(c=>c.review_id===review))return;setSelected(review);setGroup('');};
 return <View><Text accessibilityRole="header">{__catalogT('ui_365c50d71c95')}</Text>
 {page.phase==='loading'?<><ActivityIndicator/><Button onPress={page.cancel}>{__catalogT('ui_9f9751ad84ed')}</Button></>:page.phase!=='ready'?<Button onPress={page.retry}>{__catalogT('ui_5a55b25da531')}</Button>:current()?<>
 {!page.data!.contexts.length?<Text>{__catalogT('ui_6a8ba0441a45')}</Text>:null}
 {page.data!.contexts.map((c,index)=><Button key={c.review_id} onPress={()=>choose(c.review_id)}>{selected===c.review_id?'✓ ':''}{__catalogT('ui_e70d8eb90500')} {index+1} · {c.matched}/{c.total} · {c.state==='pending'?__catalogT('ui_62da294e521b'):__catalogT('ui_d790b0dd9382')}</Button>)}
 <Button disabled={!history.length} onPress={()=>{if(!current()||!page.current()||!history.length)return;setCursor(history.at(-1));setHistory(h=>h.slice(0,-1));setSelected(undefined);setGroup('');}}>{__catalogT('ui_154beab20171')}</Button>
 <Button disabled={!page.data!.next_cursor} onPress={()=>{if(!current()||!page.current()||!page.data?.next_cursor)return;setHistory(h=>[...h,cursor]);setCursor(page.data.next_cursor);setSelected(undefined);setGroup('');}}>{__catalogT('ui_0e082a6eab7c')}</Button>
 </>:null}
 {selected&&detail.phase==='loading'?<><ActivityIndicator/><Button onPress={detail.cancel}>{__catalogT('ui_c5cbfe14aba4')}</Button></>:selected&&detail.phase!=='ready'?<Button onPress={detail.retry}>{__catalogT('ui_df7ef4ebae05')}</Button>:null}
 {context&&current()&&detail.current()?<><Text>{context.matched}/{context.total} · {__catalogT('ui_e88dd2d6d5b2')}: {context.unmatched}</Text>
 {invalid?<Text>{__catalogT('ui_6424762bd060')}</Text>:<>
 {(['scene_id','exact_group_id','burst_group_id'] as const).map(key=><Button key={key} onPress={()=>{if(!current()||!detail.current())return;setMode(key);setGroup('');}}>{key==='scene_id'?__catalogT('ui_160c0535e5c3'):key==='exact_group_id'?__catalogT('ui_93c28c39252a'):__catalogT('ui_f320d4b82f62')}</Button>)}
 <Button onPress={()=>{if(current()&&detail.current())setGroup('');}}>{__catalogT('ui_1b492be77224')}</Button>{[...choices].map(([key,count],index)=><Button key={key} onPress={()=>{if(current()&&detail.current()&&liveMode.current===mode)setGroup(key);}}>{index+1} · {count}</Button>)}
 {!items.length?<Text>{__catalogT('ui_7cdbe526ba9a')}</Text>:null}
 <FlatList data={items} keyExtractor={item=>item.collection_item_id} initialNumToRender={8} windowSize={5} renderItem={({item})=><View><Text>{__catalogT('ui_d84eebada93e')} {members.get(item.collection_item_id)!.position+1} · {__catalogT('ui_a4130d7d2c3a')} {item.rank}</Text><SceneFramePreview api={api} frame={{assetId:item.asset_id,versionId:item.asset_version_id,ordinal:members.get(item.collection_item_id)!.position}} onAccessDenied={()=>{if(current()&&detail.current()){detail.cancel();page.cancel();}}}/></View>}/>
 </>}
 <Button onPress={()=>{if(!current()||!detail.current()||context.review_id!==selected)return;router.push({pathname:'/reviews/[id]',params:{id:context.review_id,tenant:props.tenant}});}}>{__catalogT('ui_91431c41c11b')}</Button>
 </>:null}
 </View>;
}
