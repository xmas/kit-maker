// Shapes of the props the Go server sends to the blueprint builder pages
// (internal/blueprints, internal/blueprints/compiler, internal/blueprints/checks).

export type Origin = 'inherited' | 'modified' | 'custom' | 'sub' | 'over' | 'blank'

export interface BlueprintInfo {
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
  status?: 'blank' | 'problem' | 'fail' | 'conflict'
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
  conflicts?: Clash[]
}

// Clash is blocks in one blueprint working against each other (the
// compiler's Conflict): a deny blocking another block's allow, an outright
// allow cancelling another's limits, a duplicate, or a credential whose
// host no block allows. Fixes hide one entry of one block, here only.
export interface Clash {
  kind: 'blocked' | 'widened' | 'duplicate' | 'gap' | 'missing'
  message: string
  slots: string[]
  fixes?: { slot: string; path: string; key: string; label: string }[]
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
  description: string
  capability?: string
}

export interface Candidate {
  id: string
  name: string
  slug: string
  type: string
  description: string
  version: number
  usedBy: number
  text?: string
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
  custom: { label: 'Blueprint only', fg: '#0F172A' },
  sub: { label: 'From included blueprint', fg: '#4338CA' },
  over: { label: 'Overrides included blueprint', fg: '#C2410C' },
  blank: { label: 'Empty', fg: '#B45309' },
}

export function slotHref(blueprintId: string, key?: string | null) {
  return key ? `/blueprints/${blueprintId}/slots/${key}` : `/blueprints/${blueprintId}`
}
