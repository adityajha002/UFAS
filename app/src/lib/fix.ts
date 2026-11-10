// Pure location helpers (no expo imports) so they can be unit-tested.

export type Fix = { latMicro: number; lonMicro: number; accuracyCm: number };

export type LocationFailure =
  | 'permission_denied' | 'permission_blocked' | 'services_off'
  | 'timeout' | 'mocked' | 'unavailable';

export class LocationError extends Error {
  constructor(public code: LocationFailure) { super(code); this.name = 'LocationError'; }
}

export type RawPosition = {
  coords: { latitude: number; longitude: number; accuracy: number | null };
  mocked?: boolean; // Android only
};

/** Server units: micro-degrees and centimetres. Refuses mock-provider fixes. */
export function fixFromRaw(raw: RawPosition): Fix {
  if (raw.mocked === true) throw new LocationError('mocked');
  const { latitude, longitude, accuracy } = raw.coords;
  if (![latitude, longitude, accuracy].every((n) => typeof n === 'number' && Number.isFinite(n))) {
    throw new LocationError('unavailable');
  }
  return {
    latMicro: Math.round(latitude * 1e6),
    lonMicro: Math.round(longitude * 1e6),
    accuracyCm: Math.round((accuracy as number) * 100),
  };
}

export function locationMessage(code: LocationFailure): string {
  switch (code) {
    case 'permission_denied': return 'Location permission is needed to verify you are on campus. Please allow it and try again.';
    case 'permission_blocked': return 'Location permission is turned off for this app. Enable it in Settings > Apps > Permissions, then try again.';
    case 'services_off': return 'Location services are off. Turn on Location in your phone settings and try again.';
    case 'timeout': return 'Could not get your location in time. Move to an open area and try again.';
    case 'mocked': return 'A mock or fake location was detected. Turn it off and try again.';
    case 'unavailable': return 'Could not read your location. Try again.';
  }
}
