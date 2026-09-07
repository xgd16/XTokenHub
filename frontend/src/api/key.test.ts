import { describe, expect, it } from 'vitest'
import { maskKey } from './key'

describe('maskKey', () => {
  it('保留前 9 位与后 4 位', () => {
    const key = 'sk-xt-' + 'a'.repeat(32)
    expect(maskKey(key)).toBe('sk-xt-aaa…aaaa')
  })

  it('短密钥原样返回', () => {
    expect(maskKey('sk-xt-short')).toBe('sk-xt-short')
  })
})
