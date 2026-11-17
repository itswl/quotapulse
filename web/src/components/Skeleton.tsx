import { cx } from './cx';

export interface SkeletonProps {
  /** Any CSS length. Defaults to the container's width. */
  width?: string;
  height?: string;
  class?: string;
}

/** A grey block standing in for content that is on its way. Use it for the
 *  first load only: a refresh keeps what is on screen. */
export function Skeleton({ width, height = '1em', class: extra }: SkeletonProps) {
  return <span class={cx('skeleton', extra)} style={{ width, height }} aria-hidden="true" />;
}
