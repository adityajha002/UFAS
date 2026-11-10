import React, { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, StatusBar, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import EnrollmentScreen from '../screens/EnrollmentScreen';
import HomeScreen from '../screens/HomeScreen';
import native from '../native/attendanceSecurity';
import { clearAll, loadEnrollment } from '../lib/storage';
import { colors } from '../theme/colors';
import { MAX_CONTENT_WIDTH } from '../lib/scale';

type Who = { facultyId: string; name?: string | null };
type Boot = 'loading' | 'enroll' | 'ready';

export default function App() {
  const [boot, setBoot] = useState<Boot>('loading');
  const [who, setWho] = useState<Who | null>(null);

  const check = useCallback(async () => {
    try {
      const [enrolment, hasKey] = await Promise.all([loadEnrollment(), native.hasKey()]);
      if (enrolment && hasKey) {
        setWho(enrolment);
        setBoot('ready');
        return;
      }
      if (enrolment && !hasKey) {
        await clearAll();
      }
      setBoot('enroll');
    } catch {
      setBoot('enroll');
    }
  }, []);

  const reenrol = useCallback(async () => {
    await native.deleteKey().catch(() => {});
    await clearAll().catch(() => {});
    setWho(null);
    setBoot('enroll');
  }, []);

  useEffect(() => {
    check();
  }, [check]);

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.cream }} edges={['top', 'bottom']}>
      <StatusBar barStyle="light-content" backgroundColor="#0B0044" />
      {/* Phone-width column: full width on phones, centred on tablets and in a browser. */}
      <View style={{ flex: 1, width: '100%', maxWidth: MAX_CONTENT_WIDTH, alignSelf: 'center' }}>
        {boot === 'loading' && (
          <View style={{ flex: 1, backgroundColor: colors.cream, justifyContent: 'center' }}>
            <ActivityIndicator color={colors.navyHeader} />
          </View>
        )}
        {boot === 'enroll' && (
          <EnrollmentScreen
            onEnrolled={(w) => {
              setWho(w);
              setBoot('ready');
            }}
          />
        )}
        {boot === 'ready' && who && (
          <HomeScreen facultyId={who.facultyId} name={who.name} onNeedsEnrol={reenrol} />
        )}
      </View>
    </SafeAreaView>
  );
}
