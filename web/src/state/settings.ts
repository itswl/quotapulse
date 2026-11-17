// The settings: the time zone, the defaults rules fall back on, and so on.
import { getSettings } from '../api/endpoints';
import { setTimezone } from './clock';
import { resource } from './resource';

export const settings = resource(getSettings, ['settings'], (data) => setTimezone(data.timezone));
