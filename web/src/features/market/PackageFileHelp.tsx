import { ExternalLink } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import type { Source } from './api'

/** Browser downloads use the same repository ref as the online import. */
export function PackageFileHelp({ source }: { source?: Source }) {
  const { t } = useTranslation()
  const url = source?.url
  const downloadURL = source?.kind === 'github' && url
    ? `${url.replace(/\/$/, '')}/archive/${encodeURIComponent(source.ref || 'HEAD')}.zip`
    : source?.kind === 'https_zip' ? url : undefined
  return <div className="flex min-w-0 flex-col gap-2 text-sm text-muted-foreground">
    <p>{t('market.import.fileHelp')}</p>
    {downloadURL && <>
      <Button asChild variant="outline" className="h-auto min-h-9 w-fit max-w-full whitespace-normal">
        <a href={downloadURL} target="_blank" rel="noreferrer"><ExternalLink data-icon="inline-start" />{t('market.import.downloadZip')}</a>
      </Button>
      {source?.path && source.path !== '.' && <p className="[overflow-wrap:anywhere]">{t('market.import.archivePath', { path: source.path })}</p>}
    </>}
  </div>
}
