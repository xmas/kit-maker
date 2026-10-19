// Shapes of the props the Go server sends to the kit builder pages
// (internal/kits, internal/kits/compiler, internal/kits/checks).

export type Origin = 'inherited' | 'modified' | 'custom' | 'sub' | 'over' | 'blank'

export interface KitInfo {
  id: string
  slug: string
  displayName: string
  description: string
  kind: string
  teamId: string
  team: string
  goal: string
}

export interface Item {
  key: string
  label: string
  section: string
  required: boolean
  origin: Origin
  edits: number
  status?: 'blank' | 'problem' | 'fail'
  blockType?: string
  blockName?: string
  blockSlug?: string
  version?: number
  latest?: number
  problem?: string
}

export interface Section {
  key: string
  label: string
  items: Item[]
  types: BlockType[]
}

export interface Op {
  op: 'set' | 'unset' | 'add' | 'remove' | 'replace'
  path: string
  key?: string
  after?: string
  value?: unknown
}

export interface SlotDetail extends Item {
  blockDescription?: string
  usedBy: number
  base?: Record<string, any>
  effective?: Record<string, any>
  origins?: Record<string, 'changed' | 'added' | 'removed'>
  ops?: Op[]
  conflicts?: Op[]
}

export interface Compiled {
  yaml: string
  valid: boolean
  error?: string
  warnings?: string[]
  problems?: { slot: string; message: string }[]
  blanks?: string[]
  digest?: string
  spans?: Record<string, [number, number][]>
  marks?: Record<string, 'added' | 'changed' | 'removed'>
}

export interface CheckResult {
  id: string
  label: string
  type: string
  url: string
  method: string
  expect: 'reach' | 'blocked'
  status: 'pass' | 'fail' | 'pending'
  actual?: string
  why: string
  source?: string
}

export interface BlockType {
  key: string
  label: string
  section: string
  capability: string
}

export interface Candidate {
  id: string
  name: string
  slug: string
  type: string
  description: string
  version: number
  usedBy: number
}

// Section colours and icons follow the mockups' block types (DESIGN.md §2.3).
export const sectionStyle: Record<string, { fg: string; bg: string; label: string }> = {
  'agent-base': { fg: '#4338CA', bg: '#EEF0FF', label: 'Agent & base' },
  network: { fg: '#0F766E', bg: '#E6F6F4', label: 'Network' },
  credentials: { fg: '#B45309', bg: '#FEF3D9', label: 'Credentials' },
  setup: { fg: '#334155', bg: '#EEF1F5', label: 'Setup' },
  agent: { fg: '#BE185D', bg: '#FDE8EF', label: 'Agent' },
  runtime: { fg: '#475569', bg: '#F1F3F6', label: 'Runtime' },
}

export const originStyle: Record<Origin, { label: string; fg: string }> = {
  inherited: { label: 'Linked', fg: '#64748B' },
  modified: { label: 'Linked, edited', fg: '#64748B' },
  custom: { label: 'Kit only', fg: '#0F172A' },
  sub: { label: 'From sub-kit', fg: '#4338CA' },
  over: { label: 'Overrides sub-kit', fg: '#C2410C' },
  blank: { label: 'Empty', fg: '#B45309' },
}

export function slotHref(kitId: string, key?: string | null) {
  return key ? `/kits/${kitId}/slots/${key}` : `/kits/${kitId}`
}
