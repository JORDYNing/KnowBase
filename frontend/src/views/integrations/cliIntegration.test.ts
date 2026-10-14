import assert from 'node:assert/strict'
import test from 'node:test'
import { spawnSync } from 'node:child_process'
import { buildCLIConnectCommand, CLI_MCP_CONFIG } from './cliIntegration'

test('CLI hosts preserve proxy prefixes and omit the SDK API suffix', () => {
  for (const [base, origin, host] of [
    ['https://kb.example.com/api/v1', 'https://ui.example.com', 'https://kb.example.com'],
    ['/app/weknora/api/v1', 'https://kb.example.com', 'https://kb.example.com/app/weknora'],
    ['http://127.0.0.1:19321/api/v1/', 'wails://wails.localhost', 'http://127.0.0.1:19321'],
    ['http://127.0.0.1:19321/api/v1', 'null', 'http://127.0.0.1:19321'],
  ]) {
    const command = buildCLIConnectCommand(base!, origin!)
    assert.equal(command, `gamelib profile add gamelib --host '${host}' --use &&\nprintf '%s\\n' "$GAMELIB_API_KEY" | gamelib auth login --with-token`)
  }
})

test('unresolved desktop URLs use an explicit server placeholder', () => {
  assert.ok(buildCLIConnectCommand('/api/v1', 'null').includes("--host 'https://your-server.com'"))
  assert.ok(buildCLIConnectCommand('/api/v1', 'wails://wails.localhost').includes("--host 'https://your-server.com'"))
})

test('copyable host arguments remain literal in a POSIX shell', () => {
  const host = "https://kb.example.com/team'/$HOME/`printf-injected`/$(printf-injected)"
  const command = buildCLIConnectCommand(`${host}/api/v1`, 'https://kb.example.com')
  const argument = command.split(' --host ')[1]!.split(' --use')[0]!
  const result = spawnSync('/bin/sh', ['-c', `printf '%s' ${argument}`], { encoding: 'utf8' })
  assert.equal(result.status, 0)
  assert.equal(result.stderr, '')
  assert.equal(result.stdout, "https://kb.example.com/team'/$HOME/%60printf-injected%60/$(printf-injected)")
})

test('CLI MCP example uses the gamelib command and connection profile', () => {
  assert.deepEqual(JSON.parse(JSON.stringify(CLI_MCP_CONFIG, null, 2)), {
    mcpServers: {
      gamelib: { command: 'gamelib', args: ['--profile', 'gamelib', 'mcp', 'serve'] },
    },
  })
})

test('CLI connection passes the API key through stdin without putting it in command arguments', () => {
  const command = buildCLIConnectCommand('/api/v1', 'https://kb.example.com')
  const shell = `gamelib() {
    if [ "$1" = "profile" ]; then return 0; fi
    [ "$1" = "auth" ] && [ "$2" = "login" ] && [ "$3" = "--with-token" ] && [ "$#" -eq 3 ] || return 2
    cat
  }
${command}`
  const result = spawnSync('/bin/sh', ['-c', shell], { encoding: 'utf8', env: { GAMELIB_API_KEY: 'test-key with spaces' } })
  assert.equal(result.status, 0)
  assert.equal(result.stdout, 'test-key with spaces\n')
})
