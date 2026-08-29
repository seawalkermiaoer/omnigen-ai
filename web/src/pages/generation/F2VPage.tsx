import MediaUrlVideoPage from './MediaUrlVideoPage'

/**
 * 文件生视频：给一份文档（docx/doc/xlsx/xls/pptx/ppt/pdf/txt/key/pages/
 * numbers/md，≤100MB、≤50 页）的 URL，模型把它讲成一段视频。
 *
 * 页面本体见 MediaUrlVideoPage——它与网页生视频只差字段名和文案。
 */
export default function F2VPage() {
  return (
    <MediaUrlVideoPage
      mode="f2v"
      urlField="fileUrl"
      testId="f2v"
      titleKey="nav.f2v"
      subtitleKey="generation.f2vSubtitle"
      urlLabelKey="generation.f2vFileLabel"
      urlPlaceholderKey="generation.f2vFilePlaceholder"
      urlRequiredKey="generation.f2vNeedFile"
    />
  )
}
