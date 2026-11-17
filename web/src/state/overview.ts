// The overview: the whole picture in one request, and the source of the
// server's clock and time zone for every other page.
import { getOverview } from '../api/endpoints';
import { setTimezone, syncClock } from './clock';
import { resource } from './resource';
import { muted } from './session';

export const overview = resource(getOverview, ['meters', 'reminders', 'incidents', 'settings'], (data) => {
  syncClock(data.now);
  setTimezone(data.timezone);
  muted.value = data.muted;
});
