import {Pressable,Text,View} from 'react-native';
import type {UploadState} from '@photoeditor/api-client';
import {useLocale} from './locale-context';
const statusKeys={queued:'import_queued',uploading:'import_uploading',paused:'import_paused',completing:'import_completing',complete:'import_complete',error:'import_error',aborted:'import_aborted'} as const;
export function UploadItemControls({state,busy,onPause,onAbort,onRefresh,restartRequired=false}:{state:UploadState;busy:boolean;onPause:()=>void;onAbort:()=>void;onRefresh?:()=>void;restartRequired?:boolean}){
 const {t}=useLocale();
 return <View><Text accessibilityLiveRegion="polite">{t(statusKeys[state])}</Text>
 {(state==='queued'||state==='uploading')?<Pressable accessibilityRole="button" disabled={busy} onPress={onPause}><Text>{t('import_pause')}</Text></Pressable>:null}
 {state!=='complete'&&state!=='aborted'&&state!=='completing'&&!restartRequired?<Pressable accessibilityRole="button" disabled={busy} onPress={onAbort}><Text>{t('import_abort')}</Text></Pressable>:null}
 {onRefresh&&(state==='paused'||state==='error'||state==='completing')?<Pressable accessibilityRole="button" disabled={busy} onPress={onRefresh}><Text>{t('import_refresh')}</Text></Pressable>:null}
 {state==='paused'||state==='error'?<Text>{t(restartRequired?'import_restart_hint':'import_resume_hint')}</Text>:null}
 </View>;
}
