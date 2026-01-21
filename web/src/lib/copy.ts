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
