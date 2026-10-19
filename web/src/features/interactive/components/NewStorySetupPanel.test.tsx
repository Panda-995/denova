import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { ConversationConfigController, ConversationConfigSnapshot } from '@/features/conversation-config/types'
import { getProjectLoreItems, type LoreItem } from '@/lib/api'
import type { GamePlanningTemplate, StorySummary, Teller } from '../types'
import { NewStorySetupPanel } from './NewStorySetupPanel'

vi.mock('@/lib/api', async (original) => ({ ...await original<typeof import('@/lib/api')>(), getProjectLoreItems: vi.fn() }))

const settingsMocks = vi.hoisted(() => ({
  fetchSettings: vi.fn(),
  fetchProjectSettings: vi.fn().mockResolvedValue({ effective: {} }),
}))

vi.mock('@/features/settings/api', () => ({
  fetchSettings: settingsMocks.fetchSettings,
  fetchProjectSettings: settingsMocks.fetchProjectSettings,
  refreshProjectSettings: settingsMocks.fetchProjectSettings,
}))

vi.mock('../api', () => ({
  getActorStates: vi.fn().mockResolvedValue([{ id: 'state-basic', name: '基础状态' }]),
  getEventPackages: vi.fn().mockResolvedValue([]),
  getRuleSystems: vi.fn().mockResolvedValue([{ id: 'd20', name: 'D20' }]),
}))

vi.mock('@/features/agents/CustomAgentSelect', () => ({
  CustomAgentSelect: () => <div data-testid="custom-agent-select" />,
}))

const planningTemplate: GamePlanningTemplate = {
  version: 1,
  id: 'default',
  name: '冒险',
  description: '',
  sections: [{ id: 'direction', title: '方向', description: '规划方向。' }],
  custom: false,
}

const teller: Teller = {
  version: 1,
  id: 'cinematic',
  name: '电影化',
  description: '',
  context_policy: {} as Teller['context_policy'],
  slots: [],
  custom: false,
}

const loreCharacter: LoreItem = {
  id: 'hero',
  enabled: true,
  type: 'character',
  type_source: 'manual',
  name: '林川',
  importance: 'major',
  load_mode: 'auto',
  tags: ['航海士', '主角'],
  brief_description: '失忆的领航员',
  keywords: [],
  content: '林川熟悉旧港的每一条水道。',
  created_at: '2026-08-30T00:00:00Z',
  updated_at: '2026-08-31T00:00:00Z',
}

const alternateCharacter: LoreItem = {
  ...loreCharacter,
  id: 'companion',
  name: '顾岚',
  tags: ['同伴'],
  brief_description: '冷静的机关师',
  content: '顾岚擅长破解遗迹机关。',
}

function conversationConfigController(): ConversationConfigController {
  const snapshot: ConversationConfigSnapshot = {
    agent_kind: 'interactive_story',
    profile_id: 'default',
    thinking_level: 'medium',
    approval_mode: 'write',
    revision: 0,
  }
  return {
    snapshot,
    initialized: true,
    loading: false,
    saving: false,
    error: null,
    patch: vi.fn().mockResolvedValue(true),
    reload: vi.fn().mockResolvedValue(snapshot),
  }
}

describe('NewStorySetupPanel', () => {
  beforeEach(() => {
    settingsMocks.fetchProjectSettings.mockResolvedValue({ workspace: {}, effective: {} })
    settingsMocks.fetchSettings.mockResolvedValue({
      effective: {
        openai_model: 'test-model',
        model_profiles: [
          { id: 'default', name: '默认模型', model: 'test-model' },
          { id: 'creative', name: '创意模型', model: 'creative-model' },
        ],
      },
    })
  })

  it('submits a Lore protagonist snapshot source and opening from one start flow', async () => {
    const user = userEvent.setup()
    const onCreate = vi.fn().mockResolvedValue(undefined)
    const controller = conversationConfigController()
    render(
      <NewStorySetupPanel
        projectId="project-1"
        tellers={[teller]}
        planningTemplates={[planningTemplate]}
        imagePresets={[{ version: 1, id: 'game-cg', name: 'Game CG', description: '', custom: false }]}
        loreItems={[loreCharacter, alternateCharacter]}
        bookOpeningPresets={[{ id: 'harbor', title: '雾港来信', content: '港口的灯逐盏熄灭。' }]}
        conversationConfig={controller}
        onCancel={vi.fn()}
        onCreate={onCreate}
      />,
    )

    expect(screen.queryByLabelText('故事线名称')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('故事简介（可选）')).not.toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '自定义' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByText('林川')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '更换' }))
    expect(await screen.findByRole('option', { name: /林川/ })).toBeInTheDocument()
    await user.click(screen.getByRole('option', { name: /顾岚/ }))
    await user.click(screen.getByRole('tab', { name: /书籍预设/ }))
    await user.click(await screen.findByRole('option', { name: /雾港来信/ }))
    const modelSelect = screen.getByRole('combobox', { name: '模型配置' })
    await waitFor(() => expect(modelSelect).toBeEnabled())
    await user.click(modelSelect)
    await user.click(await screen.findByRole('option', { name: /创意模型/ }))
    await waitFor(() => expect(controller.patch).toHaveBeenCalledWith({ profile_id: 'creative' }))
    await user.click(screen.getByRole('combobox', { name: '思考强度' }))
    await user.click(await screen.findByRole('option', { name: '高' }))
    await waitFor(() => expect(controller.patch).toHaveBeenCalledWith({ thinking_level: 'high' }))
    expect(onCreate).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: '开始故事' }))

    await waitFor(() => expect(onCreate).toHaveBeenCalledTimes(1))
    expect(onCreate.mock.calls[0]?.[0]).toMatchObject({
      title: '',
      origin: '',
      profile_id: 'creative',
      thinking_level: 'high',
      protagonist: { mode: 'lore', source_lore_item_id: 'companion' },
      planning_template_id: planningTemplate.id,
      opening: { mode: 'preset', preset_id: 'harbor', preset_text: '港口的灯逐盏熄灭。' },
      check_settings: { difficulty_shift: 0, roll_modifier: 0, rule_state_consumption_mode: 'hybrid_auto', rule_visibility_mode: 'audit_only' },
      image_settings: { mode: 'manual', interval_turns: 3, preset_id: 'game-cg' },
    })
  })

  it('selects an opening background and submits independent presentation switches', async () => {
    const user = userEvent.setup()
    const onCreate = vi.fn().mockResolvedValue(undefined)
    const location: LoreItem = {
      ...loreCharacter, id: 'station', type: 'location', name: '车站', tags: [],
      resolved_materials: [{ id: 'day', path: 'assets/day.png', name: '白昼', original_name: 'day.png', mime_type: 'image/png', size_bytes: 10, source: { kind: 'uploaded' } }],
    }
    vi.mocked(getProjectLoreItems).mockResolvedValue([loreCharacter, location])
    // The parent still holds the catalog from before the material import.
    render(<NewStorySetupPanel projectId="project-1" tellers={[teller]} planningTemplates={[planningTemplate]} imagePresets={[]} loreItems={[loreCharacter]} conversationConfig={conversationConfigController()} onCancel={vi.fn()} onCreate={onCreate} />)
    await user.click(screen.getByRole('button', { name: '当前背景' }))
    await user.click(await screen.findByRole('button', { name: /白昼.*车站/ }))
    expect(getProjectLoreItems).toHaveBeenCalledWith('project-1')
    expect(screen.getByRole('img', { name: '白昼' })).toBeInTheDocument()
    await user.click(screen.getByRole('switch', { name: '动态背景' }))
    await user.click(screen.getByRole('switch', { name: '角色差分' }))
    await user.click(screen.getByRole('button', { name: '开始故事' }))
    await waitFor(() => expect(onCreate).toHaveBeenCalledTimes(1))
    expect(onCreate.mock.calls[0][0].presentation_settings).toEqual({
      background: false, characters: false, ambient: false,
      default_background: { item_id: 'station', asset_id: 'day', path: 'assets/day.png', name: '白昼' },
    })
  })

  it('keeps the Lore choice active and treats an empty optional custom opening as AI-generated', async () => {
    const user = userEvent.setup()
    const onCreate = vi.fn().mockResolvedValue(undefined)
    render(
      <NewStorySetupPanel
        projectId="project-1"
        tellers={[teller]}
        planningTemplates={[planningTemplate]}
        imagePresets={[]}
        loreItems={[alternateCharacter]}
        conversationConfig={conversationConfigController()}
        onCancel={vi.fn()}
        onCreate={onCreate}
      />,
    )

    expect(screen.getByRole('radio', { name: '从资料库选择' })).toBeChecked()
    expect(screen.getByText(/Game Agent.*自动识别/)).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '自定义' })).toHaveAttribute('aria-selected', 'true')
    await user.click(await screen.findByRole('button', { name: '开始故事' }))

    await waitFor(() => expect(onCreate).toHaveBeenCalledTimes(1))
    expect(onCreate.mock.calls[0]?.[0]).toMatchObject({
      protagonist: { mode: 'default' },
      opening: { mode: 'ai' },
    })
  })

  it('keeps all story initialization controls behind one progressive disclosure', async () => {
    render(
      <NewStorySetupPanel
        projectId="project-1"
        tellers={[teller]}
        planningTemplates={[planningTemplate]}
        imagePresets={[]}
        loreItems={[]}
        bookOpeningPresets={[]}
        conversationConfig={conversationConfigController()}
        onCancel={vi.fn()}
        onCreate={vi.fn()}
      />,
    )

    expect(await screen.findByRole('heading', { name: 'Game Agent' })).toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: '模型配置' })).toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: '思考强度' })).toHaveTextContent('中')
    expect(screen.getByRole('combobox', { name: '游戏规划' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '回合判定' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '互动图像' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '状态面板' })).toBeInTheDocument()
    expect(screen.queryByText('主舞台展示')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /高级设置/ }))
    expect(screen.queryByRole('heading', { name: '回合判定' })).not.toBeInTheDocument()
  })

  it('restores every check default when a released story summary omits zero values', async () => {
    const user = userEvent.setup()
    const onCreate = vi.fn().mockResolvedValue(undefined)
    const story: StorySummary = {
      id: 'released-story',
      title: '雾港旧事',
      title_source: 'pending',
      origin: '',
      protagonist: { mode: 'lore', name: '林川', profile: loreCharacter.content, source_lore_item_id: loreCharacter.id },
      story_teller_id: teller.id,
      planning_template_id: planningTemplate.id,
      planning_mode: 'enabled',
      reply_target_chars: 2000,
      choice_count: 5,
      opening: { mode: 'ai' },
      check_settings: {
        rule_state_consumption_mode: 'hybrid_auto',
        rule_visibility_mode: 'audit_only',
      },
      created_at: '2026-08-30T00:00:00Z',
      updated_at: '2026-08-30T00:00:00Z',
      branches: 1,
      events: 0,
      turn_count: 0,
    }

    render(
      <NewStorySetupPanel
        projectId="project-1"
        story={story}
        tellers={[teller]}
        planningTemplates={[planningTemplate]}
        imagePresets={[]}
        loreItems={[loreCharacter]}
        conversationConfig={conversationConfigController()}
        onCancel={vi.fn()}
        onCreate={onCreate}
      />,
    )

    expect(screen.getByRole('combobox', { name: '全局难度' })).toHaveTextContent('标准')
    expect(screen.getByRole('spinbutton', { name: '骰点修正' })).toHaveValue(0)
    await user.click(screen.getByRole('button', { name: '开始故事' }))

    await waitFor(() => expect(onCreate).toHaveBeenCalledTimes(1))
    expect(onCreate.mock.calls[0]?.[0]).toMatchObject({
      check_settings: {
        difficulty_shift: 0,
        roll_modifier: 0,
        rule_state_consumption_mode: 'hybrid_auto',
        rule_visibility_mode: 'audit_only',
      },
    })
  })

  it('waits for the effective opening runtime configuration before enabling start', async () => {
    const loadingConfig: ConversationConfigController = {
      ...conversationConfigController(),
      snapshot: null,
      initialized: false,
      loading: true,
    }
    const props = {
      projectId: 'project-1',
      tellers: [teller],
      planningTemplates: [planningTemplate],
      imagePresets: [],
      loreItems: [loreCharacter],
      onCancel: vi.fn(),
      onCreate: vi.fn(),
    }
    const { rerender } = render(<NewStorySetupPanel {...props} conversationConfig={loadingConfig} />)

    expect(screen.getByRole('button', { name: /加载中/ })).toBeDisabled()

    rerender(<NewStorySetupPanel {...props} conversationConfig={conversationConfigController()} />)

    await waitFor(() => expect(screen.getByRole('button', { name: '开始故事' })).toBeEnabled())
    expect(screen.getByRole('combobox', { name: '模型配置' })).toHaveTextContent('默认模型')
    expect(screen.getByRole('combobox', { name: '思考强度' })).toHaveTextContent('中')
  })

  it('keeps loaded model controls stable while saving and refreshing remembered choices', async () => {
    const controller = conversationConfigController()
    const props = {
      projectId: 'project-1',
      tellers: [teller],
      planningTemplates: [planningTemplate],
      imagePresets: [],
      loreItems: [loreCharacter],
      onCancel: vi.fn(),
      onCreate: vi.fn(),
    }
    const { rerender } = render(<NewStorySetupPanel {...props} conversationConfig={controller} />)
    const model = screen.getByRole('combobox', { name: '模型配置' })
    const thinking = screen.getByRole('combobox', { name: '思考强度' })
    const descriptionText = '用于当前故事；模型和思考强度也会记住，用于新故事。'
    const description = screen.getByText(descriptionText)
    await waitFor(() => expect(model).toBeEnabled())
    expect(screen.getByRole('button', { name: '开始故事' })).toBeEnabled()

    rerender(<NewStorySetupPanel {...props} conversationConfig={{ ...controller, saving: true }} />)
    expect(description).toHaveTextContent(descriptionText)
    expect(screen.getByRole('combobox', { name: '模型配置' })).toBe(model)
    expect(model).toBeDisabled()
    expect(thinking).toBeDisabled()
    expect(screen.getByRole('button', { name: '开始故事' })).toBeDisabled()

    const updated = { ...controller, snapshot: { ...controller.snapshot!, profile_id: 'creative', thinking_level: 'high' as const } }
    rerender(<NewStorySetupPanel {...props} conversationConfig={{ ...updated, loading: true }} />)
    expect(description).toHaveTextContent(descriptionText)
    expect(model).toHaveTextContent('创意模型')
    expect(thinking).toHaveTextContent('高')
    expect(model).toBeDisabled()
    expect(screen.getByRole('button', { name: '开始故事' })).toBeDisabled()

    rerender(<NewStorySetupPanel {...props} conversationConfig={updated} />)
    expect(screen.getByRole('combobox', { name: '模型配置' })).toBe(model)
    expect(description).toHaveTextContent(descriptionText)
    expect(model).toBeEnabled()
    expect(screen.getByRole('button', { name: '开始故事' })).toBeEnabled()
  })

  it('seeds book resources once and keeps explicit background clearing local', async () => {
    const user = userEvent.setup()
    const onCreate = vi.fn().mockResolvedValue(undefined)
    settingsMocks.fetchProjectSettings.mockResolvedValue({ workspace: { game_creation_defaults: {
      narrative_style_id: teller.id, event_package_ids: [], default_background: { mode: 'image', item_id: 'hero', asset_id: 'background' },
    } }, effective: {} })
    vi.mocked(getProjectLoreItems).mockResolvedValue([loreCharacter])
    const props = { projectId: 'project-1', tellers: [teller], planningTemplates: [planningTemplate], imagePresets: [], loreItems: [loreCharacter], conversationConfig: conversationConfigController(), onCancel: vi.fn(), onCreate }
    const { rerender } = render(<NewStorySetupPanel {...props} />)
    await screen.findByText('已使用本书默认资源；成功创建后会记住本次配置。')
    await user.click(screen.getByRole('button', { name: '当前背景' }))
    await user.click(screen.getByRole('button', { name: '无背景' }))
    // Late resource/settings refreshes must not replace edits in this open form.
    settingsMocks.fetchProjectSettings.mockResolvedValue({ workspace: { game_creation_defaults: { narrative_style_id: 'different' } }, effective: {} })
    rerender(<NewStorySetupPanel {...props} loreItems={[...props.loreItems]} />)
    await user.click(screen.getByRole('button', { name: '开始故事' }))
    await waitFor(() => expect(onCreate).toHaveBeenCalledOnce())
    expect(onCreate.mock.calls[0][0]).toMatchObject({ story_teller_id: teller.id, module_refs: { narrative_style_id: teller.id, event_packages_disabled: true }, presentation_settings: { background: true, characters: true } })
    expect(onCreate.mock.calls[0][0].presentation_settings.default_background).toBeUndefined()
  })

  it('preserves released story metadata when resuming setup without exposing the old fields', async () => {
    const user = userEvent.setup()
    const onCreate = vi.fn().mockResolvedValue(undefined)
    const story: StorySummary = {
      id: 'legacy-story',
      title: '雾港旧事',
      title_source: 'user',
      origin: '必须保留的旧故事设想。',
      protagonist: { mode: 'lore', name: '林川', profile: loreCharacter.content, source_lore_item_id: loreCharacter.id },
      story_teller_id: teller.id,
      planning_template_id: planningTemplate.id,
      planning_mode: 'enabled',
      reply_target_chars: 2000,
      choice_count: 5,
      opening: { mode: 'ai' },
      created_at: '2026-08-30T00:00:00Z',
      updated_at: '2026-08-30T00:00:00Z',
      branches: 1,
      events: 0,
      turn_count: 0,
    }

    render(
      <NewStorySetupPanel
        projectId="project-1"
        story={story}
        tellers={[teller]}
        planningTemplates={[planningTemplate]}
        imagePresets={[]}
        loreItems={[loreCharacter]}
        conversationConfig={conversationConfigController()}
        onCancel={vi.fn()}
        onCreate={onCreate}
      />,
    )

    expect(screen.queryByDisplayValue('雾港旧事')).not.toBeInTheDocument()
    expect(screen.queryByDisplayValue('必须保留的旧故事设想。')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '开始故事' }))

    await waitFor(() => expect(onCreate).toHaveBeenCalledTimes(1))
    expect(onCreate.mock.calls[0]?.[0]).toMatchObject({
      title: '雾港旧事',
      origin: '必须保留的旧故事设想。',
    })
  })
})
