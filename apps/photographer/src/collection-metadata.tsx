import {Button,Text,TextInput} from 'react-native-paper';
import {useEffect,useMemo,useRef,useState} from 'react';
import {View} from 'react-native';
import type {ApiClient} from '@photoeditor/api-client';
import type {Collection,CollectionItem} from './catalog-types';
import {CollectionMetadataAPI,type CollectionMetadataPatch} from './collection-metadata-api';
import {readDeadline} from './read-deadline';
import {useLocale} from './locale-context';
import {useReviewForeground} from './use-review-foreground';
const draftFor=(row:Collection):CollectionMetadataPatch=>{if(!['manual','captured_at','filename'].includes(row.sort_mode))throw Error('COLLECTION_SORT_INVALID');return {name:row.name,description:row.description,sort_mode:row.sort_mode as CollectionMetadataPatch['sort_mode'],cover_item_id:row.cover_item_id??null};};
export type CollectionMetadataProps={client:ApiClient;tenant:string;collection:Collection;items:CollectionItem[];canEdit:boolean;current:()=>boolean;acquire:()=>symbol|null;release:(token:symbol)=>void;onChanged:()=>void};
export function CollectionMetadata(props:CollectionMetadataProps){const foreground=useReviewForeground(),key=useMemo(()=>crypto.randomUUID(),[props.client,props.tenant,props.collection.id,props.collection.revision,foreground.generation]);return foreground.active?<MetadataScope key={key} {...props}/>:null;}
function MetadataScope(props:CollectionMetadataProps){
 const __catalogT=useLocale().t;

 const __uiMessage=useLocale().message;

 const {locale}=useLocale(),tr=(en:string,ru:string,bg:string)=>locale==='ru'?ru:locale==='bg'?bg:en,foreground=useReviewForeground();
 const api=useMemo(()=>new CollectionMetadataAPI(props.client,props.tenant,props.collection.project_id,props.collection.id),[props.client,props.tenant,props.collection.project_id,props.collection.id]);
 const [row,setRow]=useState(props.collection),[draft,setDraft]=useState<CollectionMetadataPatch>(()=>draftFor(props.collection)),[busy,setBusy]=useState(false),[uncertain,setUncertain]=useState(false),[confirm,setConfirm]=useState(false),[message,setMessage]=useState(''),[coverPage,setCoverPage]=useState(0);
 const state=useRef({row,draft,uncertain,busy});state.current={row,draft,uncertain,busy};
 const alive=useRef(true),flight=useRef<{controller:AbortController;token:symbol;release:CollectionMetadataProps['release']}|null>(null),live=useRef({props,foreground});live.current={props,foreground};
 const current=()=>alive.current&&live.current.props.current()&&live.current.foreground.active;
 useEffect(()=>{alive.current=true;return()=>{alive.current=false;const f=flight.current;flight.current=null;f?.controller.abort();if(f)f.release(f.token);};},[]);
 useEffect(()=>{if(current()&&props.canEdit)return;const f=flight.current;if(f){flight.current=null;f.controller.abort();f.release(f.token);setBusy(false);setUncertain(true);}},[props.canEdit,props.current,foreground.active,foreground.generation]);
 function stop(displayed:typeof flight.current){if(!displayed||flight.current!==displayed||!current())return;flight.current=null;displayed.controller.abort();displayed.release(displayed.token);setBusy(false);setUncertain(true);setMessage("Result uncertain. Reload before another change.");}
 async function act(kind:'save'|'archive'|'reload'|'publish'|'unpublish'){
  if(!current()||state.current.row!==row||flight.current||kind!=='reload'&&(!live.current.props.canEdit||state.current.uncertain||!['draft','published'].includes(state.current.row.state)))return;
  const token=props.acquire();if(!token)return;const f={controller:new AbortController(),token,release:props.release};flight.current=f;setBusy(true);setMessage('');const original=state.current.row,frozen={...state.current.draft},ids=live.current.props.items.map(v=>v.id);
  const check=()=>{f.controller.signal.throwIfAborted();if(flight.current!==f||!current()||kind!=='reload'&&!live.current.props.canEdit)throw Error('COLLECTION_SCOPE_WITHDRAWN');};
  try{await readDeadline(f.controller.signal,async signal=>{check();if(kind==='archive'){await api.archive(original,signal);check();setUncertain(true);setMessage("Archived; deletion request queued. Cleanup is asynchronous.");props.onChanged();return;}
   const result=kind==='reload'?await api.get(signal):kind==='publish'||kind==='unpublish'?await api.publishCollection(original,kind==='publish',signal):await api.update(original,frozen,ids,signal);check();setRow(result);setDraft(draftFor(result));setConfirm(false);setUncertain(false);props.onChanged();
  },kind==='reload'?8000:15000);}catch{if(flight.current===f&&current()){setUncertain(true);setMessage("Unable to confirm. Reload reads current state without repeating the change.");}}finally{if(flight.current===f){flight.current=null;f.release(token);if(alive.current)setBusy(false);}}
 }
 const displayed=flight.current,locked=busy||uncertain||!props.canEdit||!['draft','published'].includes(row.state),bound=()=>current()&&state.current.row===row&&!state.current.busy&&!state.current.uncertain&&live.current.props.canEdit&&['draft','published'].includes(state.current.row.state)&&!locked;
 return <View><Text accessibilityRole="header">{__catalogT('ui_8f21abd48ac6')}</Text><Text accessibilityRole="alert">{__uiMessage(message)}</Text><TextInput accessibilityLabel={__catalogT('ui_922214922b48')} value={draft.name} editable={!locked} onChangeText={name=>{if(bound())setDraft(old=>({...old,name}));}}/><TextInput accessibilityLabel={__catalogT('ui_e3fbf5a945f9')} value={draft.description} editable={!locked} onChangeText={description=>{if(bound())setDraft(old=>({...old,description}));}}/>
 {(['manual','captured_at','filename'] as const).map(sort=><Button key={sort} disabled={locked} onPress={()=>{if(bound())setDraft(old=>({...old,sort_mode:sort}));}}>{draft.sort_mode===sort?'✓ ':''}{({manual:__catalogT('ui_363493371020'),captured_at:__catalogT('ui_498435b38d98'),filename:__catalogT('ui_971b974235a8')})[sort]}</Button>)}
 <Text>{__catalogT('ui_0b91fdc231eb')}</Text><Button disabled={locked} onPress={()=>{if(bound())setDraft(old=>({...old,cover_item_id:null}));}}>{__catalogT('ui_b2ec3181b360')}</Button>{props.items.slice(coverPage*20,(coverPage+1)*20).map((item,index)=><Button key={item.id} disabled={locked} onPress={()=>{if(bound())setDraft(old=>({...old,cover_item_id:item.id}));}}>{draft.cover_item_id===item.id?'✓ ':''}{__catalogT('ui_d84eebada93e')} {coverPage*20+index+1}</Button>)}<Button disabled={locked||coverPage===0} onPress={()=>{if(bound())setCoverPage(p=>Math.max(0,p-1));}}>{__catalogT('ui_2c71dca38f78')}</Button><Button disabled={locked||(coverPage+1)*20>=props.items.length} onPress={()=>{if(bound())setCoverPage(p=>p+1);}}>{__catalogT('ui_74e3d3d38751')}</Button>
 <Text>{__catalogT('ui_578430e639c9')}</Text><Button disabled={locked||!Number.isSafeInteger(Number(row.revision))||row.state==='published'} onPress={()=>void act('publish')}>{__catalogT('ui_5937e95914c6')}</Button><Button disabled={locked||!Number.isSafeInteger(Number(row.revision))||row.state==='draft'} onPress={()=>void act('unpublish')}>{__catalogT('ui_6911df955a5f')}</Button>
 <Button disabled={locked} onPress={()=>void act('save')}>{__catalogT('ui_7239747549f7')}</Button><Button disabled={busy} onPress={()=>void act('reload')}>{__catalogT('ui_3937a210bbb2')}</Button>{busy&&<Button onPress={()=>stop(displayed)}>{__catalogT('ui_d4af24be4049')}</Button>}
 <Button disabled={locked||!Number.isSafeInteger(Number(row.revision))} onPress={()=>{if(bound())setConfirm(true);}}>{__catalogT('ui_68a316de9c16')}</Button>{confirm&&<><Text>{__catalogT('ui_ea26dfb78954')}</Text><Button disabled={locked} onPress={()=>void act('archive')}>{__catalogT('ui_6e85014d419a')}</Button></>}
 </View>;
}
