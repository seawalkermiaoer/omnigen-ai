/**
 * 三个视频生成页面（T2V/I2V/R2V）共用的 resolution/duration/ratio 取值集合与
 * 默认值，逐字对应 server/internal/service/generation_video.go 的
 * videoResolutions/videoDurationMin/Max/videoDefaultDuration/videoRatios/
 * videoDefaultRatio 常量——后端在这些字段上是"有默认值就不算可选"的语义
 * （见该文件 normalizeVideoParams 的注释），前端的初始 state 直接取同样的
 * 默认值，保证一个从未碰过这些控件的用户提交的请求，与后端在同一份输入下
 * 会做的事完全一致。
 *
 * 这几个常量之所以不放进目录（catalog.go 的 CatalogModel），是因为它们不是
 * 按模型变化的——happyhorse 与 wan 视频模型共用同一套 resolution/duration/
 * ratio 取值范围，差异只体现在 Supports（seed/watermark/negative_prompt/
 * prompt_extend）与图片校验阈值上，那些已经由目录驱动。
 */

export const VIDEO_RESOLUTIONS = ['720P', '1080P'] as const

export const DEFAULT_RESOLUTION: string = '720P'

export const VIDEO_DURATION_MIN = 3
export const VIDEO_DURATION_MAX = 15
export const DEFAULT_DURATION = 5

export const VIDEO_RATIOS = ['16:9', '9:16', '3:4', '4:3', '4:5', '5:4', '1:1'] as const

export const DEFAULT_RATIO: string = '16:9'
