/**
 * OmniGen AI 的品牌标记。
 *
 * 造型是一枚**光圈**（相机快门 / 镜头）：一个留着三道缺口的圆环加中心点。
 * 选它的理由：
 *
 *   - 语义直接命中产品——这是一个出图、出视频的控制台，光圈是「成像」最
 *     通用的符号，不需要任何解释。
 *   - 它同时也是 OmniGen 的首字母 O。
 *   - 在 16px 下依然读得出来。这是硬约束：侧栏收起时这枚标记就是全部品牌
 *     信息，糊掉就等于没有。真实光圈的六片叶子在这个尺寸下会糊成一团，
 *     所以简化成三段圆弧——形还在，笔画少一半。
 *
 * 刻意避开了四角星「✦」：它是 AI 产品最泛滥的符号，用了就和其它所有产品
 * 长得一样，起不到标记的作用。
 *
 * 渐变色是装饰性品牌色，不走 theme/index.ts 那套 UI 主色 token（与原先
 * .shell-logo__mark / .login-brand__mark 的约定一致，见两处 CSS 文件头注释）。
 */

/** 圆环半径与线宽，dasharray 依赖它们算周长，改动时要一起改。 */
const RADIUS = 6.25
const STROKE = 2.25

// 三段等长圆弧 + 三道等宽缺口。周长 2πr ≈ 39.27，一个「弧+缺口」的周期就是
// 它的三分之一（≈13.09）。写成算式而不是硬编码 magic number，改半径时
// dasharray 会自己跟着对。
const CIRCUMFERENCE = 2 * Math.PI * RADIUS
const GAP = 4
const DASH = CIRCUMFERENCE / 3 - GAP

interface BrandMarkProps {
  /** 边长（px）。默认 28，侧栏收起时用 26，登录页用 32。 */
  size?: number
}

export default function BrandMark({ size = 28 }: BrandMarkProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 28 28"
      fill="none"
      // 品牌标记是装饰，旁边永远跟着「OmniGen AI」文字；重复念一遍对读屏
      // 用户只是噪音。
      aria-hidden
      focusable="false"
      style={{ flexShrink: 0, display: 'block' }}
    >
      <defs>
        <linearGradient id="omnigen-brand-mark" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0%" stopColor="#818cf8" />
          <stop offset="100%" stopColor="#c084fc" />
        </linearGradient>
      </defs>

      <rect width="28" height="28" rx="8.5" fill="url(#omnigen-brand-mark)" />

      {/* 三段圆弧构成光圈。旋转 -90° 让缺口落在 12 点方向起算，视觉上对称。 */}
      <circle
        cx="14"
        cy="14"
        r={RADIUS}
        stroke="#fff"
        strokeWidth={STROKE}
        strokeLinecap="round"
        strokeDasharray={`${DASH} ${GAP}`}
        transform="rotate(-90 14 14)"
      />
      <circle cx="14" cy="14" r="1.9" fill="#fff" />
    </svg>
  )
}
