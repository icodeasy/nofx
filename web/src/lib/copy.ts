/**
 * Robust copy-to-clipboard utility that works across all browsers and contexts
 * Handles both modern Clipboard API and fallback methods
 */

/**
 * Copy text to clipboard with multiple fallback methods
 * @param text - The text to copy
 * @returns Promise that resolves if successful, rejects if all methods fail
 */
export async function copyToClipboard(text: string): Promise<void> {
  // Method 1: Try modern Clipboard API (requires secure context: HTTPS or localhost)
  if (navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(text)
      return
    } catch (err) {
      console.warn('Clipboard API failed, trying fallback:', err)
      // Don't throw yet, try fallback methods
    }
  }

  // Method 2: Fallback using document.execCommand('copy')
  // Works in older browsers and non-secure contexts
  try {
    const textArea = document.createElement('textarea')
    textArea.value = text

    // Ensure textarea is not visible but still in DOM
    textArea.style.position = 'fixed'
    textArea.style.left = '-999999px'
    textArea.style.top = '-999999px'
    textArea.style.opacity = '0'
    textArea.setAttribute('readonly', '')

    document.body.appendChild(textArea)

    // iOS/iPadOS specific handling
    if (navigator.userAgent.match(/ipad|iphone/i)) {
      const range = document.createRange()
      range.selectNodeContents(textArea)
      const selection = window.getSelection()
      if (selection) {
        selection.removeAllRanges()
        selection.addRange(range)
      }
      textArea.setSelectionRange(0, text.length)
    } else {
      textArea.select()
    }

    // Execute copy command
    const successful = document.execCommand('copy')
    document.body.removeChild(textArea)

    if (successful) {
      return
    } else {
      throw new Error('execCommand("copy") returned false')
    }
  } catch (err) {
    console.error('All copy methods failed:', err)
    throw new Error('Failed to copy to clipboard. Please select and copy manually.')
  }
}

/**
 * Check if copy to clipboard is supported in current browser
 */
export function isCopySupported(): boolean {
  return !!(navigator.clipboard && window.isSecureContext) || document.queryCommandSupported?.('copy')
}

/**
 * Open content in a new browser tab/window with formatted display
 * @param content - The text content to display
 * @param title - The title for the new window/tab
 */
export function openInNewTab(content: string, title: string): void {
  const newWindow = window.open('', '_blank')
  if (newWindow) {
    newWindow.document.write(`
      <!DOCTYPE html>
      <html>
      <head>
        <title>${title}</title>
        <style>
          body {
            font-family: 'Monaco', 'Menlo', 'Ubuntu Mono', monospace;
            background: #0B0E11;
            color: #EAECEF;
            padding: 20px;
            margin: 0;
            white-space: pre-wrap;
            word-wrap: break-word;
            line-height: 1.6;
          }
        </style>
      </head>
      <body>${content.replace(/</g, '&lt;').replace(/>/g, '&gt;')}</body>
      </html>
    `)
    newWindow.document.close()
  }
}

/**
 * Open markdown content in a new browser tab/window with markdown rendering
 * @param content - The markdown content to display
 * @param title - The title for the new window/tab
 */
export function openMarkdownInNewTab(content: string, title: string): void {
  const newWindow = window.open('', '_blank')
  if (newWindow) {
    newWindow.document.write(`
      <!DOCTYPE html>
      <html>
      <head>
        <title>${title}</title>
        <meta charset="utf-8"/>
        <meta name="viewport" content="width=device-width, initial-scale=1">
        <script src="https://cdn.jsdelivr.net/npm/marked/marked.min.js"><\/script>
        <style>
          body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Helvetica Neue', Arial, sans-serif;
            background: #0B0E11;
            color: #EAECEF;
            padding: 20px;
            margin: 0;
            line-height: 1.6;
            max-width: 900px;
            margin: 0 auto;
          }
          /* Markdown styles */
          h1, h2, h3, h4, h5, h6 {
            color: #EAECEF;
            margin-top: 24px;
            margin-bottom: 16px;
            font-weight: 600;
            line-height: 1.25;
          }
          h1 { font-size: 2em; border-bottom: 1px solid #333; padding-bottom: 0.3em; }
          h2 { font-size: 1.5em; border-bottom: 1px solid #333; padding-bottom: 0.3em; }
          h3 { font-size: 1.25em; }
          code {
            background: #1a1d21;
            padding: 0.2em 0.4em;
            border-radius: 3px;
            font-family: 'Monaco', 'Menlo', 'Ubuntu Mono', monospace;
            font-size: 0.9em;
            color: #a855f7;
          }
          pre {
            background: #1a1d21;
            padding: 16px;
            border-radius: 6px;
            overflow-x: auto;
            border: 1px solid #333;
            margin: 16px 0;
          }
          pre code {
            background: transparent;
            padding: 0;
            border-radius: 0;
            font-size: 0.9em;
            color: #EAECEF;
          }
          blockquote {
            border-left: 4px solid #a855f7;
            padding-left: 16px;
            color: #a0aec0;
            margin: 16px 0;
          }
          ul, ol {
            padding-left: 2em;
            margin: 16px 0;
          }
          li {
            margin: 4px 0;
          }
          table {
            border-collapse: collapse;
            width: 100%;
            margin: 16px 0;
          }
          th, td {
            border: 1px solid #333;
            padding: 8px 12px;
            text-align: left;
          }
          th {
            background: #1a1d21;
            font-weight: 600;
          }
          a {
            color: #a855f7;
            text-decoration: none;
          }
          a:hover {
            text-decoration: underline;
          }
          img {
            max-width: 100%;
            height: auto;
          }
          hr {
            border: none;
            border-top: 1px solid #333;
            margin: 24px 0;
          }
          strong {
            color: #EAECEF;
            font-weight: 600;
          }
          em {
            color: #a0aec0;
          }
        </style>
      </head>
      <body>
        <div id="content"></div>
        <script>
          // Configure marked options
          marked.setOptions({
            breaks: true,
            gfm: true
          });
          document.getElementById('content').innerHTML = marked.parse(${JSON.stringify(content)});
        <\/script>
      </body>
      </html>
    `)
    newWindow.document.close()
  }
}
