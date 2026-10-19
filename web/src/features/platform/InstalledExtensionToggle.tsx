import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Field, FieldLabel } from '@/components/ui/field'
import { Switch } from '@/components/ui/switch'
import { localized, management, platformError, type CatalogEntry } from './api'

/** Installed enablement is global; Project settings still control where it is used. */
export function InstalledExtensionToggle({ item, onChanged, disabled }: {
  item: CatalogEntry
  onChanged: () => void | Promise<void>
  disabled?: boolean
}) {
  const { t, i18n } = useTranslation()
  const id = useId()
  const [busy, setBusy] = useState(false)
  const manifest = item.releases.find(release => release.ref.releaseId === item.currentRelease)?.manifest
  const name = localized(manifest?.name, i18n.language) || item.id
  return <Field orientation="horizontal" className="w-auto shrink-0 gap-2">
    <FieldLabel htmlFor={id} className="text-xs text-muted-foreground">{t(item.enabled ? 'platform.extensions.enabled' : 'platform.disabled')}</FieldLabel>
    <Switch id={id} checked={item.enabled} disabled={busy || disabled} aria-label={t('platform.enableNamed', { name })} onCheckedChange={enabled => {
      setBusy(true)
      void (async () => {
        try {
          await management(`/packages/${item.kind}/${encodeURIComponent(item.id)}`, 'PATCH', { enabled })
          await onChanged()
          if (!enabled) toast.info(t('platform.disabledStopped'))
        } catch (cause) {
          console.error('[extensions] changing installed enablement failed', { kind: item.kind, packageId: item.id, enabled, cause })
          toast.error(platformError(cause))
        } finally { setBusy(false) }
      })()
    }} />
  </Field>
}
