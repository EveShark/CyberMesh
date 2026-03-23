package main

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type controlModel struct {
	ctx           context.Context
	statusFetch   controlStatusFetcher
	toggleFetch   controlToggleFetcher
	readOnly      bool
	title         string
	autoExit      bool
	loading       bool
	data          controlStatusResponse
	selected      int
	err           error
	pendingKind   string
	pendingEnable bool
	pendingDraft  *actionDraft
	statusMessage string
	showHelp      bool
	scrollOffset  int
	width         int
	height        int
}

func newControlModel(ctx context.Context, statusFetch controlStatusFetcher, toggleFetch controlToggleFetcher) tea.Model {
	return newControlModelWithMode(ctx, statusFetch, toggleFetch, false, "CyberMesh Control")
}

func newControlReadOnlyModel(ctx context.Context, statusFetch controlStatusFetcher) tea.Model {
	return newControlModelWithMode(ctx, statusFetch, nil, true, "CyberMesh Leases")
}

func newControlModelWithMode(ctx context.Context, statusFetch controlStatusFetcher, toggleFetch controlToggleFetcher, readOnly bool, title string) tea.Model {
	width, height := initialTerminalSize()
	return &controlModel{
		ctx:         ctx,
		statusFetch: statusFetch,
		toggleFetch: toggleFetch,
		readOnly:    readOnly,
		title:       title,
		autoExit:    tuiAutoExitEnabled(),
		loading:     true,
		width:       width,
		height:      height,
	}
}

func (m *controlModel) Init() tea.Cmd { return asyncControlLoad(m.ctx, m.statusFetch) }

func (m *controlModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		if m.showHelp {
			switch msg.String() {
			case "?", "esc":
				m.showHelp = false
			}
			return m, nil
		}
		if m.pendingDraft != nil {
			if m.pendingDraft.handleKey(msg) {
				return m, nil
			}
			switch msg.String() {
			case "esc", "n":
				m.pendingKind = ""
				m.pendingDraft = nil
				return m, nil
			case "enter", "y":
				draft := *m.pendingDraft
				kind := m.pendingKind
				enable := m.pendingEnable
				m.pendingKind = ""
				m.pendingDraft = nil
				m.statusMessage = fmt.Sprintf("Submitting %s %v...", kind, enable)
				return m, asyncControlToggle(m.ctx, m.toggleFetch, kind, enable, draft)
			}
			return m, nil
		}
		if handleViewportKey(msg, m.height, &m.scrollOffset) {
			return m, nil
		}
		switch msg.String() {
		case "?":
			m.showHelp = true
			return m, nil
		case "ctrl+c", "q":
			return m, tea.Quit
		case "r":
			m.loading = true
			m.err = nil
			m.statusMessage = ""
			m.scrollOffset = 0
			return m, asyncControlLoad(m.ctx, m.statusFetch)
		case "up", "k":
			if m.selected > 0 {
				m.selected--
				m.scrollOffset = 0
			}
		case "down", "j":
			if m.selected < len(m.data.Leases)-1 {
				m.selected++
				m.scrollOffset = 0
			}
		case "s":
			if !m.readOnly {
				m.openToggleDraft("safe-mode", true)
			}
		case "d":
			if !m.readOnly {
				m.openToggleDraft("safe-mode", false)
			}
		case "g":
			if !m.readOnly {
				m.openToggleDraft("kill-switch", true)
			}
		case "h":
			if !m.readOnly {
				m.openToggleDraft("kill-switch", false)
			}
		}
	case controlLoadedMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.data = msg.data
			if m.selected >= len(m.data.Leases) {
				m.selected = maxInt(0, len(m.data.Leases)-1)
			}
		}
		if m.autoExit {
			return m, tea.Quit
		}
	case controlToggleLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.statusMessage = fmt.Sprintf("%s toggle failed", msg.kind)
			if m.autoExit {
				return m, tea.Quit
			}
			return m, nil
		}
		state := "disabled"
		if msg.enabled {
			state = "enabled"
		}
		m.statusMessage = fmt.Sprintf("%s %s", msg.kind, state)
		if m.autoExit {
			return m, tea.Quit
		}
		return m, asyncControlLoad(m.ctx, m.statusFetch)
	}
	return m, nil
}

func (m *controlModel) openToggleDraft(kind string, enable bool) {
	draft := newActionDraft(kind)
	if kind == "safe-mode" {
		draft.ReasonCode = "operator.interactive.safe_mode_toggle"
		draft.ReasonText = "interactive safe-mode toggle via kubectl-cybermesh"
	} else {
		draft.ReasonCode = "operator.interactive.kill_switch_toggle"
		draft.ReasonText = "interactive kill-switch toggle via kubectl-cybermesh"
	}
	if !enable {
		draft.ReasonText = "interactive " + kind + " disable via kubectl-cybermesh"
	}
	m.pendingKind = kind
	m.pendingEnable = enable
	m.pendingDraft = &draft
}

func (m *controlModel) selectedLease() *controlLeaseRow {
	if len(m.data.Leases) == 0 || m.selected < 0 || m.selected >= len(m.data.Leases) {
		return nil
	}
	return &m.data.Leases[m.selected]
}

func (m *controlModel) View() string {
	compact := compactLayout(m.width)
	header := []string{
		titleStyle.Render(m.title),
		helpStyle.Render(controlHelpText(compact, m.readOnly)),
		"",
		kvLine("Safe Mode", renderBooleanStatus(m.data.ControlMutationSafeMode)),
		kvLine("Kill Switch", renderBooleanStatus(m.data.ControlMutationKillSwitch)),
	}
	if m.statusMessage != "" {
		header = append(header, helpStyle.Render(m.statusMessage))
	}
	if m.loading {
		return joinLines(append(header, "", "Loading control status...")...)
	}
	if m.showHelp {
		lines := []string{
			"j/k or arrows: move selected lease",
			"PgUp / PgDn: scroll the full detail body",
			"Ctrl+U / Ctrl+D: faster full-body scroll",
			"r: refresh",
			"q: quit",
		}
		title := "Leases Help"
		if !m.readOnly {
			title = "Control Help"
			lines = append([]string{
				"j/k or arrows: move selected lease",
				"s: enable safe mode",
				"d: disable safe mode",
				"g: enable kill switch",
				"h: disable kill switch",
				"PgUp / PgDn: scroll the full detail body",
				"Ctrl+U / Ctrl+D: faster full-body scroll",
				"r: refresh",
				"q: quit",
			}, []string{}...)
		}
		return renderHelpOverlay(title, lines, m.height)
	}
	if m.err != nil {
		header = append(header, "", errorStyle.Render(m.err.Error()))
	}
	body := []string{""}
	body = append(body, renderControlLeasePane(m.data.Leases, m.selected, listRowsForHeight(m.height), compact)...)
	body = append(body, "")
	body = append(body, labelStyle.Render("Selected Lease"))
	if lease := m.selectedLease(); lease != nil {
		body = append(body,
			kvLine("Lease Key", lease.LeaseKey),
			kvLine("Holder", lease.HolderID),
			kvLine("Epoch", fmt.Sprintf("%d", lease.Epoch)),
			kvLine("Lease Until", formatTimestampAuto(lease.LeaseUntil)),
			kvLine("Updated At", formatTimestampAuto(lease.UpdatedAt)),
			kvLine("Active", renderBooleanStatus(lease.IsActive)),
		)
		if !compact {
			body = append(body, kvLine("Stale By", fmt.Sprintf("%dms", lease.StaleByMs)))
		}
	} else {
		body = append(body, "-")
	}
	if m.pendingDraft != nil {
		body = append(body, "")
		label := "disable"
		if m.pendingEnable {
			label = "enable"
		}
		body = append(body, errorStyle.Render(fmt.Sprintf("Confirm %s %s", label, m.pendingKind)))
		body = append(body, kvLine(fieldCursor("reason_code", m.pendingDraft.Focus == 0), m.pendingDraft.ReasonCode))
		body = append(body, kvLine(fieldCursor("reason_text", m.pendingDraft.Focus == 1), m.pendingDraft.ReasonText))
		body = append(body, kvLine(fieldCursor("classification", m.pendingDraft.Focus == 2), m.pendingDraft.Classification))
		body = append(body, helpStyle.Render("tab switch field • type to edit • enter/y confirm • esc/n cancel"))
	}
	return finalizeView(header, body, m.height, m.scrollOffset)
}

func controlHelpText(compact bool, readOnly bool) string {
	if readOnly {
		if compact {
			return "j/k move • ? help • pg scroll • r refresh • q quit"
		}
		return "j/k move • ? help • pgup/pgdn scroll • r refresh • q quit"
	}
	if compact {
		return "j/k move • ? help • pg scroll • s/d safe • g/h kill • q quit"
	}
	return "j/k move • ? help • pgup/pgdn scroll • r refresh • s enable safe-mode • d disable safe-mode • g enable kill-switch • h disable kill-switch • q quit"
}

func renderControlLeasePane(rows []controlLeaseRow, selected int, maxRows int, compact bool) []string {
	lines := []string{labelStyle.Render("Dispatcher Leases")}
	if len(rows) == 0 {
		return append(lines, "- no rows -")
	}
	start, end := listWindow(len(rows), selected, maxRows)
	if start > 0 {
		lines = append(lines, helpStyle.Render(fmt.Sprintf("... %d earlier rows ...", start)))
	}
	for i := start; i < end; i++ {
		row := rows[i]
		prefix := " "
		if i == selected {
			prefix = cursorStyle.String()
		}
		if compact {
			lines = append(lines, fmt.Sprintf("%s %s  e=%d", prefix, blankDash(shortID(row.LeaseKey, 16)), row.Epoch))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s  epoch=%d", prefix, blankDash(shortID(row.LeaseKey, 24)), renderBooleanStatus(row.IsActive), row.Epoch))
	}
	if end < len(rows) {
		lines = append(lines, helpStyle.Render(fmt.Sprintf("... %d more rows ...", len(rows)-end)))
	}
	return lines
}

func renderBooleanStatus(enabled bool) string {
	if enabled {
		return "ENABLED"
	}
	return "DISABLED"
}
