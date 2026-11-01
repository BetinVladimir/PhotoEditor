import {useLocale} from '../../src/locale-context';
import {Button, Dialog, Text} from 'react-native-paper';
import {View} from 'react-native';
import {catalogStyle as s} from '../../src/catalog-style';

// UI-09: Batch gate dialog — shown when a batch operation is rejected due to quota deficit.
// No own route (R24: quota deficit known before task creation → reject whole batch before tasks exist).
// Props carry quota deficit info from the parent screen.
export type BatchGateProps = {
  visible: boolean;
  available: string;
  required: string;
  onProceed: () => void;
  onDismiss: () => void;
};

export default function BillingBatchGate({visible, available, required, onProceed, onDismiss}: BatchGateProps) {
  const {t} = useLocale();

  return (
    <Dialog visible={visible} onDismiss={onDismiss}>
      <Dialog.Title>{t('billing_batch_gate_title')}</Dialog.Title>
      <Dialog.Content>
        <Text>{t('billing_batch_gate_message')}</Text>
        <View style={{marginTop: 8}}>
          <Text style={s.meta}>{t('billing_batch_gate_available')}: {available}</Text>
          <Text style={s.meta}>{t('billing_batch_gate_required')}: {required}</Text>
        </View>
      </Dialog.Content>
      <Dialog.Actions>
        <Button onPress={onDismiss}>{t('billing_batch_gate_dismiss')}</Button>
        <Button mode="contained" onPress={onProceed}>{t('billing_batch_gate_proceed')}</Button>
      </Dialog.Actions>
    </Dialog>
  );
}
