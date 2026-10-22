import {Button,Text} from 'react-native-paper';
import {View} from 'react-native';
import type {UploadState} from '@photoeditor/api-client';
import {useLocale} from './locale-context';
const statusKeys={queued:'import_queued',uploading:'import_uploading',paused:'import_paused',completing:'import_completing',complete:'import_complete',error:'import_error',aborted:'import_aborted'} as const;
export function UploadItemControls({state,busy,onPause,onAbort,onRefresh,restartRequired=false}:{state:UploadState;busy:boolean;onPause:()=>void;onAbort:()=>void;onRefresh?:()=>void;restartRequired?:boolean}){
 const {t}=useLocale();
 return <View><Text accessibilityLiveRegion="polite">{t(statusKeys[state])}</Text>
 {(state==='queued'||state==='uploading')?<Button accessibilityRole="button" disabled={busy} onPress={onPause}>{t('import_pause')}</Button>:null}
 {state!=='complete'&&state!=='aborted'&&state!=='completing'&&!restartRequired?<Button accessibilityRole="button" disabled={busy} onPress={onAbort}>{t('import_abort')}</Button>:null}
 {onRefresh&&(state==='paused'||state==='error'||state==='completing')?<Button accessibilityRole="button" disabled={busy} onPress={onRefresh}>{t('import_refresh')}</Button>:null}
 {state==='paused'||state==='error'?<Text>{t(restartRequired?'import_restart_hint':'import_resume_hint')}</Text>:null}
 </View>;
}
