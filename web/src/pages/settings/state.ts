// Server data only the Settings pages use. The settings themselves and the
// provider definitions are shared with other pages (state/settings.ts,
// state/providers.ts).
import { getSystem, listChannelTypes, listChannels, listTokens } from '../../api/endpoints';
import { resource } from '../../state/resource';

/** The channels, with how their recent deliveries went: a delivery or a
 *  channel that starts failing changes what the list says. */
export const channels = resource(listChannels, ['settings', 'activity', 'incidents']);

/** The kinds of channel and the fields each needs. They only change with
 *  the server's version. */
export const channelTypes = resource(listChannelTypes, []);

/** The API tokens. Nothing on the stream announces a change to them: the
 *  page reloads the list after its own writes. */
export const tokens = resource(listTokens, ['settings']);

/** Version, counts, and what each background loop last did. */
export const system = resource(getSystem, ['settings']);
