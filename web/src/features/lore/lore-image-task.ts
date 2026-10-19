import type { LoreItemImageGenerateRequest } from '@/lib/api'

export type LoreBatchImageMode = 'missing_covers' | 'additional'

/** The existing configuration Agent owns execution, cancellation and its journal. */
export function loreImageTaskInstruction(
  ids: string[],
  request: LoreItemImageGenerateRequest,
  mode: LoreBatchImageMode,
) {
  return [
    'Generate images for the selected lore items as one managed task.',
    `Exact lore item IDs: ${JSON.stringify(ids)}`,
    `Image preset ID: ${JSON.stringify(request.image_preset_id || 'game-cg')}`,
    `Additional user requirements: ${request.instruction?.trim() || 'No additional user requirements.'}`,
    'Read the exact lore items with query_lore_items using ids and detail=full. Follow next_offset with the same ids to read every selected item. Read the selected image_preset with config_read when available.',
    mode === 'missing_covers'
      ? 'Only fill missing covers. Use cover_asset_id from each query_lore_items result and skip the item when it is nonempty. An item with other image materials but no cover is still eligible. Call generate_image with lore_cover=if_missing; this sets a cover only if it is still missing at commit time.'
      : 'Generate one additional image per selected item. Omit lore_cover and preserve the existing cover.',
    'For each eligible item, author a complete final model-native prompt from its lore content, the image preset, the additional requirements, and the prompt guide in generate_image. Then call generate_image once with purpose=lore_item and that exact lore_item_id.',
    'Always append images and preserve existing materials and lore text. Continue after an individual failure and report generated, skipped, and failed item IDs at the end. Do not create or edit lore text. Do not add a negative prompt.',
  ].join('\n')
}
