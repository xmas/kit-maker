// Line diff for the YAML panel: what the saved kit was vs. what the unsaved
// edits will make it. Common prefix/suffix are trimmed first (edits are
// local), then the middle is diffed with an LCS table.

export type DiffOp = { type: 'same' | 'add' | 'del'; text: string; oldNo?: number; newNo?: number }

export function diffLines(before: string[], after: string[]): DiffOp[] {
  let start = 0
  while (start < before.length && start < after.length && before[start] === after[start]) start++
  let endB = before.length
  let endA = after.length
  while (endB > start && endA > start && before[endB - 1] === after[endA - 1]) {
    endB--
    endA--
  }

  const ops: DiffOp[] = []
  for (let i = 0; i < start; i++) ops.push({ type: 'same', text: after[i]!, oldNo: i + 1, newNo: i + 1 })

  const b = before.slice(start, endB)
  const a = after.slice(start, endA)
  if (b.length * a.length > 4_000_000) {
    // Too big to diff line by line: show it as replaced.
    b.forEach((t, i) => ops.push({ type: 'del', text: t, oldNo: start + i + 1 }))
    a.forEach((t, i) => ops.push({ type: 'add', text: t, newNo: start + i + 1 }))
  } else {
    // lcs[i][j] = LCS length of b[i:] and a[j:]
    const lcs: Uint32Array[] = Array.from({ length: b.length + 1 }, () => new Uint32Array(a.length + 1))
    for (let i = b.length - 1; i >= 0; i--) {
      for (let j = a.length - 1; j >= 0; j--) {
        lcs[i]![j] = b[i] === a[j] ? lcs[i + 1]![j + 1]! + 1 : Math.max(lcs[i + 1]![j]!, lcs[i]![j + 1]!)
      }
    }
    let i = 0
    let j = 0
    while (i < b.length || j < a.length) {
      if (i < b.length && j < a.length && b[i] === a[j]) {
        ops.push({ type: 'same', text: a[j]!, oldNo: start + i + 1, newNo: start + j + 1 })
        i++
        j++
      } else if (i < b.length && (j === a.length || lcs[i + 1]![j]! >= lcs[i]![j + 1]!)) {
        // Removals before additions, so a replaced line reads as "changed".
        ops.push({ type: 'del', text: b[i]!, oldNo: start + i + 1 })
        i++
      } else {
        ops.push({ type: 'add', text: a[j]!, newNo: start + j + 1 })
        j++
      }
    }
  }

  for (let k = 0; k < before.length - endB; k++) {
    ops.push({ type: 'same', text: before[endB + k]!, oldNo: endB + k + 1, newNo: endA + k + 1 })
  }
  return ops
}

export function splitLines(text: string): string[] {
  return (text || '').replace(/\n$/, '').split('\n')
}
