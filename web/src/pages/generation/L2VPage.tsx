import MediaUrlVideoPage from './MediaUrlVideoPage'

/**
 * 网页生视频：给一个网页链接，模型读取页面内容并生成一段视频。
 *
 * 页面本体见 MediaUrlVideoPage——它与文件生视频只差字段名和文案。
 */
export default function L2VPage() {
  return (
    <MediaUrlVideoPage
      mode="l2v"
      urlField="linkUrl"
      testId="l2v"
      titleKey="nav.l2v"
      subtitleKey="generation.l2vSubtitle"
      urlLabelKey="generation.l2vLinkLabel"
      urlPlaceholderKey="generation.l2vLinkPlaceholder"
      urlRequiredKey="generation.l2vNeedLink"
    />
  )
}
