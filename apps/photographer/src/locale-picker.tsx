import {RadioButton,Text,TouchableRipple} from 'react-native-paper';
import {View} from 'react-native';
import {useLocale,type Locale} from './locale-context';
const languages:ReadonlyArray<{locale:Locale;label:string}>=[{locale:'ru',label:'Русский'},{locale:'bg',label:'Български'},{locale:'en',label:'English'}];
export function LocalePicker({disabled=false,onChange}:{disabled?:boolean;onChange?:(locale:Locale)=>void}={}){
 const {locale,setLocale}=useLocale();
 const choose=(next:Locale)=>{if(!disabled)(onChange??setLocale)(next)};
 return <RadioButton.Group value={locale} onValueChange={next=>{if(languages.some(language=>language.locale===next))choose(next as Locale)}}><View style={{flexDirection:'row',flexWrap:'wrap'}}>{languages.map(language=><TouchableRipple key={language.locale} accessibilityRole="radio" accessibilityLabel={language.label} aria-checked={locale===language.locale} aria-disabled={disabled} accessibilityState={{checked:locale===language.locale,disabled}} disabled={disabled} onPress={()=>choose(language.locale)} style={{borderRadius:8}}><View style={{flexDirection:'row',alignItems:'center',minHeight:48,paddingRight:12}}><View accessibilityElementsHidden importantForAccessibility="no-hide-descendants" aria-hidden pointerEvents="none"><RadioButton value={language.locale} status={locale===language.locale?'checked':'unchecked'} disabled={disabled}/></View><Text>{language.label}</Text></View></TouchableRipple>)}</View></RadioButton.Group>;
}
