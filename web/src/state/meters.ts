// Every meter, with its forecast. The overview carries the same views for
// its first screen; this is the full list the Meters page works from.
import { listMeters } from '../api/endpoints';
import { resource } from './resource';

export const meters = resource(listMeters, ['meters', 'incidents', 'settings']);
