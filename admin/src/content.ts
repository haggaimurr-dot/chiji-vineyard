import DOMPurify from 'dompurify'

const maxImageBytes = 4 * 1024 * 1024
const allowedTags = ['p', 'h2', 'h3', 'strong', 'b', 'em', 'i', 'u', 's', 'br', 'blockquote', 'ul', 'ol', 'li', 'a', 'img']

// These styles are authored here, never copied from pasted or stored HTML. The
// mini program's rich-text renderer needs inline styles for article typography.
const articleStyles: Record<string, string> = {
  p: 'margin:0 0 16px;line-height:1.8;word-break:break-word;',
  h2: 'margin:24px 0 12px;font-size:22px;font-weight:700;line-height:1.5;',
  h3: 'margin:20px 0 10px;font-size:18px;font-weight:700;line-height:1.5;',
  strong: 'font-weight:700;',
  b: 'font-weight:700;',
  em: 'font-style:italic;',
  i: 'font-style:italic;',
  u: 'text-decoration:underline;',
  s: 'text-decoration:line-through;',
  blockquote: 'margin:16px 0;padding:10px 16px;border-left:3px solid #8b5c9f;color:#62566b;background:#f8f5fa;',
  ul: 'margin:12px 0;padding-left:24px;list-style-type:disc;line-height:1.8;',
  ol: 'margin:12px 0;padding-left:24px;list-style-type:decimal;line-height:1.8;',
  li: 'margin:6px 0;line-height:1.8;',
  a: 'color:#765187;text-decoration:underline;word-break:break-all;',
  img: 'display:block;max-width:100%;height:auto;margin:16px auto;border-radius:8px;',
}

/** Accept absolute HTTPS URLs and HTTP URLs for local development only. */
export function isSafeContentUrl(value: string, image = false): boolean {
  const url = value.trim()
  if (image && /^data:/i.test(url)) {
    const match = /^data:image\/(?:png|jpeg|webp);base64,([a-z0-9+/]+={0,2})$/i.exec(url)
    if (!match) return false
    const encoded = match[1]!
    if (encoded.length % 4 !== 0) return false
    const bytes = encoded.length / 4 * 3 - (encoded.endsWith('==') ? 2 : encoded.endsWith('=') ? 1 : 0)
    return bytes > 0 && bytes <= maxImageBytes
  }
  if (!/^https?:\/\//i.test(url) || /[\u0000-\u0020\u007f\\]/.test(url)) return false
  try {
    const parsed = new URL(url)
    if (parsed.username || parsed.password) return false
    return parsed.protocol === 'https:' || (parsed.protocol === 'http:' && ['localhost', '127.0.0.1', '[::1]'].includes(parsed.hostname))
  } catch {
    return false
  }
}

/** Sanitize both editor input and preview output with the same small schema. */
export function sanitizeContent(html: string): string {
  if (!html || !DOMPurify.isSupported) return ''
  const fragment = DOMPurify.sanitize(html, {
    ALLOWED_TAGS: allowedTags,
    ALLOWED_ATTR: ['href', 'src', 'alt', 'title', 'start'],
    ALLOW_DATA_ATTR: false,
    ALLOW_ARIA_ATTR: false,
    RETURN_DOM_FRAGMENT: true,
  })
  for (const element of fragment.querySelectorAll<HTMLElement>('*')) {
    const tag = element.tagName.toLowerCase()
    // Attribute allowlists also depend on the element: e.g. an href on an image
    // or src on a paragraph is never retained.
    const permitted = tag === 'a' ? ['href', 'title'] : tag === 'img' ? ['src', 'alt', 'title'] : tag === 'ol' ? ['start'] : []
    for (const attribute of Array.from(element.attributes)) {
      if (!permitted.includes(attribute.name)) element.removeAttribute(attribute.name)
    }
    if (tag === 'img') {
      const src = element.getAttribute('src')?.trim() || ''
      if (!isSafeContentUrl(src, true)) {
        element.remove()
        continue
      }
      element.setAttribute('src', src)
    }
    if (tag === 'a') {
      const href = element.getAttribute('href')?.trim() || ''
      if (!isSafeContentUrl(href)) {
        element.replaceWith(...Array.from(element.childNodes))
        continue
      }
      element.setAttribute('href', href)
      element.setAttribute('target', '_blank')
      element.setAttribute('rel', 'noopener noreferrer')
    }
    if (tag === 'ol' && element.hasAttribute('start')) {
      const start = element.getAttribute('start') || ''
      if (!/^[1-9]\d{0,5}$/.test(start)) element.removeAttribute('start')
    }
    if (articleStyles[tag]) element.setAttribute('style', articleStyles[tag])
  }
  const container = document.createElement('div')
  container.append(fragment)
  return container.innerHTML
}

function escapeHtml(value: string): string {
  return value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;')
}

/** Existing plain-text paragraphs are escaped, so literal HTML stays text. */
export function paragraphsToHtml(content: string[]): string {
  return sanitizeContent(content.map((paragraph) => `<p>${escapeHtml(paragraph).replace(/\r?\n/g, '<br>')}</p>`).join(''))
}

/** Keep a plain-text representation for existing clients and search. */
export function contentParagraphs(html: string): string[] {
  const container = document.createElement('div')
  container.innerHTML = sanitizeContent(html)
  const paragraphs: string[] = []
  let current = ''
  const flush = () => {
    const text = current.replace(/\u00a0/g, ' ').trim()
    if (text) paragraphs.push(text)
    current = ''
  }
  const blockTags = new Set(['P', 'H2', 'H3', 'BLOCKQUOTE', 'UL', 'OL', 'LI'])
  const walk = (node: Node) => {
    if (node.nodeType === Node.TEXT_NODE) {
      current += node.textContent || ''
      return
    }
    if (!(node instanceof HTMLElement)) return
    if (node.tagName === 'BR') {
      current += '\n'
      return
    }
    if (node.tagName === 'IMG') return
    const block = blockTags.has(node.tagName)
    if (block) flush()
    Array.from(node.childNodes).forEach(walk)
    if (block) flush()
  }
  Array.from(container.childNodes).forEach(walk)
  flush()
  return paragraphs
}

export function hasContent(html: string): boolean {
  const container = document.createElement('div')
  container.innerHTML = sanitizeContent(html)
  return Boolean(container.querySelector('img[src]') || container.textContent?.replace(/[\s\u200b-\u200d\ufeff]/g, ''))
}
