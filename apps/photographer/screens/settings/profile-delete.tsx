import {useLocale} from '../../src/locale-context';
import {Button, Card, HelperText, Text, TextInput} from 'react-native-paper';
import {useMemo, useState} from 'react';
import {useLocalSearchParams, useRouter} from '../../src/localized-router';
import {SafeAreaView, View} from 'react-native';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import {catalogStyle as s} from '../../src/catalog-style';

// UI-12: Profile delete screen.
// Criteria E: tenant from route params only for API call.
// User must type "DELETE" exactly (locale-invariant sentinel) to confirm — never inventing an alternate value (R7).
const CONFIRM_SENTINEL = 'DELETE';

export default function ProfileDelete() {
  const {t} = useLocale();
  const {tenant} = useLocalSearchParams<{tenant: string}>();
  const auth = useAuth();
  const api = useMemo(() => new OwnerAPI(auth.client(), tenant || ''), [auth, tenant]);
  const router = useRouter();

  const [confirmText, setConfirmText] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const confirmed = confirmText === CONFIRM_SENTINEL;

  const handleDelete = async () => {
    if (!confirmed || !tenant) return;
    setSubmitting(true);
    setError(null);
    try {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      await (api as any).profileDelete();
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      (router.replace as any)('/');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'error');
      setSubmitting(false);
    }
  };

  const handleCancel = () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (router.back as any)();
  };

  return (
    <SafeAreaView style={s.root}>
      <View style={s.header}>
        <Text variant="headlineMedium" style={s.title}>{t('profile_delete_title')}</Text>
        <HelperText type="error" visible>
          {t('profile_delete_warning')}
        </HelperText>
        <Card style={{marginBottom: 8}}>
          <Card.Title title={t('profile_delete_consequences')} />
          <Card.Content>
            <Text style={s.meta}>{t('profile_delete_photos')}</Text>
            <Text style={s.meta}>{t('profile_delete_account')}</Text>
          </Card.Content>
        </Card>
        <TextInput
          label={t('profile_delete_confirm_label')}
          placeholder={t('profile_delete_confirm_placeholder')}
          value={confirmText}
          onChangeText={setConfirmText}
          autoCapitalize="characters"
          style={{marginBottom: 8}}
        />
        {error && <HelperText type="error" visible>{error}</HelperText>}
        <View style={{flexDirection: 'row', gap: 8}}>
          <Button mode="outlined" onPress={handleCancel} disabled={submitting}>{t('profile_delete_cancel')}</Button>
          <Button
            mode="contained"
            onPress={handleDelete}
            disabled={!confirmed || submitting}
            loading={submitting}
          >
            {t('profile_delete_proceed')}
          </Button>
        </View>
      </View>
    </SafeAreaView>
  );
}
