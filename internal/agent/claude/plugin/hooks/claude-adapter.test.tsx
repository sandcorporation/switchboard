// Claude 어댑터 시험(얕게): 포트 호출이 모드 API로 이어지는지만 본다.
import { expect, test } from 'claude-code/testing'

import {
  changeBy,
  claudeHost,
  wireClaudeEvents,
  createEmitter,
  createMidTurnBox,
  createTurnWatch,
  exportRole,
  mailProfilePath,
  permissionSummary,
  submitOrigin,
} from '../adapters/claude'
import type { Self } from '../adapters/claude'

// 시험의 $ 는 플러그인 $ 의 일부만 가진 Engine 이다(clock·session.usage 등이 없다). 어댑터가 쓰는 것만 부른다.
const asSelf = (e: unknown) => e as Self

test('Claude 어댑터: submitTurn 은 사용자 글로 prompt.submit 한다', async ($, on) => {
  const got: any[] = []
  on('prompt.submit', (_: any, e: any) => {
    got.push(e)

    return { text: e.text }
  })
  const host = claudeHost(asSelf($), createEmitter(), createMidTurnBox())
  await host.submitTurn('[메일] 새 지시')
  expect(got.map(e => e.text)).toEqual(['[메일] 새 지시'])
})

test('Claude 어댑터: compact 는 session.compact 로, 건너뜀은 done:false 로', async ($, on) => {
  const asked: any[] = []
  let skip: string | undefined
  on('session.compact', (_: any, e: any) => {
    asked.push(e)

    return skip === undefined ? { messages: [{ role: 'user', text: '요약', toolUses: [] }] } : { skip }
  })
  const host = claudeHost(asSelf($), createEmitter(), createMidTurnBox())

  expect(host.features).toEqual({ midTurnInject: true, compact: true })
  expect(await host.compact?.('메일 전 정리')).toEqual({ done: true })
  expect(asked[0]?.instructions).toBe('메일 전 정리')
  skip = 'nothing'
  expect(await host.compact?.()).toEqual({ done: false, reason: 'nothing' })
})

test('Claude 어댑터: 받아들인 compact 가 끝났는데 훅 이벤트가 없었으면 완료를 한 번 알린다(#104)', async ($, on) => {
  on('session.compact', () => ({ messages: [{ role: 'user', text: '요약', toolUses: [] }] }))
  const emitter = createEmitter()
  const seen: string[] = []
  emitter.on(e => void seen.push(e.type))
  const host = claudeHost(asSelf($), emitter, createMidTurnBox())
  expect(await host.compact?.()).toEqual({ done: true })
  expect(seen.filter(t => t === 'compact.complete').length).toBe(1)
})

test('Claude 어댑터: identity 는 본 세션으로 답한다', async $ => {
  const me = await claudeHost(asSelf($), createEmitter(), createMidTurnBox()).identity()
  expect(me.isMain).toBe(true)
  expect(typeof me.machine).toBe('string')
})

test('Claude 어댑터: injectMidTurn 은 다음 도구 결과에 붙을 때 풀린다', async () => {
  const box = createMidTurnBox()
  let done: boolean | undefined
  const p = box.put('[긴급] 멈춰라').then(v => void (done = v))
  expect(box.size()).toBe(1)
  expect(done).toBeUndefined()
  expect(box.take()).toEqual(['[긴급] 멈춰라'])
  await p
  expect(done).toBe(true)
  expect(box.take()).toEqual([])
})

test('Claude 어댑터: 턴이 도구 호출 없이 끝나면 끼워 넣기는 false 로 풀리고 버려진다', async () => {
  const box = createMidTurnBox()
  const p = box.put('[긴급] 늦음')
  box.expire()
  expect(await p).toBe(false)
  expect(box.size()).toBe(0)
  expect(box.take()).toEqual([])
})

test('Claude 어댑터: 제출 origin 은 composer·bridge 만 사람이고 플러그인이 넣은 턴은 plugin 이다', () => {
  expect(submitOrigin('composer')).toBe('user')
  expect(submitOrigin('bridge')).toBe('user')
  expect(submitOrigin('plugin')).toBe('plugin')
  expect(submitOrigin('task-notification')).toBe('other')
  expect(submitOrigin(undefined)).toBe('other')
})

test('Claude 어댑터: 권한 요약은 셸 명령·파일 경로·주소를 고르고, 서브에이전트 요청이면 종류를 붙인다', () => {
  expect(permissionSummary('Bash', { command: 'git push', description: '올리기' })).toBe('git push — 올리기')
  expect(permissionSummary('Edit', { file_path: '/w/a.ts', old_string: 'x' })).toBe('/w/a.ts')
  expect(permissionSummary('WebFetch', { url: 'https://example.com', prompt: 'p' }, 'errand')).toBe('[errand] https://example.com')
  expect(permissionSummary('mcp__x__y', { a: 1 })).toBe('{"a":1}')
  expect(permissionSummary('Bash', { command: 'x'.repeat(2000) }).length).toBe(600)
})

test('Claude 어댑터: prompt.change 는 기록 불러오기·되돌리기만 사람 입력이다(#123)', () => {
  expect(changeBy('history')).toBe('person')
  expect(changeBy('undo')).toBe('person')
  for (const k of ['plugin', 'engine', 'app', 'clear', 'unknown-kind']) expect(changeBy(k)).toBe('other')
})

test('Claude 어댑터: 다음 답 추천(prompt.suggest)은 입력 이벤트를 내지 않는다(#123)', async ($, on) => {
  const emitter = createEmitter()
  const seen: unknown[] = []
  emitter.on(e => void seen.push(e))
  wireClaudeEvents(on, emitter, createMidTurnBox())
  // 시험 엔진에는 추천을 그리는 쪽이 없어 보여 줬다고 답하는 끝을 단다.
  on('prompt.suggest', () => ({ isShown: true }))
  await $.prompt.suggest({ text: '응, 옮겨 줘' } as any)
  expect(seen).toEqual([])
})

/** env 만 가진 가짜 $(#130 이름 시험). */
const envSelf = (vars: Record<string, string>) => {
  const env = { ...vars }
  const self = asSelf({
    env: {
      get: async (k: string) => env[k],
      set: async (k: string, v: string) => void (env[k] = v),
    },
    session: { info: async () => ({ platform: 'linux' }) },
  })

  return { self, env }
}

test('Claude 어댑터(#130): 번호 역할은 자식 셸에 SWITCHBOARD_ROLE·AGENT_MAIL_ROLE 둘 다로 넘긴다', async () => {
  const { self, env } = envSelf({})
  await exportRole(self, 'team/worker-2')
  expect(env.SWITCHBOARD_ROLE).toBe('team/worker-2')
  expect(env.AGENT_MAIL_ROLE).toBe('team/worker-2')
})

test('Claude 어댑터(#130): 프로필은 새 이름 우선, 옛 이름만 있어도 켜지고, 주소·토큰 덮어쓰기도 새 이름 우선', async () => {
  const fresh = await mailProfilePath(envSelf({ SWITCHBOARD_PROFILE: 'w', AGENT_MAIL_PROFILE: 'old', HOME: '/home/u' }).self)
  expect(fresh.paths?.[0]).toBe('/home/u/.config/switchboard/w.json')
  const old = await mailProfilePath(envSelf({ AGENT_MAIL_PROFILE: 'old', HOME: '/home/u' }).self)
  expect(old.paths).toEqual(['/home/u/.config/switchboard/old.json', '/home/u/.config/agent-mail/old.json'])
  expect(old.override).toBe(undefined)
  const over = await mailProfilePath(
    envSelf({ SWITCHBOARD_PROFILE: 'w', HOME: '/home/u', SWITCHBOARD_ENDPOINT: 'http://new', AGENT_MAIL_ENDPOINT: 'http://old', AGENT_MAIL_TOKEN: 't-old' }).self,
  )
  expect(over.override).toEqual({ endpoint: 'http://new', token: 't-old' })
  expect((await mailProfilePath(envSelf({ HOME: '/home/u' }).self)).paths).toBe(null)
})

/** 셸 넘기기(#526) 시험용 가짜 $: 시계·환경·턴 끊기만. */
const bgSelf = (o: { env?: Record<string, string>; abortFails?: boolean; version?: string | null }) => {
  const aborted: string[] = []
  const self = asSelf({
    session: {
      version: async () => {
        if (o.version === null) throw new Error('unknown')

        return { version: o.version ?? '2.1.330' }
      },
    },
    env: { get: async (k: string) => o.env?.[k] },
    turn: {
      abort: async ({ turnId }: { turnId: string }) => {
        if (o.abortFails === true) throw new Error('not the running turn')
        aborted.push(turnId)
      },
    },
  })

  return { self, aborted }
}

test('Claude 어댑터(#526): 본 세션의 Bash 가 문턱 넘게 돌면 턴을 끊고 걸린 끼워 넣기를 false 로 푼다', async () => {
  const box = createMidTurnBox()
  let now = 1_000
  const watch = createTurnWatch(() => now)
  watch.turnStart('turn-1')
  watch.begin('Bash')
  now = 31_000
  const { self, aborted } = bgSelf({})
  const host = claudeHost(self, createEmitter(), box, watch)
  const pending = box.put('[긴급 메일]')
  expect(await host.moveShellToBackground?.(30_000)).toBe(true)
  expect(aborted).toEqual(['turn-1'])
  expect(await pending).toBe(false)
  expect(watch.isHeld()).toBe(true) // 끊긴 Bash 결과에 끼워 넣기를 붙이지 않는다.
  watch.turnEnd()
  expect(watch.isHeld()).toBe(false)
})

test('Claude 어댑터(#526): 문턱 전·병렬 Bash 아닌 도구·낮은/모르는 판·턴 없음·백그라운드 꺼짐·끊기 실패면 넘기지 않는다', async () => {
  const cases: {
    name: string
    tools: [string, number][]
    now: number
    turn?: boolean
    env?: Record<string, string>
    abortFails?: boolean
    version?: string | null
  }[] = [
    { name: '문턱 전', tools: [['Bash', 1_000]], now: 30_999 },
    { name: 'Bash 와 병렬로 도는 Bash 아닌 도구', tools: [['Bash', 1_000], ['Agent', 1_000]], now: 60_000 },
    { name: 'Bash 둘과 병렬 Read 하나', tools: [['Bash', 1_000], ['Bash', 2_000], ['Read', 50_000]], now: 60_000 },
    { name: '턴 끊기 넘기기 전 판', tools: [['Bash', 1_000]], now: 60_000, version: '2.1.329' },
    { name: '판을 모름', tools: [['Bash', 1_000]], now: 60_000, version: null },
    { name: '도는 도구 없음', tools: [], now: 60_000 },
    { name: '턴 없음', tools: [['Bash', 1_000]], now: 60_000, turn: false },
    { name: '백그라운드 꺼짐', tools: [['Bash', 1_000]], now: 60_000, env: { CLAUDE_CODE_DISABLE_BACKGROUND_TASKS: '1' } },
    { name: '끊기 실패', tools: [['Bash', 1_000]], now: 60_000, abortFails: true },
  ]
  for (const c of cases) {
    const box = createMidTurnBox()
    let now = 0
    const watch = createTurnWatch(() => now)
    if (c.turn !== false) watch.turnStart('turn-1')
    for (const [tool, at] of c.tools) {
      now = at
      watch.begin(tool)
    }
    now = c.now
    const { self, aborted } = bgSelf({ env: c.env, abortFails: c.abortFails, version: c.version })
    const host = claudeHost(self, createEmitter(), box, watch)
    expect([c.name, await host.moveShellToBackground?.(30_000)]).toEqual([c.name, false])
    expect([c.name, aborted]).toEqual([c.name, []])
    expect([c.name, watch.isHeld()]).toEqual([c.name, false])
  }
})

test('Claude 어댑터(#526): 끝난 도구는 넘길 대상에서 빠지고, 감시 없이 만든 호스트에는 넘기기가 없다', () => {
  let now = 0
  const watch = createTurnWatch(() => now)
  watch.turnStart('turn-1')
  const key = watch.begin('Bash')
  now = 60_000
  expect(watch.movable(30_000)).toBe('turn-1')
  watch.end(key)
  expect(watch.movable(30_000)).toBe(null)
  expect(claudeHost(asSelf({}), createEmitter(), createMidTurnBox()).moveShellToBackground).toBe(undefined)
})
