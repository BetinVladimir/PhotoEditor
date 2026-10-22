import {Button,Text,TextInput} from 'react-native-paper';
import {abandonSectionCreation} from './section-create-abandon';
import {useLocale} from './locale-context';
import {useEffect,useMemo,useRef,useState} from 'react';
import {View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {APIError,type ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import {CollectionOrganizationAPI,organizationPosition,type OrganizationSection,type OrganizationItem} from './collection-organization-api';
import {SectionCreateAttemptStore,type SectionCreateAttempt} from './section-create-attempt-store';
import {useReadResource} from './use-read-resource';
import {readDeadline} from './read-deadline';
export type CollectionOrganizationProps={client:ApiClient;origin:string;tenant:string;collectionId:string;canEdit:boolean;current:()=>boolean;onChanged:()=>void;acquire:()=>symbol|null;release:(token:symbol)=>void};
export function CollectionOrganization(props:CollectionOrganizationProps){
 const __catalogT=useLocale().t;

 const __uiMessage=useLocale().message;

 const {locale}=useLocale();
 const tr=(en:string,ru:string,bg:string)=>locale==='ru'?ru:locale==='bg'?bg:en;
 const api=useMemo(()=>new CollectionOrganizationAPI(props.client,props.tenant,props.collectionId),[props.client,props.tenant,props.collectionId]);
 const owner=useMemo(()=>new OwnerAPI(props.client,props.tenant),[props.client,props.tenant]);
 const read=useReadResource(`${props.origin}:${props.tenant}:${props.collectionId}`,api,async signal=>{
  const me=await owner.me(signal);signal.throwIfAborted();
  const store=new SectionCreateAttemptStore(AsyncStorage,props.origin,me.id,props.tenant,props.collectionId);
  const attempt=await store.read();signal.throwIfAborted();const state=await api.load(signal);return {store,attempt,state};
 });
 const [name,setName]=useState(''),[position,setPosition]=useState('0'),[message,setMessage]=useState(''),[busy,setBusy]=useState(false),[sectionPage,setSectionPage]=useState(0),[itemPage,setItemPage]=useState(0);
 const flight=useRef<{controller:AbortController;token:symbol}|null>(null),known=useRef<{scope:string;attempt:SectionCreateAttempt}|null>(null),alive=useRef(true);
 const scope=useMemo(()=>({api,origin:props.origin}),[api,props.origin]),live=useRef({props,read,scope});live.current={props,read,scope};
 const knownScope=useRef(scope);if(knownScope.current!==scope){knownScope.current=scope;known.current=null;}
 useEffect(()=>{alive.current=true;return()=>{alive.current=false;const f=flight.current;flight.current=null;f?.controller.abort();if(f)props.release(f.token);};},[scope]);
 const current=()=>alive.current&&live.current.scope===scope&&read.current()&&live.current.read.current()&&live.current.props.current();
 const allowed=(recovery=false)=>current()&&live.current.props.canEdit&&read.phase==='ready'&&(recovery||['draft','published'].includes(read.data!.state.state));
 function stop(f:typeof flight.current){if(!f||flight.current!==f||!current())return;flight.current=null;f.controller.abort();props.release(f.token);setBusy(false);setMessage("The result is uncertain. Reload before making another change.");read.retry();}
 async function run(action:(signal:AbortSignal,check:()=>void)=>Promise<void>,recovery=false){
  if(!allowed(recovery)||flight.current)return;const token=props.acquire();if(!token)return;
  const f={controller:new AbortController(),token};flight.current=f;setBusy(true);setMessage('');
  const owns=()=>flight.current===f&&!f.controller.signal.aborted&&current();
  try{await readDeadline(f.controller.signal,async signal=>{const check=()=>{signal.throwIfAborted();if(!owns()||!live.current.props.canEdit)throw new Error('ORGANIZATION_AUTHORITY_WITHDRAWN');};check();await action(signal,check);check();},15000);if(owns()){setMessage("Reloading the current collection.");read.retry();props.onChanged();}}
  catch(error){if(owns()){setMessage(error instanceof APIError&&error.status===409?"The collection changed. Review the current order before trying again.":"The result is uncertain. Reload to reconcile it.");read.retry();}}
  finally{if(flight.current===f){flight.current=null;props.release(token);if(alive.current)setBusy(false);}}
 }
 async function create(signal:AbortSignal,check:()=>void){
  const data=read.data!;check();const saved=await data.store.read();check();
  const remembered=known.current?.scope===data.store.storageKey?known.current.attempt:null;
  if(saved&&remembered&&(saved.key!==remembered.key||JSON.stringify(saved.request)!==JSON.stringify(remembered.request)||saved.ticket&&remembered.ticket&&saved.ticket.resource_id!==remembered.ticket.resource_id))throw new Error('SECTION_JOURNAL_CONFLICT');
  let attempt=remembered?.ticket?remembered:saved??remembered;let firstFresh=false;
  if(!attempt){if(!Number.isSafeInteger(Number(data.state.revision)))throw new Error('REVISION_UNSAFE');const n=name.trim(),p=organizationPosition(position);if(!n||Array.from(n).length>120)throw new Error('NAME_INVALID');attempt={schema_version:1,key:crypto.randomUUID(),request:{name:n,position:p,expected_revision:Number(data.state.revision)},ticket:null};firstFresh=true;check();await data.store.write(attempt);known.current={scope:data.store.storageKey,attempt};check();}
  known.current={scope:data.store.storageKey,attempt};
  if(!attempt.ticket){
   let section:OrganizationSection;
   try{check();section=await api.create(attempt.request,attempt.key,signal);}catch(error){
    if(firstFresh&&error instanceof APIError&&(error.status===409&&error.code==='REVISION_CONFLICT'||error.status===422&&error.code==='VALIDATION_FAILED')){check();await data.store.discardRejected(attempt.key);check();known.current=null;}throw error;
   }
   attempt={...attempt,ticket:{resource_id:section.id}};known.current={scope:data.store.storageKey,attempt};check();await data.store.write(attempt);check();
  }else {check();await data.store.write(attempt);check();}
  check();if(!attempt.ticket)throw new Error('SECTION_RECEIPT_REQUIRED');await data.store.clearAccepted(attempt.key,attempt.ticket.resource_id);check();known.current=null;
 }

 async function abandon(signal:AbortSignal,check:()=>void){
  const data=read.data!;check();const saved=await data.store.read();check();const remembered=known.current?.scope===data.store.storageKey?known.current.attempt:null;
  if(saved&&remembered&&(saved.key!==remembered.key||JSON.stringify(saved.request)!==JSON.stringify(remembered.request)))throw new Error('SECTION_JOURNAL_CONFLICT');
  let attempt=remembered?.ticket?remembered:saved??remembered;if(!attempt)throw new Error('SECTION_ATTEMPT_REQUIRED');
  if(!attempt.ticket){check();const result=await abandonSectionCreation(props.client,props.tenant,props.collectionId,attempt.request,attempt.key,signal);
   if(result.status==='abandoned'){check();await data.store.clearAbandoned(attempt.key);check();known.current=null;return;}
   attempt={...attempt,ticket:{resource_id:result.section.id}};known.current={scope:data.store.storageKey,attempt};
  }
  check();await data.store.write(attempt);check();await data.store.clearAccepted(attempt.key,attempt.ticket!.resource_id);check();known.current=null;
 }
 function startCreate(){
  if(!current()||flight.current)return;
  const data=read.data!,hasPending=data.attempt||known.current?.scope===data.store.storageKey;
  if(!allowed(!!hasPending))return;
  if(!hasPending){
   if(!Number.isSafeInteger(Number(data.state.revision))){setMessage(locale==='ru'?'Создание раздела недоступно: версия коллекции превышает безопасный числовой диапазон. Существующие запросы можно восстановить.':locale==='bg'?'Създаването на раздел не е достъпно: версията е извън безопасния числов диапазон. Запазените заявки могат да се възстановят.':'Section creation is unavailable: the collection revision exceeds the safe numeric range. Saved requests can still be recovered.');return;}
   try{organizationPosition(position);if(!name.trim()||Array.from(name.trim()).length>120)throw Error();}catch{setMessage(locale==='ru'?'Введите название до 120 символов и целую неотрицательную позицию.':locale==='bg'?'Въведете име до 120 знака и цяла неотрицателна позиция.':'Enter a name up to 120 characters and a whole nonnegative position.');return;}
  }
  void run(create,!!hasPending);
 }

 const f=flight.current,data=read.data,pending=data?.attempt??(data&&known.current?.scope===data.store.storageKey?known.current.attempt:null);
 if(read.phase!=='ready')return <View><Text>{read.phase==='loading'?__catalogT('ui_683c8416ed74'):__catalogT('ui_606d19bae26f')}</Text>{read.phase==='loading'?<Button onPress={read.cancel}>{__catalogT('ui_4e5a9d5ad638')}</Button>:<Button onPress={read.retry}>{__catalogT('ui_e82d36656335')}</Button>}</View>;
 const locked=busy||!props.canEdit||!['draft','published'].includes(data!.state.state),recoveryLocked=busy||!props.canEdit;
 return <View><Text>{__catalogT('ui_132c3c84d507')}</Text><Text>{__uiMessage(message)}</Text><Button disabled={busy} onPress={()=>{if(current())read.retry();}}>{__catalogT('ui_e82d36656335')}</Button>
 {busy&&<Button onPress={()=>stop(f)}>{__catalogT('ui_55e78f2ea4bf')}</Button>}
 <TextInput accessibilityLabel={__catalogT('ui_fd5037fdbc2a')} value={name} onChangeText={setName} editable={!locked&&!pending}/><TextInput accessibilityLabel={__catalogT('ui_e411e40696f3')} value={position} onChangeText={setPosition} editable={!locked&&!pending}/>
 <Button disabled={pending?recoveryLocked:locked} onPress={startCreate}>{pending?__catalogT('ui_0ed72b220424'):__catalogT('ui_5c007f2ce505')}</Button>
 {pending&&<Text>{__catalogT('ui_613bb30b3012')}</Text>}{pending&&!pending.ticket&&<Button disabled={recoveryLocked} onPress={()=>void run(abandon,true)}>{__catalogT('ui_e5f4b6c9e14a')}</Button>}
 <Text>{__catalogT('ui_b6bfa8896d24')} {sectionPage+1}</Text><Button disabled={busy||sectionPage===0} onPress={()=>setSectionPage(p=>Math.max(0,p-1))}>{__catalogT('ui_adbe7e9eb3e1')}</Button><Button disabled={busy||(sectionPage+1)*20>=data!.state.sections.length} onPress={()=>setSectionPage(p=>p+1)}>{__catalogT('ui_d996e1c3f102')}</Button>
 {data!.state.sections.slice(sectionPage*20,sectionPage*20+20).map(section=><SectionRow key={`${section.id}:${section.revision}`} section={section} disabled={locked||!!pending} save={(n,p)=>void run(signal=>api.update(section.id,data!.state.revision,n,p,signal).then(()=>{}))} remove={()=>void run(signal=>api.remove(section.id,data!.state.revision,signal))}/>)}
 <Text>{__catalogT('ui_6198788aac00')} {itemPage+1}</Text><Button disabled={busy||itemPage===0} onPress={()=>setItemPage(p=>Math.max(0,p-1))}>{__catalogT('ui_c297a67272fd')}</Button><Button disabled={busy||(itemPage+1)*20>=data!.state.items.length} onPress={()=>setItemPage(p=>p+1)}>{__catalogT('ui_ab3b8a3f6723')}</Button>
 {data!.state.items.slice(itemPage*20,itemPage*20+20).map((item,index)=><ItemRow key={`${item.id}:${data!.state.revision}`} item={item} index={itemPage*20+index} sections={data!.state.sections} disabled={locked||!!pending} save={(p,s)=>void run(signal=>api.move(item,data!.state.revision,p,s,signal).then(()=>{}))}/>)}
 </View>;
}
function SectionRow({section,disabled,save,remove}:{section:OrganizationSection;disabled:boolean;save:(name:string,position:number)=>void;remove:()=>void}){
 const __catalogT=useLocale().t;

 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;

 const {locale}=useLocale(),tr=(en:string,ru:string,bg:string)=>locale==='ru'?ru:locale==='bg'?bg:en;
 const [name,setName]=useState(section.name),[position,setPosition]=useState(String(section.position)),[confirm,setConfirm]=useState(false),[error,setError]=useState('');
 return <View><TextInput accessibilityLabel={__uiT('ui_8cbaa55a52ef', {p0:(__catalogT('ui_7c0faddce9a7')),p1:(section.name)})} value={name} onChangeText={setName} editable={!disabled}/><TextInput accessibilityLabel={__uiT('ui_8cbaa55a52ef', {p0:(__catalogT('ui_c53edeb1ae53')),p1:(section.name)})} value={position} onChangeText={setPosition} editable={!disabled}/><Text>{__uiMessage(error,true)}</Text><Button disabled={disabled} onPress={()=>{try{const n=name.trim();if(!n||Array.from(n).length>120)throw Error();save(n,organizationPosition(position));}catch{setError("Enter a name and a whole nonnegative position.");}}}>{__catalogT('ui_5087cbb639b2')}</Button><Button disabled={disabled} onPress={()=>setConfirm(true)}>{__catalogT('ui_5bf23284519a')}</Button>{confirm&&<><Text>{__catalogT('ui_fdca45631f26')}</Text><Button disabled={disabled} onPress={remove}>{__catalogT('ui_bf05bca3b472')}</Button><Button onPress={()=>setConfirm(false)}>{__catalogT('ui_5b156b90fcc2')}</Button></>}</View>;
}
function ItemRow({item,index,sections,disabled,save}:{item:OrganizationItem;index:number;sections:OrganizationSection[];disabled:boolean;save:(position:number,section:string|null)=>void}){
 const __catalogT=useLocale().t;

 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;

 const {locale}=useLocale(),tr=(en:string,ru:string,bg:string)=>locale==='ru'?ru:locale==='bg'?bg:en;
 const [position,setPosition]=useState(String(item.position)),[section,setSection]=useState(item.section_id??null),[error,setError]=useState(''),[search,setSearch]=useState(''),[page,setPage]=useState(0);
 const choices=sections.filter(s=>s.name.toLocaleLowerCase().includes(search.toLocaleLowerCase()));
 return <View><Text>{__catalogT('ui_d84eebada93e')} {index+1} · {item.id}</Text><TextInput accessibilityLabel={__uiT('ui_8cbaa55a52ef', {p0:(__catalogT('ui_21c501aeca5c')),p1:(item.id)})} value={position} onChangeText={setPosition} editable={!disabled}/><Button disabled={disabled} onPress={()=>setSection(null)}>{section===null?'✓ ':''}{__catalogT('ui_c02b9689b3e1')}</Button><Text>{__catalogT('ui_21ef23746750')} {sections.find(s=>s.id===section)?.name??__catalogT('ui_dc937b598926')}</Text><TextInput accessibilityLabel={__uiT('ui_8cbaa55a52ef', {p0:(__catalogT('ui_a6130ba9817f')),p1:(item.id)})} value={search} onChangeText={v=>{setSearch(v);setPage(0);}} editable={!disabled}/><Button disabled={disabled||page===0} onPress={()=>setPage(p=>Math.max(0,p-1))}>{__catalogT('ui_a0ed3c23babf')}</Button><Button disabled={disabled||(page+1)*10>=choices.length} onPress={()=>setPage(p=>p+1)}>{__catalogT('ui_011932a3ef8f')}</Button>{choices.slice(page*10,page*10+10).map(s=><Button key={s.id} disabled={disabled} onPress={()=>setSection(s.id)}>{section===s.id?'✓ ':''}{s.name}</Button>)}<Text>{__uiMessage(error,true)}</Text><Button disabled={disabled} onPress={()=>{try{save(organizationPosition(position),section);}catch{setError("Enter a whole nonnegative position.");}}}>{__catalogT('ui_a32d9db56468')}</Button></View>;
}
