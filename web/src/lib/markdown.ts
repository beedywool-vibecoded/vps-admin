// A small Markdown subset for assistant replies: code fences, inline code, bold,
// italics, headings, lists and links. Text is escaped first, so the output only
// contains the tags added here.
const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')

function inline(s: string): string {
  return s
    .replace(/`([^`]+)`/g, '<code>$1</code>')
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
    .replace(/(^|[\s(])\*([^*\s][^*]*)\*/g, '$1<em>$2</em>')
    .replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, '<a href="$2" target="_blank" rel="noopener noreferrer">$1</a>')
}

export function markdown(src: string): string {
  const out: string[] = []
  const lines = esc(src).split('\n')
  let list: 'ul' | 'ol' | null = null
  const closeList = () => {
    if (list) out.push(`</${list}>`)
    list = null
  }
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!
    const fence = line.match(/^```(\w*)\s*$/)
    if (fence) {
      closeList()
      const code: string[] = []
      while (++i < lines.length && !/^```\s*$/.test(lines[i]!)) code.push(lines[i]!)
      out.push(`<pre><code>${code.join('\n')}</code></pre>`)
      continue
    }
    const h = line.match(/^#{1,4}\s+(.*)$/)
    const ul = line.match(/^\s*[-*]\s+(.*)$/)
    const ol = line.match(/^\s*\d+[.)]\s+(.*)$/)
    if (ul || ol) {
      const kind = ul ? 'ul' : 'ol'
      if (list !== kind) {
        closeList()
        out.push(`<${kind}>`)
        list = kind
      }
      out.push(`<li>${inline((ul ?? ol)![1]!)}</li>`)
      continue
    }
    closeList()
    if (h) out.push(`<p><strong>${inline(h[1]!)}</strong></p>`)
    else if (line.trim() === '') out.push('')
    else out.push(`<p>${inline(line)}</p>`)
  }
  closeList()
  return out.join('\n')
}
