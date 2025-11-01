import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"

import { ModeToggle } from "../mode-toggle"
import { ModeProvider } from "@/lib/mode-context"

describe("ModeToggle", () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it("updates the active mode and persists the selection", async () => {
    const user = userEvent.setup()

    render(
      <ModeProvider initialMode="investor">
        <ModeToggle />
      </ModeProvider>
    )

    const trigger = screen.getByTestId("mode-toggle-trigger")
    expect(trigger).toHaveTextContent("Investor Mode")

    await user.click(trigger)

    const demoOption = await screen.findByTestId("mode-option-demo")
    await user.click(demoOption)

    await waitFor(() => expect(trigger).toHaveTextContent("Demo Mode"))
    expect(localStorage.getItem("dashboardMode")).toBe("demo")
  })
})
