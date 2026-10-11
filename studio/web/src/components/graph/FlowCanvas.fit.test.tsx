import { render } from '@testing-library/react'
import { act } from 'react'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'

const fitView = vi.fn()
let initialized = false

vi.mock('@xyflow/react', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@xyflow/react')>()
  return {
    ...actual,
    useNodesInitialized: () => initialized,
    useReactFlow: () => ({ ...actual.useReactFlow(), fitView }),
  }
})

import { FlowCanvas } from './FlowCanvas'
import type { EntityFlowNode } from './EntityNode'
import { autoCleanup } from '../../lib/testUtils'

autoCleanup()
beforeAll(() => {
  class RO {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  ;(globalThis as unknown as { ResizeObserver: typeof RO }).ResizeObserver = RO
})
afterEach(() => {
  vi.useRealTimers()
  fitView.mockClear()
  initialized = false
})

const n = (id: string): EntityFlowNode => ({ id, type: 'entity', position: { x: 0, y: 0 }, data: { kind: 'agent', label: id } })

describe('FlowCanvas auto-fit', () => {
  it('does not fit before the nodes are measured', () => {
    vi.useFakeTimers()
    render(<FlowCanvas nodes={[n('a')]} edges={[]} />)
    act(() => {
      vi.advanceTimersByTime(100)
    })
    expect(fitView).not.toHaveBeenCalled()
  })

  it('fits the viewport shortly after measurement, and again when the node set changes', () => {
    vi.useFakeTimers()
    initialized = true
    const { rerender } = render(<FlowCanvas nodes={[n('a')]} edges={[]} />)
    act(() => {
      vi.advanceTimersByTime(30)
    })
    expect(fitView).toHaveBeenCalledTimes(1)
    expect(fitView).toHaveBeenCalledWith({ padding: 0.12, maxZoom: 1.1, duration: 200 })
    rerender(<FlowCanvas nodes={[n('a'), n('b')]} edges={[]} />)
    act(() => {
      vi.advanceTimersByTime(30)
    })
    expect(fitView).toHaveBeenCalledTimes(2)
  })

  it('does not refit when only unrelated props change (same node ids)', () => {
    vi.useFakeTimers()
    initialized = true
    const { rerender } = render(<FlowCanvas nodes={[n('a')]} edges={[]} />)
    act(() => {
      vi.advanceTimersByTime(30)
    })
    rerender(<FlowCanvas nodes={[n('a')]} edges={[]} className="h-10" />)
    act(() => {
      vi.advanceTimersByTime(30)
    })
    expect(fitView).toHaveBeenCalledTimes(1)
  })

  it('cancels a pending fit on unmount', () => {
    vi.useFakeTimers()
    initialized = true
    const { unmount } = render(<FlowCanvas nodes={[n('a')]} edges={[]} />)
    unmount()
    act(() => {
      vi.advanceTimersByTime(100)
    })
    expect(fitView).not.toHaveBeenCalled()
  })
})
