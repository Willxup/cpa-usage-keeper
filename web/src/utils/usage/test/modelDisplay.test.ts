import { describe, expect, it } from 'vitest'
import {
  buildUsageModelTooltipLines,
  getUsageModelDisplay,
  getUsageModelTooltip,
} from '../modelDisplay'

describe('getUsageModelDisplay', () => {
  it.each([
    {
      name: 'distinct values',
      values: [' deepseek-v4-flash ', 'deepseek-v4.1-flash', 'deepseek-flash'],
      expected: { model: 'deepseek-v4-flash', responseModel: 'deepseek-v4.1-flash', modelAlias: 'deepseek-flash' },
    },
    {
      name: 'matching values',
      values: ['DeepSeek-V4-Flash', ' deepseek-v4-flash ', 'DEEPSEEK-V4-FLASH'],
      expected: { model: 'DeepSeek-V4-Flash', responseModel: '', modelAlias: '' },
    },
    {
      name: 'missing request model',
      values: ['', 'served-model', 'client-alias'],
      expected: { model: '-', responseModel: 'served-model', modelAlias: 'client-alias' },
    },
  ])('normalizes $name', ({ values, expected }) => {
    expect(getUsageModelDisplay(values[0], values[1], values[2])).toEqual(expected)
  })

  it('compares model identifiers independently of the browser locale', () => {
    const originalLocaleLowerCase = String.prototype.toLocaleLowerCase
    String.prototype.toLocaleLowerCase = function toLocaleLowerCase() {
      return originalLocaleLowerCase.call(this, 'tr-TR')
    }
    try {
      expect(getUsageModelDisplay('mini', 'MINI', 'MINI')).toEqual({
        model: 'mini',
        responseModel: '',
        modelAlias: '',
      })
    } finally {
      String.prototype.toLocaleLowerCase = originalLocaleLowerCase
    }
  })

  it('keeps raw matching values in the tooltip and omits empty lines', () => {
    const values = getUsageModelTooltip('DeepSeek-V4-Flash', ' deepseek-v4-flash ', '')
    expect(values).toEqual({
      model: 'DeepSeek-V4-Flash',
      responseModel: 'deepseek-v4-flash',
      modelAlias: '',
    })
    expect(buildUsageModelTooltipLines(
      values,
      {
        model: 'Model',
        responseModel: 'Upstream response',
        modelAlias: 'Model Alias',
      },
      (label, value) => `${label}: ${value}`,
    )).toEqual([
      'Model: DeepSeek-V4-Flash',
      'Upstream response: deepseek-v4-flash',
    ])
  })
})
