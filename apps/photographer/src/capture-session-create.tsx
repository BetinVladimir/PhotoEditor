import {useEffect,useRef,useState} from 'react';
import {ActivityIndicator,Pressable,Text,TextInput,View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import type {Page} from './catalog-types';
import type {ShootSession,ShootSessionCreate} from './capture-types';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
export function CaptureSessionCreate({api,project,sessions,onCreated,onBusy}:{api:OwnerAPI;project:string;sessions:Array<{id:string;name:string}>;onCreated:(session:ShootSession)=>void;onBusy:(busy:boolean)=>void}){
 const {t}=useLocale();
 const [name,setName]=useState(''),[collection,setCollection]=useState(''),[template,setTemplate]=useState('');
 const [collections,setCollections]=useState<Array<{id:string;name:string}>>([]);
 const [loading,setLoading]=useState(true),[busy,setBusy]=useState(false),[error,setError]=useState('');
 const [retry,setRetry]=useState(0);const generation=useRef(0);const submitting=useRef(false);
 const attempt=useRef<{fingerprint:string;request:ShootSessionCreate;key:string}|null>(null);
 useEffect(()=>{
  const controller=new AbortController();generation.current++;setLoading(true);setCollections([]);setCollection('');setTemplate('');attempt.current=null;setError('');
  async function load(){
   const found:Array<{id:string;name:string}>=[];const ids=new Set<string>(),cursors=new Set<string>();let cursor:string|undefined;
   do{
    const page:Page<{id:string;name:string}>=await api.get(`/api/v1/tenants/{t}/collections?limit=100&project_id=${encodeURIComponent(project)}${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`,controller.signal);
    for(const item of page.items)if(!ids.has(item.id)){ids.add(item.id);found.push(item)}
    cursor=page.next_cursor??undefined;
    if(cursor){if(cursors.has(cursor))throw new Error('Repeated collection cursor');cursors.add(cursor)}
   }while(cursor&&!controller.signal.aborted);
   if(!controller.signal.aborted)setCollections(found);
  }
  void load().catch(()=>{if(!controller.signal.aborted)setError('capture_collections_error')}).finally(()=>{if(!controller.signal.aborted)setLoading(false)});
  return()=>{generation.current++;controller.abort()};
 },[api,project,retry]);
 async function create(){
  if(submitting.current||loading||!name.trim()||!collection||!template)return;
  submitting.current=true;setBusy(true);onBusy(true);setError('');const scope=generation.current;
  const fingerprint=JSON.stringify([name.trim(),collection,template]);
  try{
   if(attempt.current?.fingerprint!==fingerprint){
    const source=await api.shootSession(template);
    if(scope!==generation.current)return;
    if(source.project_id!==project||!source.capture_policy){setError('capture_template_unavailable');return;}
    attempt.current={fingerprint,key:crypto.randomUUID(),request:{project_id:project,collection_id:collection,name:name.trim(),policy:source.policy,capture_policy:source.capture_policy}};
   }
   const saved=await api.createShootSession(attempt.current.request,attempt.current.key);
   if(scope!==generation.current)return;
   attempt.current=null;onCreated(saved);
  }catch{if(scope===generation.current)setError('capture_create_error')}
  finally{submitting.current=false;if(scope===generation.current){setBusy(false);onBusy(false)}}
 }
 return <View style={{gap:8}}>
  <Text accessibilityRole="header" style={s.name}>{t('capture_create_session')}</Text>
  <Text style={s.meta}>{t('capture_create_description')}</Text>
  <TextInput accessibilityLabel={t('capture_session_name')} value={name} onChangeText={setName} maxLength={200} editable={!busy} style={s.input}/>
  <Text style={s.name}>{t('capture_choose_collection')}</Text>
  {loading?<ActivityIndicator/>:collections.map(item=><Pressable key={item.id} accessibilityRole="radio" aria-checked={collection===item.id} accessibilityState={{checked:collection===item.id,disabled:busy}} disabled={busy} style={s.button} onPress={()=>setCollection(item.id)}><Text style={s.buttonText}>{collection===item.id?'✓ ':''}{item.name}</Text></Pressable>)}
  {!loading&&!collections.length?<Text style={s.meta}>{t('capture_no_collections')}</Text>:null}
  <Text style={s.name}>{t('capture_choose_template')}</Text>
  {sessions.map(item=><Pressable key={item.id} accessibilityRole="radio" aria-checked={template===item.id} accessibilityState={{checked:template===item.id,disabled:busy}} disabled={busy} style={s.button} onPress={()=>setTemplate(item.id)}><Text style={s.buttonText}>{template===item.id?'✓ ':''}{item.name}</Text></Pressable>)}
  {!sessions.length?<Text style={s.meta}>{t('capture_no_template')}</Text>:null}
  {error?<Text accessibilityRole="alert" style={s.error}>{t(error as Parameters<typeof t>[0])}</Text>:null}
  {error==='capture_collections_error'?<Pressable accessibilityRole="button" disabled={busy} onPress={()=>setRetry(value=>value+1)} style={s.button}><Text style={s.buttonText}>{t('capture_refresh')}</Text></Pressable>:null}
  <Pressable accessibilityRole="button" disabled={busy||loading||!name.trim()||!collection||!template} style={s.button} onPress={()=>void create()}><Text style={s.buttonText}>{t('capture_create_session')}</Text></Pressable>
 </View>;
}
