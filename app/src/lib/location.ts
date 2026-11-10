import * as Location from 'expo-location';
import { Fix, fixFromRaw, LocationError } from './fix';

/** Foreground permission at the point of use, then one high-accuracy fix with a timeout. */
export async function getFix(timeoutMs = 15_000): Promise<Fix> {
  let perm = await Location.getForegroundPermissionsAsync();
  if (!perm.granted) perm = await Location.requestForegroundPermissionsAsync();
  if (!perm.granted) throw new LocationError(perm.canAskAgain ? 'permission_denied' : 'permission_blocked');

  if (!(await Location.hasServicesEnabledAsync())) throw new LocationError('services_off');

  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    const raw = await Promise.race([
      Location.getCurrentPositionAsync({ accuracy: Location.Accuracy.High }),
      new Promise<never>((_, rej) => { timer = setTimeout(() => rej(new LocationError('timeout')), timeoutMs); }),
    ]);
    return fixFromRaw(raw as Location.LocationObject);
  } catch (e) {
    throw e instanceof LocationError ? e : new LocationError('unavailable');
  } finally {
    if (timer) clearTimeout(timer);
  }
}
