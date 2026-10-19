import { onBeforeUnmount, watch, type Ref } from 'vue'

// Closes a popover when the pointer goes down outside `root` or Escape is
// pressed, while `open` is true.
export function useDismiss(root: Ref<HTMLElement | null>, open: Ref<boolean>) {
  const onDown = (e: MouseEvent) => {
    if (root.value && !root.value.contains(e.target as Node)) open.value = false
  }
  const onKey = (e: KeyboardEvent) => {
    if (e.key === 'Escape') open.value = false
  }
  const stop = () => {
    document.removeEventListener('mousedown', onDown)
    document.removeEventListener('keydown', onKey)
  }
  watch(open, (o) => {
    stop()
    if (o) {
      document.addEventListener('mousedown', onDown)
      document.addEventListener('keydown', onKey)
    }
  })
  onBeforeUnmount(stop)
}
