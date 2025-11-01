import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"

import { SidebarNav } from "../sidebar-nav"
import { NAVIGATION_ITEMS } from "@/config/navigation"
import { usePathname } from "next/navigation"
import { useMode } from "@/lib/mode-context"

jest.mock("next/navigation", () => ({
  usePathname: jest.fn(),
}))

jest.mock("@/lib/mode-context", () => ({
  useMode: jest.fn(),
}))

const mockedUsePathname = usePathname as jest.MockedFunction<typeof usePathname>
const mockedUseMode = useMode as jest.MockedFunction<typeof useMode>

const originalEnv = process.env.NEXT_PUBLIC_P2P_STYLE

afterAll(() => {
  process.env.NEXT_PUBLIC_P2P_STYLE = originalEnv
})

beforeEach(() => {
  process.env.NEXT_PUBLIC_P2P_STYLE = "true"
  mockedUsePathname.mockReset()
  mockedUseMode.mockReset()
})

describe("SidebarNav", () => {
  it("renders navigation items allowed for current mode", () => {
    mockedUseMode.mockReturnValue({ mode: "demo", setMode: jest.fn() })
    mockedUsePathname.mockReturnValue("/ai-engine")

    render(<SidebarNav />)

    const expectedItems = NAVIGATION_ITEMS.filter((item) => item.modes.includes("demo"))
    expectedItems.forEach((item) => {
      expect(screen.getByTestId(`nav-item-${item.id}`)).toBeInTheDocument()
    })

    const excludedItems = NAVIGATION_ITEMS.filter((item) => !item.modes.includes("demo"))
    excludedItems.forEach((item) => {
      expect(screen.queryByTestId(`nav-item-${item.id}`)).not.toBeInTheDocument()
    })
  })

  it("highlights the active navigation item", () => {
    mockedUseMode.mockReturnValue({ mode: "demo", setMode: jest.fn() })
    mockedUsePathname.mockReturnValue("/threats")

    render(<SidebarNav />)

    expect(screen.getByTestId("nav-active-indicator-threats")).toBeInTheDocument()
  })

  it("collapses the sidebar and hides labels", async () => {
    const user = userEvent.setup()
    mockedUseMode.mockReturnValue({ mode: "operator", setMode: jest.fn() })
    mockedUsePathname.mockReturnValue("/")

    render(<SidebarNav />)

    const toggle = screen.getByTestId("sidebar-collapse-toggle")
    await user.click(toggle)

    expect(screen.getByTestId("sidebar-root")).toHaveClass("w-20")
    expect(screen.queryByText("Dashboard")).not.toBeInTheDocument()
  })

  it("renders fallback navigation when P2P style disabled", () => {
    process.env.NEXT_PUBLIC_P2P_STYLE = "false"
    mockedUseMode.mockReturnValue({ mode: "investor", setMode: jest.fn() })
    mockedUsePathname.mockReturnValue("/")

    render(<SidebarNav />)

    expect(screen.queryByTestId("sidebar-collapse-toggle")).not.toBeInTheDocument()
    const expectedItems = NAVIGATION_ITEMS.filter((item) => item.modes.includes("investor"))
    expectedItems.forEach((item) => {
      expect(screen.getByTestId(`nav-item-${item.id}`)).toBeInTheDocument()
    })
  })
})
