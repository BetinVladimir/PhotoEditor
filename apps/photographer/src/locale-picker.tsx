import {Pressable,Text,View} from 'react-native';
import {useLocale,type Locale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
const languages:ReadonlyArray<{locale:Locale;label:string}>=[{locale:'ru',label:'Русский'},{locale:'bg',label:'Български'},{locale:'en',label:'English'}];
export function LocalePicker(){
 const {locale,setLocale}=useLocale();
 return <View style={s.toolbar}>{languages.map(language=><Pressable key={language.locale} accessibilityRole="button" accessibilityLabel={language.label} accessibilityState={{selected:locale===language.locale}} style={s.button} onPress={()=>setLocale(language.locale)}><Text style={s.buttonText}>{locale===language.locale?'✓ ':''}{language.label}</Text></Pressable>)}</View>;
}
