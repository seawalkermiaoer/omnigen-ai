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

/** 圆环半径与线宽，下面的 dasharray / 旋转角都由它们算出来。 */
const RADIUS = 6.25
const STROKE = 2.25

/**
 * 缺口**看上去**该有多宽。
 *
 * 这里必须区分「名义缺口」和「视觉缺口」，第一版就栽在这上面：
 * strokeLinecap="round" 会让每段弧的两端各多伸出 STROKE/2，于是名义 4 单位
 * 的缺口实际只剩 4 − 2.25 = 1.75 单位。26px 下折合约 1.6 物理像素，
 * 在非 Retina 屏上被抗锯齿一抹就连上了——渲染出来是一个**闭合的白环**加
 * 中心点，读作「唱片 / 录制按钮 / 靶心」，光圈的语义完全消失。
 * （实测确认，而不是推测：侧栏收起态 26px 在 1x 渲染下就已经不成立。）
 *
 * 所以 dasharray 里要填的是 VISUAL_GAP + STROKE，把圆头吃掉的那部分补回去。
 */
const VISUAL_GAP = 4
const GAP = VISUAL_GAP + STROKE

// 三段等长圆弧 + 三道等宽缺口。周长 2πr ≈ 39.27，一个「弧+缺口」的周期是
// 它的三分之一。写成算式而不是硬编码 magic number，改半径/线宽时会自己对上。
const CIRCUMFERENCE = 2 * Math.PI * RADIUS

/**
 * 保留两位小数。
 *
 * 直接把算式结果塞进 SVG 属性会渲染成 `6.8399693899574725` 这样的 16 位浮点：
 * 属性值是噪音，而且和 public/favicon.svg 里写死的字面量对不上——两者本该
 * 是同一个造型，却因为精度差异无法逐字段比对（BrandMark.test.tsx 正是靠
 * 逐字段比对来防止两处漂移的）。两位小数在 28 单位的坐标系里远超所需精度。
 */
const round2 = (n: number) => Number(n.toFixed(2))

const DASH = round2(CIRCUMFERENCE / 3 - GAP)

/**
 * 让**一段弧的中心**落在 12 点方向。
 *
 * dasharray 从路径起点（3 点方向）开始铺，转 -90° 只是把起点挪到 12 点，
 * 于是 12 点正好卡在一段弧的**开头**而不是中间——三个缺口落在不对称的位置，
 * 静态看上去像是画歪了。再回转半段弧，图形就对竖直轴左右镜像对称。
 */
const ROTATION = round2(-90 - ((DASH / CIRCUMFERENCE) * 360) / 2)

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
        {/* 底板比第一版压暗一档（#818cf8/#c084fc → #6366f1/#a855f7）。
            不是审美偏好：原来白弧对底板的对比度只有 2.98:1，白色几乎陷进
            底色里，是小尺寸下缺口糊掉的另一半根因。压暗后升到 4.5:1 上下，
            弧和缺口同时变利落。 */}
        <linearGradient id="omnigen-brand-mark" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0%" stopColor="#6366f1" />
          <stop offset="100%" stopColor="#a855f7" />
        </linearGradient>
      </defs>

      <rect width="28" height="28" rx="8.5" fill="url(#omnigen-brand-mark)" />

      {/* 三段圆弧构成光圈，旋转量见 ROTATION。 */}
      <circle
        cx="14"
        cy="14"
        r={RADIUS}
        stroke="#fff"
        strokeWidth={STROKE}
        strokeLinecap="round"
        strokeDasharray={`${DASH} ${GAP}`}
        transform={`rotate(${ROTATION} 14 14)`}
      />
      {/* r 从 1.9 提到 2.4：原来中心点比环还弱，整体读作「空心环」而不是
          「有焦点的镜头」，小尺寸下更是第一个消失的元素。 */}
      <circle cx="14" cy="14" r="2.4" fill="#fff" />
    </svg>
  )
}
