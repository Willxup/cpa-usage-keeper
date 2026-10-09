import { useRef, type KeyboardEvent } from 'react'

// 两种凭证编辑入口共用：确认输入法候选词不能同时提交修改。
export function useCredentialEnterSave(save: () => Promise<void>) {
  const composing = useRef(false)
  return {
    onCompositionStart: () => { composing.current = true },
    onCompositionEnd: () => { composing.current = false },
    onBlur: () => { composing.current = false },
    onKeyDown: (event: KeyboardEvent<HTMLElement>) => {
      if (!(event.target instanceof HTMLInputElement) || event.target.type !== 'text') return
      const isComposing = composing.current || event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229
      if (event.key === 'Escape' && isComposing) {
        // 保留输入法取消候选词的默认行为，只阻止弹框/浮层的全局关闭监听。
        event.stopPropagation()
        return
      }
      if (event.key !== 'Enter') return
      event.preventDefault()
      // 部分浏览器确认候选词时已结束 composition，但仍以 229 标记该按键。
      if (isComposing) return
      void save()
    },
  }
}
