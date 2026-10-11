import { describe, expect, it } from 'vitest'
import { canUndo, conversationContext, HISTORY_MESSAGES, isWaiting, pendingHandoffs, proposalDetails, replyState, resultLink, resultNote, studioAdds, threadOrder } from './chatLogic'
import { msg } from './testUtils'


describe('replies', () => {
  it('waits while a reply run is queued or running', () => {
    expect(isWaiting([msg({ role: 'user', run_status: null }), msg({ run_status: 'running' })])).toBe(true)
    expect(isWaiting([msg({ run_status: 'queued' })])).toBe(true)
    expect(isWaiting([msg({ run_status: 'succeeded', content: 'x' })])).toBe(false)
  })

  it('does not wait for hand-offs: they run in the background', () => {
    const working = msg({ id: 'h', run_kind: 'handoff', run_status: 'running' })
    const queued = msg({ id: 'q', run_kind: 'handoff', run_status: 'queued' })
    const done = msg({ id: 'd', run_kind: 'handoff', run_status: 'succeeded', content: 'Result' })
    expect(isWaiting([working, queued, done])).toBe(false)
    expect(pendingHandoffs([working, queued, done, msg({ run_status: 'running' })]).map((m) => m.id)).toEqual(['h', 'q'])
    expect(pendingHandoffs([])).toEqual([])
  })

  it('says how to show a reply', () => {
    expect(replyState(msg({ content: 'hi' }))).toBe('text')
    expect(replyState(msg({ run_status: 'running' }))).toBe('waiting')
    expect(replyState(msg({ run_status: 'failed' }))).toBe('failed')
    expect(replyState(msg({ run_status: 'cancelled' }))).toBe('failed')
    expect(replyState(msg({ run_status: null, run_id: null }))).toBe('empty')
  })
})

describe('compaction', () => {
  const user = (id: string, content = 'x'.repeat(400)) => msg({ id, role: 'user', content, run_id: null, run_status: null })
  const reply = (id: string, content = 'y'.repeat(400)) => msg({ id, content })

  it('waits while a summary is being written', () => {
    expect(isWaiting([msg({ role: 'summary', run_status: 'running' })])).toBe(true)
  })

  it('counts what each turn resends: the latest summary and the messages after it', () => {
    expect(conversationContext([])).toEqual({ messages: 0, dropped: 0, tokens: 0, canCompact: false })
    expect(conversationContext([user('u1'), reply('a1')])).toEqual({ messages: 2, dropped: 0, tokens: 200, canCompact: true })
    const compacted = [user('u1'), reply('a1'), msg({ id: 's1', role: 'summary', content: 'z'.repeat(40) }), user('u2')]
    expect(conversationContext(compacted)).toEqual({ messages: 1, dropped: 0, tokens: 110, canCompact: false })
  })

  it('ignores a summary that is still being written or failed', () => {
    const pending = [user('u1'), reply('a1'), msg({ id: 's1', role: 'summary', content: '', run_status: 'failed' })]
    expect(conversationContext(pending).messages).toBe(2)
  })

  it('reports messages that no longer fit, and caps each one', () => {
    const many = Array.from({ length: HISTORY_MESSAGES + 3 }, (_, i) => user(`u${i}`, 'w'.repeat(10_000)))
    const ctx = conversationContext(many)
    expect(ctx.messages).toBe(HISTORY_MESSAGES)
    expect(ctx.dropped).toBe(3)
    expect(ctx.tokens).toBe(HISTORY_MESSAGES * 1000)
  })
})

describe('proposals', () => {
  it('lists a work order proposal with its inputs', () => {
    const rows = proposalDetails({ type: 'start_work_order', summary: 's', payload: { title: 'T', brief: 'B', inputs: { company: 'Acme' } } })
    expect(rows).toEqual([{ label: 'Title', value: 'T' }, { label: 'Brief', value: 'B', block: true }, { label: 'Input company', value: 'Acme' }])
    expect(proposalDetails({ type: 'start_work_order', summary: 's', payload: {} })[0]).toEqual({ label: 'Title', value: '' })
    // From agent chat the card names the crew it would run
    expect(proposalDetails({ type: 'start_work_order', summary: 's', payload: { crew_id: 'c1', crew_name: 'PMO', title: 'T', brief: 'B' } })[0])
      .toEqual({ label: 'Crew', value: 'PMO' })
  })

  it('lists a note change, new or replacing a version', () => {
    const changed = proposalDetails({ type: 'notes_put', summary: 's', payload: { name: 'risks', subject: 'project:atlas', base_version: 2, content: 'x' } })
    expect(changed.map((r) => r.value)).toEqual(['risks', 'project:atlas', 'version 2', 'x'])
    const created = proposalDetails({ type: 'notes_put', summary: 's', payload: { name: 'risks', content: 7 } })
    expect(created.map((r) => r.value)).toEqual(['risks', 'nothing (new note)', '7'])
  })

  it('shows unknown proposal types as JSON', () => {
    expect(proposalDetails({ type: 'track_item', summary: 's', payload: { a: 1 } })[0].value).toContain('"a": 1')
    expect(proposalDetails({ type: 'track_item', summary: 's', payload: undefined as unknown as Record<string, unknown> })[0].value).toBe('{}')
  })

  it('links results and knows what can be undone', () => {
    expect(resultLink('start_work_order', { work_order_id: 'w1', work_order_number: 7 }, 'c1')).toEqual({ to: '/work-orders/w1', label: 'Open WO-7' })
    expect(resultLink('notes_put', { version: 3 }, 'c1')).toEqual({ to: '/crews/c1?tab=state', label: 'Saved as version 3' })
    expect(resultLink('notes_put', null, 'c1')).toBeNull()
    expect(resultLink('start_work_order', {}, 'c1')).toBeNull()
    expect(resultLink('other', { version: 1 }, 'c1')).toBeNull()
    expect(canUndo(msg({ proposal_state: 'confirmed', proposal_result: { undoable: true } }))).toBe(true)
    expect(canUndo(msg({ proposal_state: 'confirmed', proposal_result: { undoable: true }, undone_at: 'x' }))).toBe(false)
    expect(canUndo(msg({ proposal_state: 'confirmed', proposal_result: {} }))).toBe(false)
    expect(canUndo(msg({ proposal_state: 'pending' }))).toBe(false)
  })
})

describe('tracking proposals', () => {
  it('shows what will be tracked and links to the State tab', () => {
    const add = proposalDetails({ type: 'tracking_add', summary: 's', payload: { item_ref: 'PRJ-10', project: 'Atlas', include_children: 'direct', connection: '', note: 'n', lead_account_id: 'acc' } })
    expect(add).toEqual([
      { label: 'Item', value: 'PRJ-10' }, { label: 'Project', value: 'Atlas' }, { label: 'Track', value: 'direct children only' },
      { label: 'Connection', value: "the crew's tracker" }, { label: 'Note', value: 'n' }, { label: 'Lead', value: 'acc' },
    ])
    expect(proposalDetails({ type: 'tracking_add', summary: 's', payload: { item_ref: 'A-1', include_children: 'odd', connection: 'J' } }))
      .toEqual([{ label: 'Item', value: 'A-1' }, { label: 'Track', value: 'odd' }, { label: 'Connection', value: 'J' }])
    expect(proposalDetails({ type: 'tracking_remove', summary: 's', payload: { item_ref: 'PRJ-10', connection: 'J' } }))
      .toEqual([{ label: 'Stop tracking', value: 'PRJ-10' }, { label: 'Connection', value: 'J' }])
    expect(proposalDetails({ type: 'tracking_remove', summary: 's', payload: { item_ref: 'PRJ-10' } })).toHaveLength(1)
    expect(resultLink('tracking_add', { item_ref: 'PRJ-10' }, 'c1')).toEqual({ to: '/crews/c1?tab=state', label: 'Tracking PRJ-10' })
    expect(resultLink('tracking_remove', { item_ref: 'PRJ-10' }, 'c1')).toEqual({ to: '/crews/c1?tab=state', label: 'Stopped tracking PRJ-10' })
    expect(resultLink('tracking_add', {}, 'c1')).toBeNull()
  })
})

describe('hand-off cards', () => {
  it('lists the worker, title, brief and limits', () => {
    const rows = proposalDetails({ type: 'start_handoff', summary: 'Hand off to Ann: Vendors', payload: {
      worker_name: 'Ann', worker_role: 'Financial Analyst', title: 'Vendors', brief: 'Compare them', budget_usd: 0.5, timeout_minutes: 20 } })
    expect(rows).toEqual([
      { label: 'Worker', value: 'Ann (Financial Analyst)' },
      { label: 'Title', value: 'Vendors' },
      { label: 'Brief', value: 'Compare them', block: true },
      { label: 'Limits', value: 'up to $0.50, up to 20 minutes' },
    ])
    // A card made before the limits were stored shows none
    expect(proposalDetails({ type: 'start_handoff', summary: 's', payload: { worker_name: 'Ann', worker_role: 'Ann', title: 't', brief: 'b' } }))
      .toEqual([{ label: 'Worker', value: 'Ann' }, { label: 'Title', value: 't' }, { label: 'Brief', value: 'b', block: true }])
  })

  it('links a started hand-off to its run', () => {
    expect(resultLink('start_handoff', { handoff_id: 'h1', run_id: 'r9', message_id: 'm2' }, '')).toEqual({ to: '/runs/r9', label: 'Hand-off run' })
    expect(resultLink('start_handoff', {}, '')).toBeNull()
  })
})

describe('tracker and KM2 cards', () => {
  const via = { connection_name: 'Acme Jira', proposed_by: 'Ann' }
  it('says where each change goes, exactly what it posts or saves, and who drafted it', () => {
    expect(proposalDetails({ type: 'tracker_comment', summary: 's', payload: { ...via, issue: 'PRJ-10', text: 'Done.' } })).toEqual([
      { label: 'Connection', value: 'Acme Jira' }, { label: 'Issue', value: 'PRJ-10' }, { label: 'Comment', value: 'Done.', block: true },
      { label: 'Proposed by', value: 'Ann' }])
    expect(proposalDetails({ type: 'tracker_suggestion', summary: 's', payload: { connection_name: 'Acme Jira', issue: 'PRJ-10',
      field: 'due_date', value: '2026-11-01', reason: 'Slipped.' } })).toEqual([
      { label: 'Connection', value: 'Acme Jira' }, { label: 'Issue', value: 'PRJ-10' }, { label: 'Suggested change', value: 'due date: 2026-11-01' },
      { label: 'Reason', value: 'Slipped.', block: true }])
    expect(proposalDetails({ type: 'tracker_issue', summary: 's', payload: { ...via, project: 'PRJ', title: 'Chase', parent: 'PRJ-10',
      description: 'Why' } }).map((r) => r.label)).toEqual(['Connection', 'Project', 'Title', 'Parent', 'Description', 'Proposed by'])
    expect(proposalDetails({ type: 'tracker_issue', summary: 's', payload: { connection_name: 'J', project: 'PRJ', title: 'Chase' } })
      .map((r) => r.label)).toEqual(['Connection', 'Project', 'Title'])
    const km2 = proposalDetails({ type: 'km2_save', summary: 's', payload: { connection_name: 'KM2', title: 'Vendors',
      external_ref: 'agent-chat-vendors', content: '# V' } })
    expect(km2).toContainEqual({ label: 'Folder', value: 'the connection’s default folder' })
    expect(km2).toContainEqual({ label: 'Document id', value: 'agent-chat-vendors (saving again with this id replaces the document)' })
    expect(proposalDetails({ type: 'km2_save', summary: 's', payload: { folder: 'Atlas' } })).toContainEqual({ label: 'Folder', value: 'Atlas' })
  })

  it('links a post to the tracker, only with a web address', () => {
    expect(resultLink('tracker_comment', { issue: 'PRJ-10', url: 'https://x.example/PRJ-10' }, 'c')).toEqual(
      { to: 'https://x.example/PRJ-10', label: 'Posted on PRJ-10', external: true })
    expect(resultLink('tracker_issue', { issue: 'NEW-1', url: 'http://x.example/NEW-1' }, 'c')?.label).toBe('Open NEW-1')
    expect(resultLink('tracker_suggestion', { issue: 'PRJ-10', url: 'javascript:alert(1)' }, 'c')).toBeNull()
    expect(resultLink('tracker_comment', { issue: 'PRJ-10' }, 'c')).toBeNull()
    expect(resultLink('tracker_comment', { issue: 'PRJ-10', url: 'https://x.example/PRJ-10', outcome: 'unknown' }, 'c')?.label).toBe('Open PRJ-10')
    expect(resultLink('km2_save', { document_id: 'd' }, 'c')).toBeNull()
  })
})

describe('studioAdds', () => {
  it('says what Studio adds to each card when it posts or saves it', () => {
    const card = (type: string, proposed_by = 'Ann') => ({ type, summary: 's', payload: { proposed_by } })
    expect(studioAdds(card('tracker_comment'), 'Dana')).toBe('Studio will add: (Posted by Dana from a Crew Studio agent chat; drafted by Ann.)')
    expect(studioAdds(card('tracker_issue', ''), '')).toBe('Studio will add: (Posted by you from a Crew Studio agent chat.)')
    expect(studioAdds(card('tracker_suggestion'), 'Dana')).toMatch(/^Studio will post it as “Suggested change from Dana \(Crew Studio\)/)
    expect(studioAdds(card('km2_save'), 'Dana')).toBe('Studio will add to the document’s details: saved_by Dana, drafted_by Ann.')
    expect(studioAdds(card('km2_save', ''), 'Dana')).toBe('Studio will add to the document’s details: saved_by Dana.')
    expect(studioAdds(card('start_work_order'), 'Dana')).toBeNull()
  })
})

describe('resultNote', () => {
  it('says when a post may or may not have been made', () => {
    expect(resultNote({ outcome: 'unknown', message: 'Check first.' })).toBe('Check first.')
    expect(resultNote({ outcome: 'unknown' })).toBe('It is not known whether the change was made.')
    expect(resultNote({ outcome: 'created' })).toBeNull()
    expect(resultNote(null)).toBeNull()
  })
})

describe('threadOrder', () => {
  const handoff = { id: 'h1', title: 'T', worker_name: 'Ann', worker_role: 'R', deadline_at: '2026-10-07T12:20:00Z', budget_usd: 0.5 }
  it('puts the cards a hand-off proposed just before its result', () => {
    const ask = msg({ id: 'u1', role: 'user' })
    const result = msg({ id: 'r', run_kind: 'handoff', handoff })
    const later = msg({ id: 'a2' })
    const card1 = msg({ id: 'p1', handoff, handoff_proposal: true })
    const card2 = msg({ id: 'p2', handoff, handoff_proposal: true })
    expect(threadOrder([ask, result, later, card1, card2]).map((m) => m.id)).toEqual(['u1', 'p1', 'p2', 'r', 'a2'])
    // Without hand-off cards nothing moves; a card whose result is not loaded stays where it is
    expect(threadOrder([ask, later]).map((m) => m.id)).toEqual(['u1', 'a2'])
    expect(threadOrder([later, card1]).map((m) => m.id)).toEqual(['a2', 'p1'])
  })
})
