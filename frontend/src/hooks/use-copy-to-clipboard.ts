"use client"

import { useCallback, useState } from "react"

export function useCopyToClipboard() {
  const [isCopied, setIsCopied] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const copy = useCallback(async (text: string) => {
    setError(null)

    const fallbackCopy = () => {
      if (typeof document === "undefined") {
        throw new Error("Clipboard API not available in this environment")
      }

      const textArea = document.createElement("textarea")
      textArea.value = text
      textArea.setAttribute("readonly", "")
      textArea.style.position = "absolute"
      textArea.style.left = "-9999px"
      document.body.appendChild(textArea)

      const selection = document.getSelection()
      const selected = selection ? selection.rangeCount > 0 ? selection.getRangeAt(0) : null : null

      textArea.select()
      const successful = document.execCommand("copy")
      document.body.removeChild(textArea)

      if (selected && selection) {
        selection.removeAllRanges()
        selection.addRange(selected)
      }

      if (!successful) {
        throw new Error("Copy command was not successful")
      }
    }

    try {
      if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text)
      } else {
        fallbackCopy()
      }
      setIsCopied(true)
      setTimeout(() => setIsCopied(false), 2000)
      return true
    } catch (err) {
      try {
        fallbackCopy()
        setIsCopied(true)
        setTimeout(() => setIsCopied(false), 2000)
        return true
      } catch (fallbackError) {
        const message = fallbackError instanceof Error ? fallbackError.message : "Failed to copy"
        console.error("Failed to copy:", err ?? fallbackError)
        setIsCopied(false)
        setError(message)
        return false
      }
    }
  }, [])

  return { copy, isCopied, error }
}
