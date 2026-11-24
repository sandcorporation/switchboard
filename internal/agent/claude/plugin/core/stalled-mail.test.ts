// #590 read 는 턴이 시작될 때, 진행 없는 멈춤(stalled:queued·stalled:turn)은 presence 와 대장 메일 한 통.
import { expect, test } from 'claude-code/testing'

import { EP, inlined, setup } from './mailtest'
import { QUEUED_STALL_MS, TURN_STALL_MS } from './stall'
import { flush } from './testing'
import type { FakeHost } from './testing'

const BOSS = 'team/boss'
const QUEUED_MS = 60_000
const TURN_MS = 120_000
const CHECK_MS = 10_000
const board = { permissions: { waitMs: 0, bosses: [BOSS] }, stall: { progress: { queuedMs: QUEUED_MS, turnMs: TURN_MS, checkMs: CHECK_MS } } }

/** 플러그인이 보낸 메일(받는 쪽·제목·본문). */
const sent = (host: FakeHost) =>
  host.http.requests
    .filter(r => r.url === `${EP}/api/messages` && r.init.method === 'POST')
    .map(r => JSON.parse(String(r.init.body)) as { recipient: string; subject: string; body: string })

test('문턱은 상수다: 넣고 안 돈 메일 10분, 움직임 없는 턴 20분', () => {
  expect(QUEUED_STALL_MS).toBe(10 * 60_000)
  expect(TURN_STALL_MS).toBe(20 * 60_000)
})

test('정상 세션: 넣은 메일은 그 턴이 시작되면 read 가 되고 멈춤이 없다', async () => {
  const { host, mail, board: b } = await setup({ board })
  const a = mail.send()
  await flush()
  expect(host.turns).toEqual([inlined([a])])
  expect(mail.status(a)).toBe('read')
  host.emit({ type: 'turn.complete' })
  await host.clock.advance(QUEUED_MS * 3)
  expect(b.state().condition).toBe(null)
  expect(sent(host)).toEqual([])
})

test('턴이 시작되지 않는 세션: 넣은 메일은 read 가 되지 않고, 문턱 뒤 stalled:queued 와 대장 메일 한 통, 턴이 돌면 read·풀림', async () => {
  // 엔진이 넣은 글의 턴을 시작하지 않는다(앞 턴이 끝나지 않은 세션 흉내).
  const { host, mail, board: b } = await setup({ board, engine: false })
  const a = mail.send('normal', '주기 점검')
  await flush()
  expect(host.turns.length).toBe(1)
  expect(host.turns[0]).toContain('주기 점검')
  expect(mail.status(a)).toBe('pending')

  // 사람이 친 다른 턴은 그 메일의 턴이 아니다.
  host.emit({ type: 'turn.start', text: '사람이 친 글' })
  await flush()
  expect(mail.status(a)).toBe('pending')
  host.emit({ type: 'turn.complete' })
  await flush()

  await host.clock.advance(QUEUED_MS - CHECK_MS)
  expect(b.state().condition).toBe(null)
  await host.clock.advance(CHECK_MS * 2)
  expect(b.state().condition).toBe('stalled:queued')
  const notes = sent(host)
  expect(notes.length).toBe(1)
  expect(notes[0]!.recipient).toBe(BOSS)
  expect(notes[0]!.subject).toContain('멈춤 queued')
  expect(notes[0]!.body).toContain('넣고 안 돈 메일: 1통')
  expect(notes[0]!.body).toContain('마지막 움직임: ')
  expect(mail.status(a)).toBe('pending')

  // 멈춤 하나당 한 번.
  await host.clock.advance(QUEUED_MS * 5)
  expect(sent(host).length).toBe(1)

  // 그 글로 턴이 시작되면(앞뒤에 덧붙어도) read, 멈춤이 풀린다.
  host.emit({ type: 'turn.start', text: `앞 글\n${host.turns[0]}\n뒤 글` })
  await flush()
  expect(mail.status(a)).toBe('read')
  expect(b.state().condition).toBe(null)
  await host.clock.advance(QUEUED_MS * 2)
  expect(b.state().condition).toBe(null)
  expect(sent(host).length).toBe(1)
})

test('움직임 없는 턴: 문턱 뒤 stalled:turn 과 대장 메일 한 통(넣고 안 돈 메일 수), 도구가 움직이면 풀린다', async () => {
  // 넣고 안 돈 메일 문턱은 멀리 둔다(이 시험은 턴 멈춤만 본다).
  const slow = { ...board, stall: { progress: { queuedMs: TURN_MS * 100, turnMs: TURN_MS, checkMs: CHECK_MS } } }
  const { host, mail, board: b } = await setup({ board: slow, features: { midTurnInject: false } })
  host.emit({ type: 'turn.start', text: '긴 일' })
  await flush()
  // 바쁜 중에 온 긴급 메일은 그 턴 뒤 새 턴으로 넣는다: 턴이 끝나지 않아 read 가 되지 않는다.
  const u = mail.send('urgent', '지금 봐라')
  await flush()
  expect(host.turns.length).toBe(1)
  expect(mail.status(u)).toBe('pending')

  await host.clock.advance(TURN_MS - CHECK_MS)
  expect(b.state().condition).toBe(null)
  await host.clock.advance(CHECK_MS * 2)
  expect(b.state().condition).toBe('stalled:turn')
  const notes = sent(host)
  expect(notes.length).toBe(1)
  expect(notes[0]!.recipient).toBe(BOSS)
  expect(notes[0]!.subject).toContain('멈춤 turn')
  expect(notes[0]!.body).toContain('넣고 안 돈 메일: 1통')
  await host.clock.advance(TURN_MS * 3)
  expect(sent(host).length).toBe(1)

  // 움직이면 풀린다. 턴이 끝나면 넣은 메일의 턴이 시작되고 read.
  host.emit({ type: 'activity' })
  await flush()
  expect(b.state().condition).toBe(null)
  host.emit({ type: 'turn.complete' })
  await flush()
  expect(mail.status(u)).toBe('read')
  expect(b.state().condition).toBe(null)
})

test('움직이는 긴 턴은 멈춤이 아니다(도구·서브에이전트 움직임)', async () => {
  const { host, board: b } = await setup({ board })
  host.emit({ type: 'turn.start', text: '긴 일' })
  await flush()
  for (let i = 0; i < 6; i++) {
    await host.clock.advance(TURN_MS / 2)
    host.emit(i % 2 === 0 ? { type: 'activity' } : { type: 'tool.complete' })
    await flush()
  }
  expect(b.state().condition).toBe(null)
  expect(sent(host)).toEqual([])
})

test('오류로 멈춘 세션은 오류 condition 을 stalled 로 덮지 않는다', async () => {
  const { host, board: b } = await setup({ board })
  host.emit({ type: 'turn.start', text: '일' })
  await flush()
  host.emit({ type: 'notification', kind: 'permission_prompt' })
  await flush()
  await host.clock.advance(TURN_MS * 2)
  expect(b.state().condition).toBe('blocked:permission_prompt')
})
