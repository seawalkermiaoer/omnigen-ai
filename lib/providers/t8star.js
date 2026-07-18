/**
 * t8star (gpt-image-2) provider — OpenAI chat-completions protocol.
 *
 * Pure functions only: no network IO, so they can be unit tested offline.
 * The upstream returns the generated image as a markdown link embedded in the
 * assistant's reply text, followed by prose commentary.
 */

const T8STAR_DEFAULT_BASE = 'https://ai.t8star.org';

/** Matches a markdown image link and captures its URL. */
function imageLinkRegex() {
  // Built fresh on every call: a shared /g regex carries lastIndex between calls.
  return /!\[[^\]]*\]\((https?:\/\/[^)\s]+)\)/g;
}

/**
 * Builds the chat-completions request body.
 * With no images the content is a plain string; with images it is a block array.
 */
function buildPayload({ model, prompt, images }) {
  const list = Array.isArray(images) ? images.filter(Boolean) : [];
  let content;

  if (list.length === 0) {
    content = prompt || '';
  } else {
    content = [];
    if (prompt) content.push({ type: 'text', text: prompt });
    for (const url of list) {
      content.push({ type: 'image_url', image_url: { url } });
    }
  }

  return { model, stream: false, messages: [{ role: 'user', content }] };
}

/**
 * Extracts image URLs and the leftover prose from the assistant reply.
 * Returns { images: string[], note: string }.
 */
function parseResponse(data) {
  const content = data?.choices?.[0]?.message?.content;
  if (typeof content !== 'string') return { images: [], note: '' };

  const images = [];
  const re = imageLinkRegex();
  let m;
  while ((m = re.exec(content)) !== null) images.push(m[1]);

  const note = content.replace(imageLinkRegex(), '').trim();
  return { images, note };
}

/** Normalizes a user-supplied base URL, falling back to the official host. */
function resolveBaseUrl(input) {
  const v = (input || '').trim();
  if (!/^https?:\/\//i.test(v)) return T8STAR_DEFAULT_BASE;
  return v.replace(/\/+$/, '');
}

module.exports = { buildPayload, parseResponse, resolveBaseUrl, T8STAR_DEFAULT_BASE };
