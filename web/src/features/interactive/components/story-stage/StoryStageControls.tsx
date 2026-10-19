import { SlidersHorizontal } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { StoryPicker, type StoryPickerProps } from '../StoryPicker'
import { TurnNavigator, type TurnNavigatorProps } from '../TurnNavigator'

interface StoryStageControlsProps {
  isMobile: boolean
  picker: StoryPickerProps
  history: TurnNavigatorProps
  directorPanelVisible?: boolean
  onToggleDirectorPanel?: () => void
}

export function StoryStageControls({ isMobile, picker, history, directorPanelVisible, onToggleDirectorPanel }: StoryStageControlsProps) {
  const { t } = useTranslation()
  return (
    <>
      {isMobile ? (
        <TurnNavigator {...history} renderTrigger={(openHistory) => <StoryPicker {...picker} onOpenHistory={history.items.length ? openHistory : undefined} />} />
      ) : <StoryPicker {...picker} variant="compact" />}
      {isMobile && onToggleDirectorPanel && (
        <Button type="button" variant="ghost" size="icon" onClick={onToggleDirectorPanel} aria-label={directorPanelVisible ? t('storyStage.hideDirectorPanel') : t('storyStage.showDirectorPanel')} aria-expanded={directorPanelVisible} title={t('storyStage.directorPanel')}>
          <SlidersHorizontal />
        </Button>
      )}
    </>
  )
}
