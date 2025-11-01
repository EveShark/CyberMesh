import type { ReactNode } from "react"
import dynamic from "next/dynamic"

import { RootFrame } from "@/components/root-frame"

const ClientProviders = dynamic(() => import("./client-providers"), {
  ssr: false,
})

type ProvidersProps = {
  children: ReactNode
}

export function Providers({ children }: ProvidersProps) {
  return (
    <>
      <RootFrame />
      <ClientProviders>{children}</ClientProviders>
    </>
  )
}
