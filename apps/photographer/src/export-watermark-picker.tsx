import {Button,Text,TouchableRipple} from 'react-native-paper';
import {readDeadline} from './read-deadline';
import {useLocale} from './locale-context';
import {APIError} from '@photoeditor/api-client';
import {useEffect,useRef,useState} from 'react';
import {View} from 'react-native';
import type {OwnerAPI} from './owner-api';
export type WatermarkAsset={id:string;upload_state:string;metadata:Record<string,unknown>};
const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
export function ExportWatermarkPicker({api,value,onChange,disabled}:{api:OwnerAPI;value?:string;onChange:(id:string|undefined)=>void;disabled:boolean}){
 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;

 const {t}=useLocale();
 const [items,setItems]=useState<WatermarkAsset[]>([]),[cursor,setCursor]=useState<string|null>(null),[busy,setBusy]=useState(false),[notice,setNotice]=useState('');const flight=useRef<AbortController|null>(null),seenCursors=useRef(new Set<string>()),inventory=useRef<WatermarkAsset[]>([]);
 async function load(next?:string){if(flight.current)return;const controller=new AbortController();flight.current=controller;setBusy(true);try{const page=await readDeadline(controller.signal,signal=>api.watermarkAssets(next,signal));controller.signal.throwIfAborted();if(!page||!Array.isArray(page.items)||page.items.length>100||page.items.some(item=>!item||!uuid.test(item.id)||item.id==='00000000-0000-0000-0000-000000000000'||item.upload_state!=='verified')||page.next_cursor!==null&&page.next_cursor!==undefined&&(typeof page.next_cursor!=='string'||!page.next_cursor||page.next_cursor.length>4096||page.items.length===0||seenCursors.current.has(page.next_cursor)||page.next_cursor===next))throw new Error('WATERMARK_ASSET_PAGE_INVALID');const ids=new Set(next?inventory.current.map(item=>item.id):[]);for(const item of page.items){if(ids.has(item.id))throw new Error('WATERMARK_ASSET_PAGE_INVALID');ids.add(item.id)}inventory.current=next?[...inventory.current,...page.items]:page.items;setItems(inventory.current);if(next)seenCursors.current.add(next);setCursor(page.next_cursor??null);setNotice('')}catch(error){if(!controller.signal.aborted){if(error instanceof APIError&&[401,403,404,410].includes(error.status)){inventory.current=[];setItems([]);setCursor(null)}setNotice('Unable to load watermark images')}}finally{if(flight.current===controller){flight.current=null;setBusy(false)}}}
 useEffect(()=>{inventory.current=[];setItems([]);setCursor(null);seenCursors.current.clear();void load();return()=>{flight.current?.abort();flight.current=null}},[api]);
 return <View>{busy?<Button accessibilityRole="button" onPress={()=>{flight.current?.abort();flight.current=null;setBusy(false);setNotice('Unable to load watermark images')}}>{t('read_cancel')}</Button>:null}<Text>{__uiT('ui_0b797b98f2b3')}</Text><Text>{__uiT('ui_6d56097605a8')}</Text><Button disabled={disabled} onPress={()=>onChange(undefined)}>{!value?'◉':'○'} {__uiT('ui_e7f03e13d9cb')}</Button>{items.map(item=><TouchableRipple key={item.id} accessibilityRole="radio" accessibilityLabel={__uiT('ui_1324355d08d2', {p0:(String(item.metadata?.filename??item.id))})} accessibilityState={{checked:item.id===value,disabled}} aria-checked={item.id===value} disabled={disabled} onPress={()=>onChange(item.id)}><Text>{item.id===value?'◉':'○'} {String(item.metadata?.filename??item.id)}</Text></TouchableRipple>)}{cursor?<Button disabled={busy||disabled} onPress={()=>void load(cursor)}>{__uiT('ui_425ce2fdb913')}</Button>:null}{notice?<View><Text accessibilityRole="alert">{__uiMessage(notice)}</Text><Button disabled={busy||disabled} onPress={()=>void load(cursor??undefined)}>{__uiT('ui_d9a954da5d4b')}</Button></View>:null}</View>
}
