import {Platform} from 'react-native';
import {Button, Text, TextInput} from "react-native-paper";
import {LocalePicker} from '../../src/locale-picker';
import {useCASMutation} from '../../src/use-cas-mutation';
import {useLocale} from '../../src/locale-context';
import {useEffect,useMemo,useState} from 'react';
import {SafeAreaView, View} from "react-native";
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import {useReadResource} from '../../src/use-read-resource';
import {ReadResourceState} from '../../src/read-resource-state';
import type {UserProfile} from '../../src/catalog-types';
import {catalogStyle as s} from '../../src/catalog-style';
export default function Profile(){
 const __uiMessage=useLocale().message;

 const auth=useAuth(),api=useMemo(()=>new OwnerAPI(auth.client(),''),[auth]);
 const resource=useReadResource(JSON.stringify([auth.origin,'profile']),api,signal=>api.me(signal));
 const value=resource.data,[timezone,setTimezone]=useState(''),[error,setError]=useState('');
 useEffect(()=>{setTimezone(value?.timezone??'');setError('');},[value]);
 const {t,locale,setLocale}=useLocale();
 useEffect(()=>{if(Platform.OS!=='web'&&value?.locale&&value.locale!==locale)setLocale(value.locale)},[value?.locale,locale,setLocale]);const mutation=useCASMutation(api,resource.current,resource.retry),busy=mutation.busy;
 async function save(patch:Partial<Pick<UserProfile,'locale'|'theme'|'timezone'>>){if(!value||busy)return;setError('');await mutation.run(signal=>api.updateMe(value,patch,signal),updated=>{resource.commit(updated);if(Platform.OS==='web'&&patch.locale&&updated.locale===patch.locale)setLocale(updated.locale);});}
 return <SafeAreaView style={s.root}><View style={s.header}><Text variant="headlineMedium" style={s.title}>{t('profile_title')}</Text><ReadResourceState {...resource}/>{value?<><LocalePicker disabled={busy} onChange={next=>void save({locale:next})}/><Text style={s.subtitle}>{value.email}</Text><TextInput mode="outlined" accessibilityLabel={t('profile_timezone')} editable={!busy} value={timezone} onChangeText={setTimezone} placeholder={t('profile_timezone')} placeholderTextColor="#888" style={s.input}/><View style={s.toolbar}>{(['light','dark','system'] as const).map(theme=><Button mode="outlined" key={theme} disabled={busy} onPress={()=>void save({theme})}>{value.theme===theme?'✓ ':''}{t(`profile_theme_${theme}`)}</Button>)}</View><Button mode="outlined" disabled={busy||!timezone} onPress={()=>void save({timezone})}>{t('profile_save_timezone')}</Button></>:null}{busy?<Button mode="outlined" accessibilityRole="button" onPress={mutation.cancel}>{t('read_cancel')}</Button>:null}{mutation.uncertain?<Text accessibilityRole="alert" style={s.error}>{t('settings_cas_uncertain')}</Text>:null}{error?<><Text accessibilityRole="alert" style={s.error}>{__uiMessage(error,true)}</Text><Button mode="outlined" disabled={busy} onPress={resource.retry}>{t('profile_reload')}</Button></>:null}</View></SafeAreaView>;
}
