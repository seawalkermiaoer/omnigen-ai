import '@testing-library/jest-dom/vitest'

// antd 的 Select/Dropdown/Switch 等组件依赖 ResizeObserver 做尺寸探测，
// jsdom 未实现——不打桩会在渲染阶段直接抛 ReferenceError，与业务逻辑无关。
class MockResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}
Object.defineProperty(window, 'ResizeObserver', {
  writable: true,
  value: MockResizeObserver,
})

// antd 的响应式组件依赖 matchMedia，jsdom 未实现，需打桩。
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }),
})
