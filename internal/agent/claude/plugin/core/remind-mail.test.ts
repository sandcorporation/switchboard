// 남긴 것 다시 알림(#571): 시스템 통지로 넣은 뒤 항목마다 --verdict 를 붙인 complete 꼬리. 옛 CLI 면 올리라는 안내.
import { expect, test } from 'claude-code/testing'

import { MAIL_VERDICT_MIN_VERSION, noticeText } from './mailbox'
import { setup } from './mailtest'
import { flush } from './testing'

const SYSTEM = 'switchboard/system'
const SUBJECT = '[남긴 것] 2건 처리 안 됨'
const remindBody = (id: number) =>
  [
    '남긴 것이 1일 넘게 처리되지 않았다.',
    '',
    '#3 시험 보강 — 지시 #40 빌드',
    '#5 문서 — 지시 #40 빌드',
    '',
    '판정(항목마다): done 끝남 · keep 그대로 둠(다음 알림에 다시) · drop:까닭 버림 · issue:URL 이슈로 넘김',
    `끝내기: complete ${id} --verdict 3=판정 --verdict 5=판정`,
  ].join('\n')
const version = (v: string | undefined) => ({ mail: { cliVersion: async () => v } })

test('다시 알림: 통지 뒤에 열린 항목마다 --verdict 꼴 complete 명령을 붙이고, 읽음만 표시한다', async () => {
  const { host, mail } = await setup(version(MAIL_VERDICT_MIN_VERSION))
  const id = mail.send('normal', remindBody(1), undefined, { sender: SYSTEM, subject: SUBJECT, left_remind: [3, 5] })
  await flush()
  expect(host.turns.length).toBe(1)
  const t = host.turns[0]!
  expect(t.startsWith(noticeText({ id, sender: SYSTEM, subject: SUBJECT }, remindBody(id)))).toBe(true)
  expect(t).toContain(`switchboard complete ${id} --verdict 3=done|keep|drop:까닭|issue:URL --verdict 5=done|keep|drop:까닭|issue:URL`)
  expect(t).toContain('열린 항목(#3 #5)마다 판정해야 끝난다')
  expect(t).not.toContain('--left-none')
  // 플러그인은 끝내지 않는다: read 만.
  expect(mail.status(id)).toBe('read')
})

test('다시 알림: 서버가 준 열린 항목만 꼬리에 싣는다(본문에 있어도 끝난 항목은 빼고)', async () => {
  const { host, mail } = await setup(version('0.66.1'))
  const id = mail.send('normal', remindBody(1), undefined, { sender: SYSTEM, subject: SUBJECT, left_remind: [5] })
  await flush()
  const t = host.turns[0]!
  expect(t).toContain(`switchboard complete ${id} --verdict 5=done|keep|drop:까닭|issue:URL`)
  expect(t).not.toContain('--verdict 3=done')
})

test('다시 알림: 서버가 열린 항목이 없다고 하면([]) 본문 끝내기 줄을 읽지 않고 통지만 넣는다', async () => {
  const { host, mail } = await setup(version('0.66.1'))
  const id = mail.send('normal', remindBody(1), undefined, { sender: SYSTEM, subject: SUBJECT, left_remind: [] })
  await flush()
  expect(host.turns).toEqual([noticeText({ id, sender: SYSTEM, subject: SUBJECT }, remindBody(id))])
})

test('다시 알림: left_remind 를 안 주는 서버면 본문 끝내기 줄에서 항목을 읽는다', async () => {
  const { host, mail } = await setup(version('0.66.1'))
  const id = mail.send('normal', remindBody(1), undefined, { sender: SYSTEM, subject: SUBJECT })
  await flush()
  expect(host.turns[0]).toContain(`switchboard complete ${id} --verdict 3=done|keep|drop:까닭|issue:URL --verdict 5=done|keep|drop:까닭|issue:URL`)
})

test('다시 알림: --verdict 를 모르는 옛 CLI 면 새 판으로 올리라고 한다', async () => {
  for (const v of ['0.66.0', '0.64.0', undefined]) {
    const { host, mail } = await setup(version(v))
    const id = mail.send('normal', remindBody(1), undefined, { sender: SYSTEM, subject: SUBJECT, left_remind: [3, 5] })
    await flush()
    const t = host.turns[0]!
    expect(t).toContain(`switchboard CLI(${v ?? '판 모름'})는 --verdict 를 모른다(${MAIL_VERDICT_MIN_VERSION} 부터)`)
    expect(t).toContain(`switchboard CLI 를 새 판으로 올린 뒤 끝낸다: switchboard complete ${id} --verdict 3=`)
    expect(mail.status(id)).toBe('read')
  }
})

test('다시 알림 아닌 시스템 통지·일반 메일은 꼬리가 그대로다', async () => {
  const { host, mail } = await setup(version('0.66.1'))
  const n = mail.send('normal', '멈춤 알림', undefined, { sender: SYSTEM, subject: '[switchboard] 멈춤' })
  await flush()
  expect(host.turns).toEqual([noticeText({ id: n, sender: SYSTEM, subject: '[switchboard] 멈춤' }, '멈춤 알림')])
  // 일반 보낸이가 [남긴 것] 제목을 써도 다시 알림이 아니다.
  const g = mail.send('normal', '끝내기: complete 9 --verdict 3=판정', undefined, { subject: SUBJECT })
  host.emit({ type: 'turn.complete' })
  await flush()
  await host.clock.advance(20_000)
  await flush()
  const t = host.turns[1]!
  expect(t).toContain(`#${g} ${SUBJECT}`)
  expect(t).toContain(`답하고 끝내기: switchboard reply ${g} --complete --left`)
  expect(t).not.toContain('--verdict 3=done')
})
