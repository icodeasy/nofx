import { useState, useCallback } from 'react'
import { createRoot } from 'react-dom/client'
import { Root } from 'react-dom/client'
import { MarkdownPreviewModal } from '../components/MarkdownPreviewModal'

let modalRoot: HTMLElement | null = null
let root: Root | null = null

// Initialize modal container on first use
const getModalRoot = () => {
  if (!modalRoot) {
    modalRoot = document.createElement('div')
    modalRoot.id = 'markdown-modal-root'
    document.body.appendChild(modalRoot)
    root = createRoot(modalRoot)
  }
  return root
}

export const useMarkdownModal = () => {
  const [isOpen, setIsOpen] = useState(false)
  const [content, setContent] = useState('')
  const [title, setTitle] = useState('')

  const openMarkdownModal = useCallback((markdownContent: string, modalTitle: string) => {
    setContent(markdownContent)
    setTitle(modalTitle)
    setIsOpen(true)
  }, [])

  const closeMarkdownModal = useCallback(() => {
    setIsOpen(false)
  }, [])

  return {
    openMarkdownModal,
    closeMarkdownModal,
    isOpen,
    content,
    title,
  }
}

/**
 * Global function to open markdown modal from anywhere
 * Similar to the original openMarkdownInNewTab function
 */
export function openMarkdownModal(content: string, title: string): void {
  const currentRoot = getModalRoot()
  const close = () => {
    if (currentRoot) {
      currentRoot.unmount()
    }
    if (modalRoot && modalRoot.parentNode) {
      modalRoot.parentNode.removeChild(modalRoot)
    }
    modalRoot = null
    root = null
  }

  if (currentRoot) {
    currentRoot.render(
      <MarkdownPreviewModal content={content} title={title} onClose={close} />
    )
  }
}
