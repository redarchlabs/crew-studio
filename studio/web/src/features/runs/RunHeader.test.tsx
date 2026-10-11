import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, screen } from '@testing-library/react'
import { BudgetBar } from './BudgetBar'
import { RunHeader } from './RunHeader'
import type { StreamState } from './timeline/useRunTimeline'
import type { Run, RunStatus } from '../../api/types'
import { makeRun, makeSpec, renderAt } from './testUtils'

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

function header(run: Run, over: Partial<{ status: RunStatus; spent: number; budgetState: string | null; stream: StreamState; cancelling: boolean; onCancel: () => void }> = {}) {
  return renderAt(<RunHeader run={run} status={over.status ?? run.status} spent={over.spent ?? 0.25} budgetState={over.budgetState ?? null} stream={over.stream ?? 'closed'} cancelling={over.cancelling ?? false} onCancel={over.onCancel ?? (() => {})} />)
}

describe('RunHeader', () => {
  it('shows the crew name linked to the crew, status, id and a back link', () => {
    header(makeRun({ id: 'run-xyz' }))
    expect(screen.getByText('Research crew').closest('a')?.getAttribute('href')).toBe('/crews/crew-1')
    expect(screen.getByText('succeeded')).toBeTruthy()
    expect(screen.getByText('run-xyz')).toBeTruthy()
    expect(screen.getByText('Runs').closest('a')?.getAttribute('href')).toBe('/runs')
  })

  it('does not link a deleted crew', () => {
    header(makeRun({ crew_id: null, crew_name: 'Gone crew' }))
    expect(screen.getByText('Gone crew').closest('a')).toBeNull()
  })

  it.each<[RunStatus, boolean]>([['running', true], ['queued', true], ['succeeded', false], ['failed', false], ['cancelled', false]])('cancel button for %s: %s', (status, shown) => {
    header(makeRun({ status }), { status })
    expect(!!screen.queryByText('Cancel run')).toBe(shown)
  })

  it('invokes onCancel and disables the button while cancelling', () => {
    const onCancel = vi.fn()
    const { unmount } = header(makeRun({ status: 'running' }), { onCancel })
    fireEvent.click(screen.getByText('Cancel run'))
    expect(onCancel).toHaveBeenCalledTimes(1)
    unmount()
    header(makeRun({ status: 'running' }), { onCancel, cancelling: true })
    expect((screen.getByText('Cancel run').closest('button') as HTMLButtonElement).disabled).toBe(true)
  })

  it('prefers the live status over the stored one', () => {
    header(makeRun({ status: 'queued' }), { status: 'running' })
    expect(screen.getByText('running')).toBeTruthy()
    expect(screen.queryByText('queued')).toBeNull()
  })

  it.each<[StreamState, string | null]>([
    ['idle', null], ['loading', 'loading events'], ['connecting', 'connecting'], ['live', 'live'], ['reconnecting', 'reconnecting…'], ['closed', null],
  ])('stream label for %s', (stream, label) => {
    header(makeRun({ status: 'running' }), { stream })
    if (label) expect(screen.getByText(label)).toBeTruthy()
    else expect(screen.queryByText(/live|connecting|loading events|reconnecting/)).toBeNull()
  })

  it('shows tokens and request count when usage is known', () => {
    header(makeRun({ usage: { prompt_tokens: 1000, completion_tokens: 234, cached_prompt_tokens: 0, successful_requests: 12 } }))
    expect(screen.getByText('1,234')).toBeTruthy()
    expect(screen.getByText('12 requests')).toBeTruthy()
  })

  it('shows a pending note when usage is not available yet', () => {
    header(makeRun({ usage: null }))
    expect(screen.getByText('final usage pending')).toBeTruthy()
  })

  it('shows process and counts from the spec, or dashes without one', () => {
    header(makeRun({ spec: makeSpec() }))
    expect(screen.getByText('sequential')).toBeTruthy()
    expect(screen.getByText('1 agents · 1 tasks')).toBeTruthy()
    cleanup()
    header(makeRun({ spec: undefined }))
    expect(screen.getByText('Process').nextElementSibling?.textContent).toBe('—')
  })

  it('shows the finished duration for a completed run', () => {
    header(makeRun({ started_at: '2026-01-01T00:00:00Z', finished_at: '2026-01-01T00:01:05Z' }))
    expect(screen.getByText('1m 5s')).toBeTruthy()
  })

  it('ticks the duration while the run is active', () => {
    vi.useFakeTimers()
    vi.setSystemTime(Date.parse('2026-01-01T00:00:10Z'))
    header(makeRun({ status: 'running', started_at: '2026-01-01T00:00:00Z', finished_at: null }), { status: 'running' })
    expect(screen.getByText('10s')).toBeTruthy()
    act(() => { vi.advanceTimersByTime(5000) })
    expect(screen.getByText('15s')).toBeTruthy()
  })

  it('has no duration for a queued run that has not started', () => {
    header(makeRun({ status: 'queued', started_at: null, finished_at: null }), { status: 'queued' })
    expect(screen.getByText('Duration').nextElementSibling?.textContent).toBe('—')
  })

  it('lists inputs, showing empty values as quotes', () => {
    header(makeRun({ inputs: { topic: 'cats', empty: '' } }))
    expect(screen.getByText('topic')).toBeTruthy()
    expect(screen.getByText('cats')).toBeTruthy()
    expect(screen.getByText('""')).toBeTruthy()
  })

  it('shows the error unless the run is still running', () => {
    header(makeRun({ status: 'failed', error: 'kaboom' }))
    expect(screen.getByText('kaboom')).toBeTruthy()
    cleanup()
    header(makeRun({ status: 'running', error: 'transient' }), { status: 'running' })
    expect(screen.queryByText('transient')).toBeNull()
  })

  it('shows spend against budget', () => {
    header(makeRun({ budget_usd: 2 }), { spent: 1.5 })
    expect(screen.getByText('$1.50')).toBeTruthy()
    expect(screen.getByText('/ $2.00')).toBeTruthy()
  })
})

describe('BudgetBar', () => {
  const bar = () => screen.getByRole('progressbar')
  const fill = () => bar().firstElementChild as HTMLElement

  it('is green under 80% of budget', () => {
    renderAt(<BudgetBar spent={0.5} budget={1} state={null} />)
    expect(fill().className).toContain('bg-ok')
    expect(fill().style.width).toBe('50%')
    expect(bar().getAttribute('aria-valuenow')).toBe('0.5')
    expect(bar().getAttribute('aria-valuemax')).toBe('1')
  })

  it('turns warning at 80% and danger at 100%', () => {
    renderAt(<BudgetBar spent={0.8} budget={1} state={null} />)
    expect(fill().className).toContain('bg-warn')
    cleanup()
    renderAt(<BudgetBar spent={1} budget={1} state={null} />)
    expect(fill().className).toContain('bg-danger')
  })

  it('extends the track to the hard stop and marks the budget', () => {
    renderAt(<BudgetBar spent={1} budget={1} hardStopMultiplier={2} state={null} />)
    expect(fill().style.width).toBe('50%')
    expect(screen.getByTitle('Budget').getAttribute('style')).toContain('left: 50%')
    expect(screen.getByText('hard stop $2.00')).toBeTruthy()
  })

  it('treats a multiplier under 1 as 1', () => {
    renderAt(<BudgetBar spent={0.5} budget={1} hardStopMultiplier={0.5} state={null} />)
    expect(screen.queryByText(/hard stop \$/)).toBeNull()
    expect(fill().style.width).toBe('50%')
  })

  it('caps the fill at 100% when overspent', () => {
    renderAt(<BudgetBar spent={5} budget={1} hardStopMultiplier={2} state={null} />)
    expect(fill().style.width).toBe('100%')
  })

  it('handles a zero budget without dividing by zero', () => {
    renderAt(<BudgetBar spent={1} budget={0} state={null} />)
    expect(fill().style.width).toBe('0%')
    expect(fill().className).toContain('bg-ok')
  })

  it('labels the budget state', () => {
    renderAt(<BudgetBar spent={1} budget={1} state="hard_stopped" />)
    expect(screen.getByText('hard stop')).toBeTruthy()
    cleanup()
    renderAt(<BudgetBar spent={1} budget={1} state="searches_stopped" />)
    expect(screen.getByText('searches stopped')).toBeTruthy()
  })
})
