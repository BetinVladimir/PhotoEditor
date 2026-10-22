import {TouchableRipple,Button,ActivityIndicator,Text} from 'react-native-paper';
import {APIError} from '@photoeditor/api-client';
import {useEffect,useRef,useState} from 'react';
import {FlatList,View} from 'react-native';
import type {CollectionItem} from './catalog-types';
import type {OwnerAPI} from './owner-api';
import {comparePreview} from './compare-preview';
import {compareFrames,type FrameComparison} from './compare-frames';
import {CompareFramesView} from './compare-frames-view';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
/** Mounted within a tenant/collection scope; decisions are local until explicitly saved. */
export function GalleryComparison({items,api,onSave,onAccessDenied,saving=false}:{items:CollectionItem[];onAccessDenied?:(error:APIError)=>void;onSave?:(decisions:FrameComparison['decisions'])=>void;saving?:boolean;api:Pick<OwnerAPI,'draft'|'mediaLinks'>}){
 const __uiT=useLocale().t;

 const accessDenied=useRef(onAccessDenied);accessDenied.current=onAccessDenied;
 const {t}=useLocale();const [selected,setSelected]=useState<string[]>([]);
 const [loaded,setLoaded]=useState<{selection:string[];items:CollectionItem[];api:typeof api;state:FrameComparison}>();
 const [error,setError]=useState(false);const [busy,setBusy]=useState(false);
 useEffect(()=>{
  const controller=new AbortController();let timer:ReturnType<typeof setTimeout>|undefined;
  setLoaded(undefined);setError(false);setBusy(false);
  if(selected.length<2)return()=>controller.abort();
  const chosen=selected.map(id=>items.find(item=>item.id===id));
  if(chosen.some(item=>!item)){setSelected([]);return()=>controller.abort();}
  setBusy(true);
  async function load(){
   try{
    // At most six frames in flight; do not fetch original/master artifacts.
    const previews=await Promise.all(chosen.map(async item=>{
     const frame=item!;
     const [draft,links]=await Promise.all([api.draft(frame.asset_id,controller.signal),api.mediaLinks(frame.asset_id,frame.asset_version_id,['preview1600'],controller.signal)]);
     if(controller.signal.aborted)throw new Error('COMPARE_CANCELLED');
     if(draft.asset_version_id!==frame.asset_version_id||links.items.length!==1)throw new Error('COMPARE_VERSION_CHANGED');
     return {frame:comparePreview(links.items[0],frame.asset_version_id,draft.source_sha256),expiry:Date.parse(links.items[0].expires_at)};
    }));
    if(controller.signal.aborted)return;
    const remaining=Math.min(...previews.map(value=>value.expiry))-Date.now();
    if(remaining<=0)throw new Error('COMPARE_EXPIRED');
    setLoaded({selection:selected,items,api,state:compareFrames(previews.map(value=>value.frame))});
    timer=setTimeout(()=>{setLoaded(undefined);setError(true);},Math.min(remaining,2147483647));
   }catch(error){if(!controller.signal.aborted){setLoaded(undefined);setError(true);if(error instanceof APIError&&[401,403].includes(error.status)){setBusy(false);controller.abort();accessDenied.current?.(error);}}}
   finally{if(!controller.signal.aborted)setBusy(false);}
  }
  void load();return()=>{controller.abort();if(timer)clearTimeout(timer);};
 },[selected,items,api]);
 const state=loaded?.selection===selected&&loaded.items===items&&loaded.api===api?loaded.state:undefined;
 return <View style={{gap:8}}><Text style={s.name}>{t('compare_title')}</Text><Text style={s.meta}>{t('compare_select_hint')}</Text>
  <FlatList horizontal data={items} keyExtractor={item=>item.id} initialNumToRender={10} maxToRenderPerBatch={10} windowSize={5} contentContainerStyle={{gap:6}} renderItem={({item})=><TouchableRipple key={item.id} accessibilityRole="button" accessibilityLabel={__uiT('ui_8cbaa55a52ef', {p0:(t('compare_select')),p1:(item.position+1)})} accessibilityState={{selected:selected.includes(item.id),disabled:!selected.includes(item.id)&&selected.length>=6}} disabled={!selected.includes(item.id)&&selected.length>=6} onPress={()=>setSelected(current=>current.includes(item.id)?current.filter(id=>id!==item.id):current.length<6?[...current,item.id]:current)} style={s.button}><View style={{alignItems:'center',justifyContent:'center'}}><Text style={s.buttonText}>{selected.includes(item.id)?'✓ ':''}{t('compare_frame')} {item.position+1}</Text></View></TouchableRipple>}/>
  {selected.length?<Button accessibilityRole="button" onPress={()=>setSelected([])}    mode="contained" contentStyle={{minHeight:48}}>{t('compare_clear')}</Button>:null}
  {busy?<ActivityIndicator/>:null}{error?<Text accessibilityRole="alert" style={s.error}>{t('compare_unavailable')}</Text>:null}
  {state?<>{onSave?<Button accessibilityRole="button" disabled={saving||Object.keys(state.decisions).length===0} onPress={()=>onSave({...state.decisions})}    mode="contained" contentStyle={{minHeight:48}}>{t(saving?'compare_saving':'compare_save')}</Button>:null}<Text style={s.meta}>{t('compare_local_decisions')}</Text><CompareFramesView state={state} onChange={state=>setLoaded(value=>value?{...value,state}:value)}/></>:null}
 </View>;
}
