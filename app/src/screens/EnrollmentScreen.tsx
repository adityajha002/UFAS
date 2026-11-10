import React, { useCallback, useRef, useState } from 'react';
import { ActivityIndicator, Linking, Pressable, StyleSheet, Text, TextInput, View } from 'react-native';
import { CameraView, useCameraPermissions } from 'expo-camera';
import { colors } from '../theme/colors';
import { s } from '../lib/scale';
import native from '../native/attendanceSecurity';
import { api } from '../api/client';
import { clearAll, saveEnrollment } from '../lib/storage';
import { enroll, enrollMessage } from '../lib/enroll';

const deps = {
  generateKey: () => native.generateKey(),
  deleteKey: () => native.deleteKey(),
  register: (code: string, publicKey: string) => api().register(code, publicKey),
  saveEnrollment,
  clearAll,
};

export default function EnrollmentScreen({ onEnrolled }: { onEnrolled: (who: { facultyId: string; name?: string }) => void }) {
  const [permission, requestPermission] = useCameraPermissions();
  const [manual, setManual] = useState(false);
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const lock = useRef(false);

  const submit = useCallback(async (raw: string) => {
    if (lock.current) return;
    lock.current = true;
    setBusy(true);
    setError(null);
    try {
      onEnrolled(await enroll(raw, deps));
    } catch (e) {
      setError(enrollMessage(e));
    } finally {
      lock.current = false;
      setBusy(false);
    }
  }, [onEnrolled]);

  const cameraDenied = permission && !permission.granted;
  const resetDevice = useCallback(async () => {
    await native.deleteKey().catch(() => {});
    await clearAll().catch(() => {});
    setText('');
    setError(null);
  }, []);

  return (
    <View style={styles.page}>
      <Text style={styles.title}>Enrol this device</Text>
      <Text style={styles.sub}>
        {manual ? 'Type the 20-character enrollment code from the office.' : 'Scan the QR code given to you by the office.'}
      </Text>
      <Text style={styles.note}>Use the short office code, not the 32-character faculty ID.</Text>

      {!manual && (
        <View style={styles.cameraBox}>
          {!permission ? (
            <ActivityIndicator color={colors.yellow} />
          ) : cameraDenied ? (
            <View style={styles.center}>
              <Text style={styles.body}>Camera access is needed to scan the QR code.</Text>
              <Pressable
                style={styles.btn}
                onPress={() => (permission.canAskAgain ? requestPermission() : Linking.openSettings())}
              >
                <Text style={styles.btnText}>{permission.canAskAgain ? 'ALLOW CAMERA' : 'OPEN SETTINGS'}</Text>
              </Pressable>
            </View>
          ) : (
            <CameraView
              style={StyleSheet.absoluteFill}
              facing="back"
              barcodeScannerSettings={{ barcodeTypes: ['qr'] }}
              // Paused while enrolling or after a failure, so one bad code is not retried in a loop.
              onBarcodeScanned={busy || error ? undefined : ({ data }) => submit(data)}
            />
          )}
        </View>
      )}

      {manual && (
        <TextInput
          style={styles.input}
          value={text}
          onChangeText={(t) => { setText(t); setError(null); }}
          placeholder="e.g. 3fa91c07d2b84e6a15cd"
          placeholderTextColor={colors.buttonDisabledText}
          autoCapitalize="none"
          autoCorrect={false}
          editable={!busy}
          maxLength={40}
        />
      )}

      {busy && <ActivityIndicator style={styles.gap} color={colors.navyHeader} />}
      {error && <Text style={styles.error} accessibilityRole="alert">{error}</Text>}

      {manual && (
        <Pressable style={[styles.btn, (busy || !text.trim()) && styles.btnOff]} disabled={busy || !text.trim()} onPress={() => submit(text)}>
          <Text style={styles.btnText}>ENROL</Text>
        </Pressable>
      )}
      {!manual && error && !busy && (
        <Pressable style={styles.btn} onPress={() => setError(null)}>
          <Text style={styles.btnText}>SCAN AGAIN</Text>
        </Pressable>
      )}

      <Pressable style={styles.link} onPress={() => { setManual(!manual); setError(null); }} disabled={busy}>
        <Text style={styles.linkText}>{manual ? 'Scan a QR code instead' : 'Enter the code manually'}</Text>
      </Pressable>
      <Pressable style={styles.resetLink} onPress={resetDevice} disabled={busy}>
        <Text style={styles.resetText}>Reset device and clear old faculty</Text>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  page: { flex: 1, backgroundColor: colors.cream, padding: s(60), justifyContent: 'center' },
  title: { color: colors.navyHeader, fontSize: s(64), fontWeight: '900', marginBottom: s(20) },
  sub: { color: colors.textDark, fontSize: s(40), marginBottom: s(16) },
  note: { color: '#7A3E00', fontSize: s(28), marginBottom: s(32) },
  cameraBox: { height: s(800), borderRadius: s(30), overflow: 'hidden', backgroundColor: colors.navyCard, alignItems: 'center', justifyContent: 'center' },
  center: { padding: s(40), alignItems: 'center' },
  body: { color: colors.white, fontSize: s(38), textAlign: 'center', marginBottom: s(30) },
  input: { backgroundColor: colors.white, borderRadius: s(20), padding: s(30), fontSize: s(44), color: colors.textDark, marginBottom: s(30) },
  btn: { minHeight: 48, backgroundColor: colors.yellow, borderRadius: s(20), alignItems: 'center', justifyContent: 'center', paddingHorizontal: s(40), paddingVertical: s(24), marginTop: s(30) },
  btnOff: { backgroundColor: colors.chipGrey },
  btnText: { color: colors.navyHeader, fontSize: s(38), fontWeight: '800', letterSpacing: 1 },
  error: { color: '#B00020', fontSize: s(38), marginTop: s(30) },
  gap: { marginTop: s(30) },
  link: { minHeight: 48, alignItems: 'center', justifyContent: 'center', marginTop: s(30) },
  linkText: { color: colors.navyHeader, fontSize: s(38), textDecorationLine: 'underline' },
  resetLink: { minHeight: 48, alignItems: 'center', justifyContent: 'center', marginTop: s(18) },
  resetText: { color: '#912B2B', fontSize: s(30), textDecorationLine: 'underline' },
});
