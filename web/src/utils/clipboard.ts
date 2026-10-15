export const copyTextToClipboard = async (content: string) => {
  const clipboard = globalThis.navigator?.clipboard
  if (clipboard) {
    try {
      await clipboard.writeText(content)
      return
    } catch {
      // HTTP 局域网页面可能禁止 Clipboard API，继续使用 textarea 兜底。
    }
  }

  if (typeof document === 'undefined' || typeof document.execCommand !== 'function') {
    throw new Error('clipboard is not available')
  }
  const previouslyFocused = document.activeElement instanceof HTMLElement ? document.activeElement : null
  const textarea = document.createElement('textarea')
  textarea.value = content
  textarea.readOnly = true
  textarea.tabIndex = -1
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  textarea.style.pointerEvents = 'none'
  textarea.style.top = '0'
  textarea.style.left = '0'
  document.body.appendChild(textarea)
  textarea.focus()
  textarea.select()
  try {
    if (!document.execCommand('copy')) throw new Error('copy command failed')
  } finally {
    textarea.remove()
    if (previouslyFocused?.isConnected) previouslyFocused.focus()
  }
}

