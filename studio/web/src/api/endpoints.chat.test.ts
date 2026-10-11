import { afterEach, describe, expect, it, vi } from 'vitest'
import { chatApi } from './endpoints.chat'

function stub(data: unknown = {}) {
  const fn = vi.fn<typeof fetch>(() =>
    Promise.resolve(new Response(JSON.stringify({ success: true, data, error: null }), { status: 200, headers: { 'Content-Type': 'application/json' } })),
  )
  vi.stubGlobal('fetch', fn)
  return fn
}

afterEach(() => vi.unstubAllGlobals())

const call = (fn: ReturnType<typeof stub>) => {
  const [url, init] = fn.mock.calls[0]
  return { url: String(url), method: init?.method, body: init?.body ? JSON.parse(String(init.body)) : undefined }
}

describe('agent chat endpoints', () => {
  it('lists the agents you can chat with (null as none)', async () => {
    const fn = stub(null)
    expect(await chatApi.agents()).toEqual([])
    expect(call(fn)).toMatchObject({ url: '/api/chat/agents', method: 'GET' })
  })

  it('filters conversations by agent', async () => {
    const fn = stub([])
    await chatApi.conversations(undefined, undefined, 'a1')
    expect(call(fn).url).toBe('/api/chat/conversations?agent_id=a1')
  })

  it('starts a conversation with an agent', async () => {
    const fn = stub({})
    await chatApi.startWithAgent('a1', 'hi')
    expect(call(fn)).toEqual({ url: '/api/chat/conversations', method: 'POST', body: { agent_id: 'a1', content: 'hi' } })
  })

  it('reads and saves an agent’s chat settings', async () => {
    let fn = stub({})
    await chatApi.agentSettings('a1')
    expect(call(fn)).toMatchObject({ url: '/api/agents/a1/chat', method: 'GET' })
    vi.unstubAllGlobals()
    fn = stub({})
    const input = { enabled: true, llm_id: null, specialist_ids: ['s1'], crew_ids: [], connection_ids: ['k1'], daily_budget_usd: 1, person_daily_budget_usd: 5,
      handoffs_enabled: true, handoff_budget_usd: 0.5, handoff_timeout_minutes: 20, max_active_handoffs: 2,
      questions_enabled: true, question_rules: 'Ask about prices', question_timeout_minutes: 30, question_on_timeout: 'proceed' as const, max_questions: 3,
      specialists_may_propose: true }
    await chatApi.saveAgentSettings('a1', input)
    expect(call(fn)).toEqual({ url: '/api/agents/a1/chat', method: 'PUT', body: input })
  })

  it('reads and cancels a hand-off', async () => {
    let fn = stub({})
    await chatApi.handoff('h1')
    expect(call(fn)).toMatchObject({ url: '/api/chat/handoffs/h1', method: 'GET' })
    vi.unstubAllGlobals()
    fn = stub({})
    await chatApi.cancelHandoff('h1')
    expect(call(fn)).toEqual({ url: '/api/chat/handoffs/h1/cancel', method: 'POST', body: {} })
  })

  it('lists a conversation’s questions (null as none)', async () => {
    const fn = stub(null)
    expect(await chatApi.questions('c1')).toEqual([])
    expect(call(fn)).toMatchObject({ url: '/api/chat/conversations/c1/questions', method: 'GET' })
  })
})
