import type { ReactNode } from 'react'
import { MobileWorkspaceHeader } from '@/components/layout/mobile-workspace-header'

interface StoryStageHeaderProps {
  isMobile: boolean
  controls: ReactNode
}

export function StoryStageHeader({ isMobile, controls }: StoryStageHeaderProps) {
  if (!isMobile) {
    return (
      <div className="nova-story-stage-header pointer-events-none absolute inset-x-0 top-2 z-[1] flex pl-3 pr-12">
        <div className="nova-story-stage-controls pointer-events-auto flex min-w-0 items-center gap-1 p-1">{controls}</div>
      </div>
    )
  }
  return (
    <MobileWorkspaceHeader route="interactive">
      {controls}
    </MobileWorkspaceHeader>
  )
}
