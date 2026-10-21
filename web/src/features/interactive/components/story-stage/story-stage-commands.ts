import { searchInputCommands } from '@/components/Chat/input-command-search'

export interface StoryStageCommandItem {
  name: string
  description: string
  hint?: string
  builtIn: boolean
}

interface SkillCommand {
  name: string
  description?: string
}

interface CommandLabels {
  compactDescription: string
  compactHint: string
}

interface StoryStageCommandMenu {
  commands: StoryStageCommandItem[]
  builtInItems: Array<{ command: StoryStageCommandItem; index: number }>
  skillItems: Array<{ command: StoryStageCommandItem; index: number }>
}

export function buildStoryStageCommandMenu(query: string | null, skills: SkillCommand[], labels: CommandLabels): StoryStageCommandMenu {
  if (query === null) return { commands: [], builtInItems: [], skillItems: [] }
  const seen = new Set(['compact'])
  const builtins: StoryStageCommandItem[] = [
    {
      name: 'compact',
      description: labels.compactDescription,
      hint: labels.compactHint,
      builtIn: true,
    },
  ]
  const skillCommands: StoryStageCommandItem[] = []

  for (const skill of skills) {
    if (seen.has(skill.name)) continue
    seen.add(skill.name)
    skillCommands.push({
      name: skill.name,
      description: skill.description || skill.name,
      builtIn: false,
    })
  }

  const filtered = [builtins, skillCommands]
    .flatMap((group) => searchInputCommands(group, query, (command) => command.name))
  const indexed = filtered.map((command, index) => ({ command, index }))
  return {
    commands: filtered,
    builtInItems: indexed.filter(({ command }) => command.builtIn),
    skillItems: indexed.filter(({ command }) => !command.builtIn),
  }
}
