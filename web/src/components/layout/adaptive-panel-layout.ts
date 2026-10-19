import type { AdaptiveSurfaceSideResize } from './adaptive-surface'

export type AdaptivePanelSize = `${number}px` | `${number}%`

export interface AdaptivePanelSizing {
  left: AdaptiveSurfaceSideResize
  right: AdaptiveSurfaceSideResize
  leftVisible: boolean
  rightVisible: boolean
  rightExpanded: boolean
}

/** Minimum space for all currently visible panes, including the former outer main constraint. */
export function adaptivePanelMinimumWidth(sizing: AdaptivePanelSizing) {
  const main = sizing.rightExpanded ? 0 : parseFloat(sizing.right.mainMinSize ?? '240px')
  const right = sizing.rightVisible ? parseFloat(sizing.right.minSize ?? '300px') : 0
  const left = sizing.leftVisible ? parseFloat(sizing.left.minSize ?? '200px') : 0
  return left + Math.max(parseFloat(sizing.left.mainMinSize ?? '320px'), main + right)
}

export function panelSizeInPixels(size: AdaptivePanelSize, available: number) {
  return size.endsWith('%') ? parseFloat(size) / 100 * available : parseFloat(size)
}

/**
 * The directory owns a preferred pixel width; the two content panes share the remainder by ratio.
 * Clamp the content split once, so a pane at its minimum stops contributing to further shrinkage.
 * Preferred sizes belong to user input and are never replaced by these constrained results.
 */
export function resolveAdaptivePanelLayout(sizing: AdaptivePanelSizing, width: number, leftPixels: number, rightRatio: number) {
  if (width <= 0 || width < adaptivePanelMinimumWidth(sizing)) return null
  const leftMin = sizing.leftVisible ? parseFloat(sizing.left.minSize ?? '200px') : 0
  const mainMin = sizing.rightExpanded ? 0 : parseFloat(sizing.right.mainMinSize ?? '240px')
  const rightMin = sizing.rightVisible ? parseFloat(sizing.right.minSize ?? '300px') : 0
  const leftMax = sizing.leftVisible ? Math.max(leftMin, Math.min(
    panelSizeInPixels(sizing.left.maxSize ?? '40%', width),
    width - Math.max(parseFloat(sizing.left.mainMinSize ?? '320px'), mainMin + rightMin),
  )) : 0
  const left = Math.min(leftMax, Math.max(leftMin, leftPixels))
  const content = width - left
  // Percentage constraints still refer to the content area, as they did in the nested group.
  const rightMax = sizing.rightVisible ? sizing.rightExpanded ? content : Math.max(rightMin, Math.min(
    panelSizeInPixels(sizing.right.maxSize ?? '65%', content), content - mainMin,
  )) : 0
  const right = sizing.rightExpanded ? content : Math.min(rightMax, Math.max(rightMin, content * rightRatio))
  return {
    layout: { left: left / width * 100, main: (content - right) / width * 100, right: right / width * 100 },
    minimum: { left: leftMin, main: mainMin, right: rightMin },
    maximum: { left: leftMax, main: sizing.rightExpanded ? 0 : width, right: rightMax },
  }
}
