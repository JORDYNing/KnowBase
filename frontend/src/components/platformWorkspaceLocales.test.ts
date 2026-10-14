import assert from 'node:assert/strict'
import test from 'node:test'
import en from '../i18n/locales/en-US.ts'
import zh from '../i18n/locales/zh-CN.ts'
import tw from '../i18n/locales/zh-TW.ts'
import ja from '../i18n/locales/ja-JP.ts'
import ko from '../i18n/locales/ko-KR.ts'
import ru from '../i18n/locales/ru-RU.ts'

test('platform catalog controls and CLI token instructions are translated in every supported locale', () => {
  for (const locale of [en, zh, tw, ja, ko, ru]) {
    assert.deepEqual(Object.keys(locale.platformWorkspaces).sort(), Object.keys(en.platformWorkspaces).sort())
    for (const text of Object.values(locale.platformWorkspaces)) assert.ok(text.trim())
    assert.ok(locale.integrations.cli.connectDesc.includes('GAMELIB_API_KEY'))
  }
})
