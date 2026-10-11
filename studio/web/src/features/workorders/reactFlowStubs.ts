// jsdom lacks the browser APIs React Flow measures with. Call installReactFlowStubs() before rendering a canvas.
export function installReactFlowStubs(): void {
  class ResizeObserverStub {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  class DOMMatrixStub {
    m22 = 1
    constructor(_transform?: string) {}
  }
  const g = globalThis as Record<string, unknown>
  g.ResizeObserver ??= ResizeObserverStub
  g.DOMMatrixReadOnly ??= DOMMatrixStub
  g.DOMMatrix ??= DOMMatrixStub
  // jsdom reports 0 for layout sizes; give nodes a size so React Flow treats them as measured
  Object.defineProperty(HTMLElement.prototype, 'offsetHeight', { configurable: true, get: () => 100 })
  Object.defineProperty(HTMLElement.prototype, 'offsetWidth', { configurable: true, get: () => 200 })
}
