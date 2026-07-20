import { describe, it, expect } from 'vitest'
import { render } from '@testing-library/react'

import BrandMark from './BrandMark'
// Vite 的 ?raw 后缀把文件按纯文本导入。用它读 favicon 而不是 node:fs——
// 这个测试属于 app 工程（tsconfig.app.json 只有 vite/client 类型，没有 node），
// 用 fs 会 vitest 能跑但 tsc -b 报错。
import faviconRaw from '../../public/favicon.svg?raw'

/**
 * favicon 与应用内品牌标记必须是同一个造型。
 *
 * 这条测试的由来：换 logo 时只改了 BrandMark.tsx（侧栏与登录页），
 * 漏了 public/favicon.svg —— 那里还留着脚手架自带的紫色闪电。结果标签页上
 * 是闪电、页面里是光圈，用户直接问「这个 logo 没换」。
 *
 * 两者是两份独立的源文件（一份 React 组件、一份静态 SVG，构建期不共享任何
 * 代码），没有机制阻止它们漂移，只能靠断言钉在一起。
 *
 * 断言打在**渲染结果**上而不是源码文本上：BrandMark 的几何是由常量算出来的
 * （dasharray、旋转角都是算式），比对算式的字面量等于在测试里重写一遍算法，
 * 改个常量名就会假红。渲染后拿到的是浏览器真正会用的那组值。
 */
describe('favicon 与 BrandMark 保持同一造型', () => {
  // 必须在每个用例内部渲染：testing-library 的自动 cleanup 会在每个用例后
  // 卸载组件，放在 describe 体里渲染的那份到用例执行时已经被移除了
  // （查询会返回空，表现为一条看起来毫无道理的假红）。
  const renderMark = () => render(<BrandMark />).container

  /**
   * 从 favicon 的 SVG 文本里取某个属性值。
   *
   * 前面那个 (?:^|[\s]) 不是装饰：直接用 `r="..."` 会匹配到 `stop-color="`
   * 的结尾，取回一个颜色值当成半径。
   */
  const faviconAttr = (name: string): string | undefined =>
    new RegExp(`(?:^|\\s)${name}="([^"]+)"`, 'm').exec(faviconRaw)?.[1]

  it('favicon 用的是光圈，不是脚手架残留的闪电', () => {
    expect(faviconRaw).toContain('stroke-dasharray')
    expect(faviconRaw).not.toContain('#863bff') // 旧闪电的紫色
  })

  it('两处渐变色一致', () => {
    const stops = renderMark().querySelectorAll('stop')
    const rendered = Array.from(stops).map((s) => s.getAttribute('stop-color'))
    expect(rendered).toEqual(['#6366f1', '#a855f7'])
    for (const color of rendered) {
      expect(faviconRaw, `favicon 缺少 ${color}`).toContain(color as string)
    }
  })

  // 这条是真正防漂移的一条：BrandMark 里改了 RADIUS / STROKE / VISUAL_GAP
  // 任何一个，算出来的 dasharray 或旋转角就会和 favicon 里写死的字面量对不上。
  it('圆弧几何一致（半径、线宽、缺口补偿、旋转）', () => {
    const arc = renderMark().querySelector('circle[stroke-dasharray]')
    expect(arc).not.toBeNull()
    for (const attr of ['r', 'stroke-width', 'stroke-dasharray', 'transform']) {
      expect(faviconAttr(attr), `favicon 的 ${attr} 与 BrandMark 不一致`)
        .toBe(arc!.getAttribute(attr) ?? undefined)
    }
  })
})
