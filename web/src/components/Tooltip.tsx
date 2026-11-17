import { Info } from 'lucide-preact';
import { cloneElement, type ComponentChildren, type VNode } from 'preact';
import { useEffect, useId, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { t } from '../i18n';

export interface TooltipProps {
  /** What the bubble says. Keep it to a sentence or two. */
  content: ComponentChildren;
  /** The one element the tooltip describes. It must be able to take focus —
   *  a button or a link — or keyboard and touch users never see the bubble. */
  children: VNode;
}

const MARGIN = 8;

/**
 * Explains its child on hover and on focus. The text is tied to the child
 * with aria-describedby, so a screen reader reads it without any hovering.
 * A tooltip adds to what is on screen; it must never be the only place a
 * fact can be found.
 */
export function Tooltip({ content, children }: TooltipProps) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [place, setPlace] = useState<{ left: number; top: number } | null>(null);
  const anchor = useRef<HTMLSpanElement>(null);
  const bubble = useRef<HTMLSpanElement>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const show = (delay: number) => {
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setOpen(true), delay);
  };
  const hide = () => {
    clearTimeout(timer.current);
    setOpen(false);
  };

  useLayoutEffect(() => {
    if (!open || !anchor.current || !bubble.current) return;
    const target = (anchor.current.firstElementChild ?? anchor.current).getBoundingClientRect();
    const size = bubble.current.getBoundingClientRect();
    const view = document.documentElement;
    let left = target.left + target.width / 2 - size.width / 2;
    left = Math.max(MARGIN, Math.min(left, view.clientWidth - size.width - MARGIN));
    let top = target.top - size.height - 6;
    if (top < MARGIN) top = target.bottom + 6;
    setPlace({ left: Math.round(left), top: Math.round(top) });
  }, [open, content]);

  useEffect(() => {
    if (!open) return;
    // The bubble is positioned once; anything that moves the page closes it.
    addEventListener('scroll', hide, true);
    addEventListener('resize', hide);
    return () => {
      removeEventListener('scroll', hide, true);
      removeEventListener('resize', hide);
    };
  }, [open]);

  useEffect(() => () => clearTimeout(timer.current), []);

  return (
    <span
      class="tip"
      ref={anchor}
      onPointerEnter={(event) => {
        if (event.pointerType !== 'touch') show(150);
      }}
      onPointerLeave={hide}
      onFocusCapture={() => show(0)}
      onBlurCapture={hide}
      onKeyDown={(event) => {
        if (event.key === 'Escape' && open) {
          event.stopPropagation();
          hide();
        }
      }}
    >
      {cloneElement(children, { 'aria-describedby': id })}
      <span
        class="tip__bubble"
        role="tooltip"
        id={id}
        ref={bubble}
        hidden={!open}
        style={place && open ? { left: `${place.left}px`, top: `${place.top}px` } : { left: '0px', top: '0px', visibility: 'hidden' }}
      >
        {content}
      </span>
    </span>
  );
}

/** A small "i" that explains the thing next to it. `label` names what is
 *  being explained, for a screen reader: "About expected daily burn". */
export function InfoTip({ children, label }: { children: ComponentChildren; label?: string }) {
  return (
    <Tooltip content={children}>
      <button type="button" class="info-tip" aria-label={label ?? t('common.action.info')}>
        <Info size={14} />
      </button>
    </Tooltip>
  );
}
