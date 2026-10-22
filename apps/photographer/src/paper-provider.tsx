import {type ReactNode} from 'react';
import {PaperProvider, Surface} from 'react-native-paper';
import {SafeAreaProvider} from 'react-native-safe-area-context';
import MaterialCommunityIcons from '@expo/vector-icons/MaterialCommunityIcons';
import {photographerDarkTheme} from './paper-theme';

export function PhotographerPaperProvider({children}:{children:ReactNode}) {
 return <SafeAreaProvider><PaperProvider
  theme={photographerDarkTheme}
  settings={{icon:props=><MaterialCommunityIcons {...props}/>}}>
  <Surface elevation={0} style={{flex:1,backgroundColor:photographerDarkTheme.colors.background}}>{children}</Surface>
 </PaperProvider></SafeAreaProvider>;
}
