import { useQuery } from '@tanstack/react-query'
import { management, type PluginPage } from './api'

/** Navigation reads the page catalog without loading plugin players or editors. */
export function usePluginPages() {
  return useQuery({
    queryKey: ['platform', 'plugin-pages'],
    queryFn: () => management<PluginPage[]>('/plugin-pages'),
    refetchInterval: 5000,
  })
}
