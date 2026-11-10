import React from 'react';
import { Image, Text, View } from 'react-native';
import logo from '../assets/logo.png';
import { colors } from '../theme/colors';
import { makeStyles } from '../lib/scale';

export function Header({ name }: { name?: string | null }) {
  const styles = useStyles();
  return (
    <View style={styles.wrap}>
      <View style={styles.bar}><Text style={styles.title}>USIC&T FACULTY</Text></View>
      <View style={styles.pill}>
        <Text style={styles.name} numberOfLines={1} ellipsizeMode="tail">{name?.trim() ? name : '-'}</Text>
      </View>
      <View style={styles.avatar} accessibilityLabel="Profile picture">
        <Image source={logo} style={styles.logo} resizeMode="cover" />
      </View>
    </View>
  );
}

const useStyles = makeStyles((s) => ({
  wrap: { height: s(280), backgroundColor: colors.cream },
  bar: { height: s(165), backgroundColor: colors.navyHeader, borderBottomWidth: s(8), borderBottomColor: colors.orange, justifyContent: 'center', paddingLeft: s(60) },
  title: { color: colors.white, fontSize: s(52), fontWeight: '900' },
  pill: { position: 'absolute', top: s(130), left: s(360), right: s(120), height: s(76), backgroundColor: colors.yellow, borderTopLeftRadius: s(38), borderBottomLeftRadius: s(38), justifyContent: 'center', paddingLeft: s(50), paddingRight: s(120) },
  name: { color: colors.navyHeader, fontSize: s(38), fontWeight: '800' },
  avatar: { position: 'absolute', top: s(52), right: 0, width: s(222), height: s(222), borderRadius: s(111), backgroundColor: colors.white, borderWidth: s(7), borderColor: colors.yellow, alignItems: 'center', justifyContent: 'center', overflow: 'hidden', padding: s(10) },
  logo: { width: '100%', height: '100%', backgroundColor: colors.white },
}));
