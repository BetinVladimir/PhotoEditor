import {ActivityIndicator,Button,Text,TextInput} from 'react-native-paper';
import {useEffect,useMemo,useRef,useState,type ReactNode} from 'react';
import {View} from 'react-native';
import type {ApiClient} from '@photoeditor/api-client';
import type {Collection,CollectionItem} from './catalog-types';
import {useReadResource} from './use-read-resource';
import {useLocale} from './locale-context';
import {loadCollectionGallery,type GalleryFilter} from './collection-gallery-api';
const empty:GalleryFilter={search:'',review:'',visibility:'',rating:'',flag:'',tag:'',section_id:''};
type CollectionGalleryProps={client:ApiClient;tenant:string;collection:Collection;items:CollectionItem[];current:()=>boolean;children:(items:CollectionItem[])=>ReactNode};
export function CollectionGallery(props:CollectionGalleryProps){
 const instance=useMemo(()=>crypto.randomUUID(),[props.client,props.tenant,props.collection.id]);
 return <GalleryScope key={instance} {...props}/>;
}
function GalleryScope(props:CollectionGalleryProps){
 const __catalogT=useLocale().t;

 const {locale}=useLocale(),tr=(en:string,ru:string,bg:string)=>locale==='ru'?ru:locale==='bg'?bg:en;
 const [draft,setDraft]=useState(empty),[filter,setFilter]=useState(empty);
 const live=useRef({props,draft});live.current={props,draft};const alive=useRef(true);
 useEffect(()=>{alive.current=true;return()=>{alive.current=false;};},[]);
 const current=()=>alive.current&&live.current.props.current();
 const connection=useMemo(()=>({client:props.client,collection:props.collection,items:props.items,filter}),[props.client,props.collection,props.items,filter]);
 const read=useReadResource(`${props.tenant}:${props.collection.id}:${props.collection.revision}:gallery`,connection,async signal=>{
  if(!current())throw Error('GALLERY_SCOPE_CHANGED');
  const items=Object.values(filter).some(Boolean)?await loadCollectionGallery(props.client,props.tenant,props.collection,filter,signal):props.items;
  signal.throwIfAborted();if(!current())throw Error('GALLERY_SCOPE_CHANGED');return items;
 });
 const apply=()=>{if(current())setFilter({...live.current.draft})};
 const select=(key:keyof GalleryFilter,value:string)=>{if(current())setDraft(old=>({...old,[key]:value}))};
 return <View><Text>{__catalogT('ui_8d47b09d51a7')}</Text>
 {props.collection.sort_mode==='captured_at'?<Text>{__catalogT('ui_2a76cb037b4a')}</Text>:null}
 <TextInput accessibilityLabel={__catalogT('ui_fbcc4b62036c')} value={draft.search} onChangeText={v=>select('search',v)} maxLength={200}/>
 <TextInput accessibilityLabel={__catalogT('ui_5d676e087f90')} value={draft.tag} onChangeText={v=>select('tag',v)} maxLength={64}/>
 <View>{(['','pending_review','accepted','rejected'] as const).map(value=><Button key={value} accessibilityRole="button" onPress={()=>select('review',value)}>{draft.review===value?'✓ ':''}{value==='pending_review'?__catalogT('ui_f1c45f3f1314'):value==='accepted'?__catalogT('ui_a00fb0c50741'):value==='rejected'?__catalogT('ui_aea4a04a8042'):__catalogT('ui_1ee5677346f3')}</Button>)}</View>
 <View>{(['','visible','hidden'] as const).map(value=><Button key={value} accessibilityRole="button" onPress={()=>select('visibility',value)}>{draft.visibility===value?'✓ ':''}{value==='visible'?__catalogT('ui_8411f5abb4c7'):value==='hidden'?__catalogT('ui_7e6fefff0f0a'):__catalogT('ui_bbf571dc1bda')}</Button>)}</View>
 <View>{['','0','1','2','3','4','5'].map(value=><Button key={value} accessibilityRole="button" onPress={()=>select('rating',value)}>{draft.rating===value?'✓ ':''}{value===''?__catalogT('ui_a90e7e92a678'):value+'★'}</Button>)}</View>
 <View>{['','none','pick','reject'].map(value=><Button key={value} accessibilityRole="button" onPress={()=>select('flag',value)}>{draft.flag===value?'✓ ':''}{value==='none'?__catalogT('ui_4d6f6e4f772d'):value==='pick'?__catalogT('ui_831a9d52a9c3'):value==='reject'?__catalogT('ui_2aa188d85f41'):__catalogT('ui_36dce7f5cd9a')}</Button>)}</View>
 <Button accessibilityRole="button" onPress={apply}>{__catalogT('ui_25d9f9e76c10')}</Button>
 <Button accessibilityRole="button" onPress={()=>{if(current()){setDraft(empty);setFilter({...empty});}}}>{__catalogT('ui_b92325bdab93')}</Button>
 <Text>{__catalogT('ui_c26751e18080')}</Text>
 {read.phase==='loading'?<View><ActivityIndicator/><Button accessibilityRole="button" onPress={read.cancel}>{__catalogT('ui_b28b2754b5c8')}</Button></View>:read.phase!=='ready'?<Button accessibilityRole="button" onPress={read.retry}>{__catalogT('ui_092be6522f7a')}</Button>:current()?<><Text>{read.data!.length} / {props.items.length}</Text>{props.children(read.data!)}</>:null}
 </View>;
}
