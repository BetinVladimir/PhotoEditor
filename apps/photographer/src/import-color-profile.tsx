import {View} from 'react-native';
import {TouchableRipple,Text} from 'react-native-paper';
import {readUploadAssumeProfile,type UploadAssumeProfile} from '@photoeditor/api-client';
import {useLocale} from './locale-context';
/** Missing-profile policy is opt-in; undefined preserves strict decoder admission. */
export function ImportColorProfile({value,disabled,onChange}:{value?:UploadAssumeProfile;disabled:boolean;onChange:(value:UploadAssumeProfile|undefined)=>void}){
 const {t}=useLocale();
 const choices=[['embedded','import_profile_embedded'],['srgb_v1','import_profile_srgb'],['linear_srgb_v1','import_profile_linear'],['work_rec2020_gamma24_v1','import_profile_rec2020']] as const;
 return <View style={{paddingVertical:12}}><Text variant="titleMedium" style={{fontSize:18}}>{t('import_profile_title')}</Text><Text style={{fontSize:16,lineHeight:24}}>{t('import_profile_hint')}</Text>
 {choices.map(([id,label])=>{const checked=(value??'embedded')===id;return <TouchableRipple key={id} accessibilityRole="radio" accessibilityLabel={t(label)} accessibilityState={{checked,disabled}} aria-checked={checked} disabled={disabled} onPress={()=>{if(!disabled)onChange(id==='embedded'?undefined:readUploadAssumeProfile(id))}} style={{minHeight:48,justifyContent:'center',paddingHorizontal:12}}><Text style={{fontSize:16,lineHeight:24}}>{checked?'● ':'○ '}{t(label)}</Text></TouchableRipple>})}</View>;
}
