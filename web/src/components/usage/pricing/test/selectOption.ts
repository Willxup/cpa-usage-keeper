import { act } from 'react'

// 通过真实 Keeper 选择菜单选择选项，覆盖 portal 内的交互而非模拟原生 change。
export async function selectOption(trigger: HTMLElement, label: string) {
  await act(async () => trigger.click())
  const option = [...document.querySelectorAll<HTMLButtonElement>('[role="option"]')].find((item) => item.textContent === label)
  if (!option) throw new Error(`Missing option: ${label}`)
  await act(async () => option.click())
}
