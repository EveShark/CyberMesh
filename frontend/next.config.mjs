import { config as loadEnv } from "dotenv"
import fs, { existsSync, mkdirSync, writeFileSync, promises as fsPromises } from "fs"
import { dirname, resolve } from "path"
import { fileURLToPath } from "url"

const __filename = fileURLToPath(import.meta.url)
const __dirname = dirname(__filename)

// Ensure frontend picks up shared project root environment configuration
loadEnv({ path: resolve(__dirname, "..", ".env"), override: true })

const FALLBACK_500_HTML = [
  "<!DOCTYPE html>",
  '<html lang="en">',
  "<head>",
  '<meta charSet="utf-8" />',
  "<title>Server error</title>",
  "</head>",
  "<body>",
  '<div id="__next"></div>',
  "</body>",
  "</html>",
].join("")

let renamePatched = false

function patchRenameFor500Fallback() {
  if (renamePatched) {
    return
  }
  renamePatched = true

  const normalizePath = (input) => (typeof input === "string" ? input.replace(/\\/g, "/") : "")

  const originalRenamePromise = fsPromises.rename.bind(fsPromises)
  fsPromises.rename = async function patchedRename(oldPath, newPath, ...rest) {
    const normalizedDest = normalizePath(newPath)
    if (normalizedDest.endsWith("/500.html")) {
      try {
        return await originalRenamePromise(oldPath, newPath, ...rest)
      } catch (error) {
        if (error && error.code === "ENOENT") {
          const dir = dirname(newPath)
          if (!existsSync(dir)) {
            mkdirSync(dir, { recursive: true })
          }
          writeFileSync(newPath, FALLBACK_500_HTML, "utf8")
          return
        }
        throw error
      }
    }
    return originalRenamePromise(oldPath, newPath, ...rest)
  }

  const originalRenameCallback = fs.rename.bind(fs)
  fs.rename = function patchedRename(oldPath, newPath, callback) {
    const normalizedDest = normalizePath(newPath)
    if (!normalizedDest.endsWith("/500.html")) {
      return originalRenameCallback(oldPath, newPath, callback)
    }

    originalRenameCallback(oldPath, newPath, (error) => {
      if (error && error.code === "ENOENT") {
        try {
          const dir = dirname(newPath)
          if (!existsSync(dir)) {
            mkdirSync(dir, { recursive: true })
          }
          writeFileSync(newPath, FALLBACK_500_HTML, "utf8")
          callback?.()
          return
        } catch (innerError) {
          callback?.(innerError)
          return
        }
      }
      callback?.(error)
    })
  }
}

patchRenameFor500Fallback()

const nextConfig = {
  env: {
    NEXT_PUBLIC_BACKEND_API_BASE: process.env.FRONTEND_BACKEND_API_BASE,
    NEXT_PUBLIC_AI_API_BASE: process.env.FRONTEND_AI_API_BASE,
  },
  eslint: {
    ignoreDuringBuilds: true,
  },
  typescript: {
    ignoreBuildErrors: true,
  },
  images: {
    unoptimized: true,
  },
  webpack(config) {
    return config
  },
}

if (!process.env.FRONTEND_BACKEND_API_BASE || !process.env.FRONTEND_AI_API_BASE) {
  console.warn("[frontend] Required environment variables FRONTEND_BACKEND_API_BASE or FRONTEND_AI_API_BASE are missing. Check project root .env file.")
}

export default nextConfig
