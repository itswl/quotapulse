/** The mark: a pulse line through a disc. It draws in the text colour, with
 *  the line in the colour of whatever is behind it, so it works on any
 *  surface in either theme. */
export function Logo({ size = 24, class: extra }: { size?: number; class?: string }) {
  return (
    <svg class={extra} width={size} height={size} viewBox="0 0 24 24" aria-hidden="true">
      <circle cx="12" cy="12" r="10" fill="currentColor" />
      <path
        d="M0 12.2H8.1L10.25 7L13.75 17.5L15.9 12.2H24"
        fill="none"
        stroke="var(--logo-line, var(--bg))"
        stroke-width="2.6"
        stroke-linecap="round"
        stroke-linejoin="round"
      />
    </svg>
  );
}
