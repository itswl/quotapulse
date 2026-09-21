/**
 * Icons used by the render modules.
 *
 * The drawings live once, as <symbol>s in the sprite at the top of index.html; this module
 * only emits references to them. Size, stroke width and color come from the CSS of the
 * surrounding control (`.icon` sets the defaults), so one drawing serves every context.
 * Icons are decorative: the surrounding button or heading carries the accessible name.
 */

const icon = (name: string, extraClass = ''): string =>
  `<svg class="icon${extraClass ? ` ${extraClass}` : ''}" aria-hidden="true" focusable="false"><use href="#i-${name}"></use></svg>`;

export const ICON_EDIT = icon('edit');
export const ICON_DELETE = icon('trash');
export const ICON_CHECK = icon('check');
export const ICON_UNDO = icon('undo');
export const ICON_ARROW_RIGHT = icon('arrow-right');
export const ICON_CLOSE = icon('close');
export const ICON_PLUS = icon('plus');
export const ICON_SEARCH = icon('search');
export const ICON_INFO_CIRCLE = icon('info');
export const ICON_CHECK_CIRCLE = icon('check-circle');
export const ICON_CLOUD_OFF = icon('error');
/* Subscriptions are recurring charges, so their empty state shows the renewal cycle. */
export const ICON_CALENDAR = icon('cycle');
export const ICON_MAIL = icon('at');
