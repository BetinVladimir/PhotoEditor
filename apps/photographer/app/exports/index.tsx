import {useEffect,useMemo,useRef,useState} from 'react';
import {useLocalSearchParams} from 'expo-router';
import {FlatList,Pressable,SafeAreaView,Text,TextInput,View} from 'react-native';
import {PresetMetadataEditor} from '../../src/preset-metadata-editor';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import type {ExportPreset} from '../../src/catalog-types';
import {useLocale} from '../../src/locale-context';
import {catalogStyle as s} from '../../src/catalog-style';

const config:ExportPreset['config']={format:'jpeg',icc_id:'srgb_v1',quality:92,subsampling:'444',alpha:'matte',matte_rgb:[1,1,1],metadata_policy:'strip',naming_template:'{original_name}_edited'};
type PresetPage={items:ExportPreset[];next_cursor:string|null};
function readPresetPage(value:unknown,existing:ReadonlySet<string>,usedCursors:ReadonlySet<string>,requested?:string):PresetPage{
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('EXPORT_PRESET_PAGE_INVALID');
 const page=value as Record<string,unknown>;
 if(!Array.isArray(page.items)||page.items.length>100||!(page.next_cursor===null||typeof page.next_cursor==='string'&&page.next_cursor.length>0&&page.next_cursor.length<=4096)||page.items.length===0&&page.next_cursor!==null)throw new Error('EXPORT_PRESET_PAGE_INVALID');
 const ids=new Set(existing);
 for(const item of page.items){
  if(!item||typeof item!=='object'||typeof item.id!=='string'||!item.id||ids.has(item.id))throw new Error('EXPORT_PRESET_PAGE_INVALID');
  ids.add(item.id);
 }
 if(page.next_cursor&&(page.next_cursor===requested||usedCursors.has(page.next_cursor)))throw new Error('EXPORT_PRESET_CURSOR_REPEATED');
 return {items:page.items as ExportPreset[],next_cursor:page.next_cursor as string|null};
}

export default function Exports(){
 const {tenant,snapshot}=useLocalSearchParams<{tenant:string;snapshot?:string}>(),auth=useAuth();
 const client=useMemo(()=>auth.client(),[auth]);
 const api=useMemo(()=>new OwnerAPI(client,tenant||''),[client,tenant]);
 const generation=useMemo(()=>crypto.randomUUID(),[client]);
 return <ScopedExports key={JSON.stringify([generation,auth.origin,tenant,snapshot])} api={api} snapshot={snapshot}/>;
}

function ScopedExports({api,snapshot}:{api:OwnerAPI;snapshot?:string}){
 const {t}=useLocale();
 const [metadataPolicy,setMetadataPolicy]=useState<ExportPreset['config']['metadata_policy']>('strip');
 const [reload,setReload]=useState(0),[editing,setEditing]=useState<ExportPreset|null>(null);
 const [items,setItems]=useState<ExportPreset[]>([]),[cursor,setCursor]=useState<string|null>(null);
 const [name,setName]=useState('Web JPEG'),[busy,setBusy]=useState(false),[pageLoading,setPageLoading]=useState(true),[notice,setNotice]=useState('');
 const createAttempt=useRef<{intent:string;key:string}|null>(null),downloadAttempt=useRef<{intent:string;key:string}|null>(null);
 const pageFlight=useRef<AbortController|null>(null),usedCursors=useRef(new Set<string>());
 useEffect(()=>{
  let active=true;const controller=new AbortController();
  pageFlight.current?.abort();pageFlight.current=null;usedCursors.current.clear();
  setEditing(null);setItems([]);setCursor(null);setNotice('');setPageLoading(true);
  void api.presets(undefined,controller.signal).then(value=>{
   if(!active||controller.signal.aborted)return;
   const page=readPresetPage(value,new Set(),usedCursors.current);
   setItems(page.items);setCursor(page.next_cursor);
  }).catch(()=>{if(active&&!controller.signal.aborted)setNotice('Unable to load presets');}).finally(()=>{if(active&&!controller.signal.aborted)setPageLoading(false)});
  return()=>{active=false;controller.abort();pageFlight.current?.abort();pageFlight.current=null};
 },[api,reload]);
 async function loadMore(){
  if(!cursor||pageFlight.current||busy)return;
  const requested=cursor,controller=new AbortController();pageFlight.current=controller;setPageLoading(true);setNotice('');
  try{
   if(usedCursors.current.has(requested))throw new Error('EXPORT_PRESET_CURSOR_REPEATED');
   const value=await api.presets(requested,controller.signal);
   if(controller.signal.aborted)return;
   const page=readPresetPage(value,new Set(items.map(item=>item.id)),usedCursors.current,requested);
   usedCursors.current.add(requested);setItems(prior=>[...prior,...page.items]);setCursor(page.next_cursor);
  }catch{if(!controller.signal.aborted)setNotice('Unable to load presets')}
  finally{if(pageFlight.current===controller){pageFlight.current=null;setPageLoading(false)}}
 }
 async function create(){
  const trimmed=name.trim(),frozen={...config,metadata_policy:metadataPolicy},intent=JSON.stringify([trimmed,frozen]);
  if(createAttempt.current?.intent!==intent)createAttempt.current={intent,key:crypto.randomUUID()};
  const key=createAttempt.current.key;setBusy(true);
  try{const preset=await api.createPreset(trimmed,frozen,key);createAttempt.current=null;setItems(values=>[preset,...values]);setNotice('')}
  catch{setNotice('Unable to create preset')}
  finally{setBusy(false)}
 }
 async function start(preset:ExportPreset){
  if(preset.config.metadata_policy!=='strip'){setNotice(t('delivery_metadata_delivery_only'));return}
  if(!snapshot){setNotice('Open exports from a frozen selection snapshot.');return}
  const intent=JSON.stringify([snapshot,preset.id]);
  if(downloadAttempt.current?.intent!==intent)downloadAttempt.current={intent,key:crypto.randomUUID()};
  const key=downloadAttempt.current.key;setBusy(true);
  try{const out=await api.startDownload(snapshot,preset.id,key);downloadAttempt.current=null;setNotice(`Download job ${out.download_job_id.slice(0,8)} is ${out.status}.`)}
  catch{setNotice('Unable to start download job')}
  finally{setBusy(false)}
 }
 return <SafeAreaView style={s.root}><View style={s.header}>
  <Text style={s.title}>Exports</Text>
  <Pressable accessibilityRole="button" disabled={busy} onPress={()=>setReload(value=>value+1)}><Text>{t('preset_metadata_reload')}</Text></Pressable>
  <View style={s.toolbar}><TextInput value={name} onChangeText={setName} placeholder="Preset name" placeholderTextColor="#888" style={s.input}/><Pressable disabled={busy||pageLoading||!name.trim()} onPress={create} style={s.button}><Text style={s.buttonText}>Create</Text></Pressable></View>
  <Text style={s.subtitle}>{t('delivery_metadata_title')}</Text>
  {([['strip','delivery_metadata_strip'],['copyright','delivery_metadata_copyright'],['approved_exif','delivery_metadata_approved']] as const).map(([mode,label])=><Pressable key={mode} accessibilityRole="radio" accessibilityLabel={t(label)} accessibilityState={{checked:metadataPolicy===mode,disabled:busy}} disabled={busy} onPress={()=>setMetadataPolicy(mode)}><Text style={s.name}>{metadataPolicy===mode?'◉':'○'} {t(label)}</Text></Pressable>)}
  <Text style={s.meta}>{t('delivery_metadata_delivery_only')}</Text>
  {notice?<Text accessibilityRole={notice.startsWith('Unable')?'alert':undefined} style={notice.startsWith('Unable')?s.error:s.subtitle}>{notice}</Text>:null}
  </View>
  {editing?<PresetMetadataEditor api={api} preset={editing} onCancel={()=>setEditing(null)} onSaved={updated=>{setItems(values=>values.map(value=>value.id===updated.id?updated:value));setEditing(null)}}/>:null}
  <FlatList data={items} keyExtractor={item=>item.id} contentContainerStyle={s.list}
   ListEmptyComponent={!pageLoading&&!notice.startsWith('Unable')?<Text style={s.empty}>No export presets</Text>:null}
   renderItem={({item})=><View><Pressable disabled={busy||pageLoading} onPress={()=>start(item)} style={s.row}><View><Text style={s.name}>{item.name}</Text><Text style={s.meta}>{item.config.format.toUpperCase()} · {item.config.icc_id} · {t(item.config.metadata_policy==='copyright'?'delivery_metadata_copyright':item.config.metadata_policy==='approved_exif'?'delivery_metadata_approved':'delivery_metadata_strip')}</Text></View><Text style={s.chevron}>›</Text></Pressable><Pressable accessibilityRole="button" disabled={busy||pageLoading} onPress={()=>setEditing(item)}><Text>{t('preset_metadata_edit')}</Text></Pressable></View>}
   ListFooterComponent={cursor?<Pressable accessibilityRole="button" disabled={busy||pageLoading} onPress={()=>void loadMore()} style={s.button}><Text style={s.buttonText}>{t('delivery_create_more')}</Text></Pressable>:null}/>
 </SafeAreaView>;
}
