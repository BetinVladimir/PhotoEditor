import {Pressable,Text,View} from 'react-native';
import type {EditorDraft} from './catalog-types';
import {catalogStyle as s} from './catalog-style';
import {useLocale} from './locale-context';
export function EditorDraftConflict({localRecipe,localMasks,server,busy,onResolve}:{localRecipe:string;localMasks:unknown[];server:EditorDraft;busy:boolean;onResolve:(choice:'local'|'server')=>void}){
 const {t}=useLocale();
 return <View style={{gap:8}}>
  <Text accessibilityRole="alert" style={s.name}>{t('editor_conflict_title')}</Text>
  <Text style={s.subtitle}>{t('editor_conflict_help')}</Text>
  <Text style={s.name}>{t('editor_conflict_local')}</Text>
  <Text selectable style={s.meta}>{localRecipe}</Text>
  <Text selectable style={s.meta}>{JSON.stringify(localMasks)}</Text>
  <Text style={s.name}>{t('editor_conflict_server')} · {server.revision}</Text>
  <Text selectable style={s.meta}>{JSON.stringify({params:server.params,mask_refs:server.mask_refs},null,2)}</Text>
  <Pressable accessibilityRole="button" disabled={busy} onPress={()=>onResolve('local')} style={s.button}><Text style={s.buttonText}>{t('editor_conflict_keep_local')}</Text></Pressable>
  <Pressable accessibilityRole="button" disabled={busy} onPress={()=>onResolve('server')} style={s.button}><Text style={s.buttonText}>{t('editor_conflict_use_server')}</Text></Pressable>
 </View>;
}
