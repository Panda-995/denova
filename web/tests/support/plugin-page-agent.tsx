import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { ThemeProvider } from 'next-themes'
import { TooltipProvider } from '@/components/ui/tooltip'
import { AgentApprovalProvider } from '@/features/agent-approval/AgentApprovalProvider'
import { PluginPageWorkspace } from '@/features/platform/PluginPageWorkspace'
import { getProjectLoreItems } from '@/lib/api'
import '@/i18n'
import '@/index.css'

const projectId = new URLSearchParams(location.search).get('projectId')!
const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })

function Fixture() {
  const lore = useQuery({ queryKey: ['lore-items', projectId], queryFn: () => getProjectLoreItems(projectId) })
  if (!lore.data) return null
  return <PluginPageWorkspace projectId={projectId} route="plugin:index.comic-studio/studio" visible
    loreReferenceLabels={Object.fromEntries(lore.data.map(item => [item.id, item.name]))}
    loreSuggestions={lore.data.map(item => ({ value: item.id, label: item.name }))}
    onChooseProject={() => {}} />
}

createRoot(document.getElementById('root')!).render(
  <QueryClientProvider client={client}><ThemeProvider attribute="data-theme" defaultTheme="dark" enableSystem={false} themes={['light', 'dark']}>
    <TooltipProvider><AgentApprovalProvider><Fixture /></AgentApprovalProvider></TooltipProvider>
  </ThemeProvider></QueryClientProvider>,
)
