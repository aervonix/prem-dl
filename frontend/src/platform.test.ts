import {describe, expect, it} from 'vitest'
import {systemWords} from './platform'

describe('systemWords', () => {
  it('keeps the Windows names', () => {
    expect(systemWords('windows')).toEqual({
      matchTheme: 'Match Windows',
      notifications: 'Windows notifications',
      startAtSignIn: 'Start with Windows',
      inTray: 'in the notification area',
    })
  })

  it("doesn't say Windows elsewhere", () => {
    for (const word of Object.values(systemWords('linux'))) expect(word).not.toMatch(/windows/i)
  })
})
