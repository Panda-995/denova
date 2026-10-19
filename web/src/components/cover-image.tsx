import { useLayoutEffect, useRef, useState, type ComponentProps } from 'react'
import { cn } from '@/lib/utils'

export interface ImageFocus { x: number; y: number }

// Center the source point where possible; clamp at image edges to avoid gaps.
export function coverPosition(width: number, height: number, imageWidth: number, imageHeight: number, focus: ImageFocus = { x: 0.5, y: 0.5 }): string {
  if (!width || !height || !imageWidth || !imageHeight) return '50% 50%'
  const scale = Math.max(width / imageWidth, height / imageHeight)
  const position = (viewport: number, image: number, point: number) => {
    const overflow = image * scale - viewport
    return overflow > 0 ? Math.max(0, Math.min(1, (point * image * scale - viewport / 2) / overflow)) * 100 : 50
  }
  return `${position(width, imageWidth, focus.x)}% ${position(height, imageHeight, focus.y)}%`
}

/** Responsive cover rendering with an optional focal point in source-image coordinates. */
export function CoverImage({ focus, className, style, onLoad, ...props }: ComponentProps<'img'> & { focus?: ImageFocus }) {
  const ref = useRef<HTMLImageElement>(null)
  const [position, setPosition] = useState('50% 50%')
  const measure = () => {
    const image = ref.current
    if (image) setPosition(coverPosition(image.clientWidth, image.clientHeight, image.naturalWidth, image.naturalHeight, focus))
  }
  useLayoutEffect(() => {
    measure()
    const observer = new ResizeObserver(measure)
    if (ref.current) observer.observe(ref.current)
    return () => observer.disconnect()
  }, [focus?.x, focus?.y, props.src])
  return <img {...props} ref={ref} className={cn('object-cover object-center', className)} style={{ ...style, objectPosition: position }} onLoad={event => { measure(); onLoad?.(event) }} />
}
