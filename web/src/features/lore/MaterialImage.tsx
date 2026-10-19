import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ImageOff } from 'lucide-react'
import { CoverImage, type ImageFocus } from '@/components/cover-image'

/** A failed remote image keeps its material accessible instead of showing a broken icon. */
export function MaterialImage({
  src,
  alt,
  className,
  focus,
}: {
  src: string
  alt: string
  className?: string
  focus?: ImageFocus
}) {
  const { t } = useTranslation()
  const [failed, setFailed] = useState(false)
  return failed ? (
    <span
      role="img"
      aria-label={t('lore.materials.imageFailed')}
      className="flex min-h-24 w-full flex-col items-center justify-center gap-2 p-3 text-xs text-muted-foreground"
    >
      <ImageOff className="size-5" />
      {t('lore.materials.imageFailed')}
    </span>
  ) : focus ? (
    <CoverImage src={src} alt={alt} focus={focus} className={className} loading="lazy" referrerPolicy="no-referrer" onError={() => setFailed(true)} />
  ) : (
    <img
      src={src}
      alt={alt}
      className={className}
      loading="lazy"
      referrerPolicy="no-referrer"
      onError={() => setFailed(true)}
    />
  )
}
