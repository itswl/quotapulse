// Small pieces the parts of the Spend page share.
import type { ComponentChildren } from 'preact';

/** Stands in for a parameter whose value is drawn by a component, not written
 *  as text: t('spend.sum.past', { amount: SLOT }). */
export const SLOT = '\u0000';

/** Writes a sentence with a node where its SLOT is, so that an amount inside
 *  a sentence is set like every other amount on the page. */
export function Fill({ text, children }: { text: string; children: ComponentChildren }) {
  const [before, after = ''] = text.split(SLOT);
  return (
    <>
      {before}
      {children}
      {after}
    </>
  );
}
