package main

import "strings"

func isHelpToken(s string) bool {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "help", "-h", "--help":
		return true
	default:
		return false
	}
}

func hasHelpArg(args []string) bool {
	for _, arg := range args {
		if isHelpToken(arg) {
			return true
		}
	}
	return false
}

func normalizeHelpArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if isHelpToken(arg) {
			continue
		}
		out = append(out, strings.TrimSpace(strings.ToLower(arg)))
	}
	return out
}

func helpTextForArgs(args []string) string {
	parts := normalizeHelpArgs(args)
	if len(parts) == 0 {
		return usageText()
	}
	switch parts[0] {
	case "policies":
		return helpPolicies(parts[1:])
	case "workflows":
		return helpWorkflows(parts[1:])
	case "audit":
		return helpAudit(parts[1:])
	case "ai":
		return helpAI(parts[1:])
	case "backlog":
		return helpBacklog()
	case "control":
		return helpControl(parts[1:])
	case "completion":
		return helpCompletion(parts[1:])
	case "trace":
		return helpTrace(parts[1:])
	case "monitor":
		return helpMonitor()
	case "anomalies":
		return helpAnomalies(parts[1:])
	case "outbox":
		return helpOutbox(parts[1:])
	case "ack":
		return helpAck(parts[1:])
	case "validators":
		return helpValidators()
	case "consensus":
		return helpConsensus()
	case "safe-mode":
		return helpSafeMode()
	case "kill-switch":
		return helpKillSwitch()
	case "revoke":
		return helpRevoke()
	case "version":
		return helpVersion()
	case "doctor":
		return helpDoctor()
	case "leases":
		return helpLeases()
	default:
		return usageText()
	}
}

func helpPolicies(args []string) string {
	if len(args) == 0 {
		return strings.TrimSpace(`
Policies
  Investigate policy state, coverage, trace context, and operator decisions.

Usage:
  kubectl cybermesh policies list [--limit N] [--status STATUS] [--workflow-id <id>]
  kubectl cybermesh policies get <policy_id>
  kubectl cybermesh policies coverage <policy_id>
  kubectl cybermesh policies acks <policy_id>
  kubectl cybermesh policies revoke <policy_id> --reason-code <code> --reason-text <text> --yes [--classification <value>] [--workflow-id <id>]
  kubectl cybermesh policies approve <policy_id> --reason-code <code> --reason-text <text> --yes [--classification <value>] [--workflow-id <id>]
  kubectl cybermesh policies reject <policy_id> --reason-code <code> --reason-text <text> --yes [--classification <value>] [--workflow-id <id>]

Examples:
  kubectl cybermesh policies list --limit 20
  kubectl cybermesh policies get <policy_id>
  kubectl cybermesh policies revoke <policy_id> --reason-code operator.revoke --reason-text "manual revoke" --yes

Interactive:
  On a real terminal, policies list|get open the interactive view by default.
  Use --no-interactive for plain table output and -o json for automation.
`)
	}
	switch args[0] {
	case "list":
		return strings.TrimSpace(`
Policies List
Usage:
  kubectl cybermesh policies list [--limit N] [--status STATUS] [--workflow-id <id>]

Shows policy summaries with latest status, workflow, outbox count, and ack state.

Examples:
  kubectl cybermesh policies list --limit 20
  kubectl cybermesh policies list --status acked
  kubectl cybermesh policies list --workflow-id incident-42
`)
	case "get":
		return strings.TrimSpace(`
Policies Get
Usage:
  kubectl cybermesh policies get <policy_id>

Shows the selected policy, latest outbox row, latest ack, coverage, and linked trace context.

Examples:
  kubectl cybermesh policies get <policy_id>
  kubectl cybermesh --no-interactive policies get <policy_id>
  kubectl cybermesh -o json policies get <policy_id>
`)
	case "coverage":
		return strings.TrimSpace(`
Policies Coverage
Usage:
  kubectl cybermesh policies coverage <policy_id>

Shows publish and ack coverage for a policy lineage.

Examples:
  kubectl cybermesh policies coverage <policy_id>
`)
	case "acks":
		return strings.TrimSpace(`
Policies ACKs
Usage:
  kubectl cybermesh policies acks <policy_id>

Shows policy-first ACK history for a single policy.

Examples:
  kubectl cybermesh policies acks <policy_id>
  kubectl cybermesh -o json policies acks <policy_id>
`)
	case "revoke", "approve", "reject":
		return helpPolicyMutation(args[0])
	default:
		return helpPolicies(nil)
	}
}

func helpBacklog() string {
	return strings.TrimSpace(`
Backlog
  Inspect outbox backlog health and ACK closure at a glance.

Usage:
  kubectl cybermesh backlog

Shows:
  pending, retry, publishing, published, acked, terminal, total,
  oldest pending age, and ACK closure ratio.

Interactive:
  On a real terminal, backlog opens a read-only interactive view by default.
`)
}

func helpAI(args []string) string {
	if len(args) == 0 {
		return strings.TrimSpace(`
AI
  Inspect AI detection history and suspicious-node views.

Usage:
  kubectl cybermesh ai history
  kubectl cybermesh ai suspicious-nodes

Interactive:
  On a real terminal, AI opens an interactive read-only view by default.
  Use tab to switch between history and suspicious-node views.
`)
	}
	switch args[0] {
	case "history":
		return strings.TrimSpace(`
AI History
Usage:
  kubectl cybermesh ai history

Shows recent AI detections if the backend has recorded any.
`)
	case "suspicious-nodes":
		return strings.TrimSpace(`
AI Suspicious Nodes
Usage:
  kubectl cybermesh ai suspicious-nodes

Shows suspicious nodes reported by the AI surface if data is available.
`)
	default:
		return helpAI(nil)
	}
}

func helpPolicyMutation(action string) string {
	actionUpper := strings.ToUpper(action)
	return strings.TrimSpace(`
Policies ` + actionUpper + `
Usage:
  kubectl cybermesh policies ` + action + ` <policy_id> --reason-code <code> --reason-text <text> --yes [--classification <value>] [--workflow-id <id>]

Required:
  <policy_id>
  --reason-code
  --reason-text
  --yes

Notes:
  - Mutations may require --tenant depending on backend policy.
  - Safe mode and kill switch can block the request.
  - Use policies get, audit get, or trace policy to verify the result.

Examples:
  kubectl cybermesh --tenant default policies ` + action + ` <policy_id> --reason-code operator.` + action + ` --reason-text "manual ` + action + `" --yes
`)
}

func helpWorkflows(args []string) string {
	if len(args) == 0 {
		return strings.TrimSpace(`
Workflows
  Inspect workflow lineage and rollback mutable workflow members.

Usage:
  kubectl cybermesh workflows list [--limit N] [--status STATUS]
  kubectl cybermesh workflows get <workflow_id>
  kubectl cybermesh workflows rollback <workflow_id> --reason-code <code> --reason-text <text> --yes [--classification <value>]

Examples:
  kubectl cybermesh workflows list --limit 20
  kubectl cybermesh workflows get incident-42
  kubectl cybermesh --tenant default workflows rollback incident-42 --reason-code operator.rollback --reason-text "bounded rollback" --yes
`)
	}
	switch args[0] {
	case "list":
		return strings.TrimSpace(`
Workflows List
Usage:
  kubectl cybermesh workflows list [--limit N] [--status STATUS]

Shows workflow summaries with latest policy, status, and policy counts.
`)
	case "get":
		return strings.TrimSpace(`
Workflows Get
Usage:
  kubectl cybermesh workflows get <workflow_id>

Shows workflow summary, member policies, latest ack state, and linked trace context.
`)
	case "rollback":
		return strings.TrimSpace(`
Workflows Rollback
Usage:
  kubectl cybermesh workflows rollback <workflow_id> --reason-code <code> --reason-text <text> --yes [--classification <value>]

Required:
  <workflow_id>
  --reason-code
  --reason-text
  --yes

Notes:
  - Rollback creates revoke actions for mutable members in the workflow.
  - Safe mode and kill switch can block the request.
  - Verify with workflows get or audit get --workflow-id <id>.
`)
	default:
		return helpWorkflows(nil)
	}
}

func helpAudit(args []string) string {
	if len(args) == 0 {
		return strings.TrimSpace(`
Audit
  Browse and export operator actions and control-plane decisions.

Usage:
  kubectl cybermesh audit get [--policy-id <id>] [--workflow-id <id>] [--action-type <type>] [--actor <actor>] [--window <duration>] [--limit N]
  kubectl cybermesh audit export [--policy-id <id>] [--workflow-id <id>] [--action-type <type>] [--actor <actor>] [--window <duration>] [--limit N]
`)
	}
	switch args[0] {
	case "get":
		return strings.TrimSpace(`
Audit Get
Usage:
  kubectl cybermesh audit get [--policy-id <id>] [--workflow-id <id>] [--action-type <type>] [--actor <actor>] [--window <duration>] [--limit N]

Shows recent action journal rows. On a real terminal, this opens the interactive audit view by default.
`)
	case "export":
		return strings.TrimSpace(`
Audit Export
Usage:
  kubectl cybermesh audit export [--policy-id <id>] [--workflow-id <id>] [--action-type <type>] [--actor <actor>] [--window <duration>] [--limit N]

Exports bounded audit rows in JSON envelope format.
`)
	default:
		return helpAudit(nil)
	}
}

func helpControl(args []string) string {
	if len(args) == 0 {
		return strings.TrimSpace(`
Control
  Inspect cluster mutation gates and lease holders.

Usage:
  kubectl cybermesh control

Interactive:
  On a real terminal, control opens the interactive control view by default.
  Keys include safe-mode and kill-switch toggles with guarded drafts.
`)
	}
	return helpControl(nil)
}

func helpLeases() string {
	return strings.TrimSpace(`
Leases
  Inspect dispatcher lease state and control gate flags.

Usage:
  kubectl cybermesh leases

Shows:
  lease holders, epochs, lease expiry, safe mode, and kill switch state.

Interactive:
  On a real terminal, leases opens a read-only interactive lease view by default.
`)
}

func helpCompletion(args []string) string {
	return strings.TrimSpace(`
Completion
Usage:
  kubectl cybermesh completion <bash|zsh|powershell>

Examples:
  kubectl cybermesh completion bash > cybermesh.bash
  kubectl cybermesh completion zsh > _kubectl-cybermesh
  kubectl cybermesh completion powershell > kubectl-cybermesh.ps1
`)
}

func helpTrace(args []string) string {
	return strings.TrimSpace(`
Trace
  Follow end-to-end lineage from policy, trace, anomaly, source, sentinel, or workflow selectors.

Usage:
  kubectl cybermesh trace policy <policy_id> [--interactive]
  kubectl cybermesh trace trace <trace_id> [--interactive]
  kubectl cybermesh trace anomaly <anomaly_id> [--interactive]
  kubectl cybermesh trace sentinel <sentinel_event_id> [--interactive]
  kubectl cybermesh trace source <source_event_id> [--interactive]
  kubectl cybermesh trace workflow <workflow_id> [--interactive]
`)
}

func helpMonitor() string {
	return strings.TrimSpace(`
Monitor
  Open the interactive consensus/outbox/ack monitor.

Usage:
  kubectl cybermesh monitor [--policy-id <id>] [--workflow-id <id>] [--status <status>]
`)
}

func helpAnomalies(args []string) string {
	return strings.TrimSpace(`
Anomalies
Usage:
  kubectl cybermesh anomalies list [--limit N] [--severity LEVEL]
`)
}

func helpOutbox(args []string) string {
	return strings.TrimSpace(`
Outbox
Usage:
  kubectl cybermesh outbox get [selectors]

Use selectors such as:
  --policy-id
  --workflow-id
  --request-id
  --command-id
  --trace-id
  --anomaly-id
  --source-id
  --source-type
`)
}

func helpAck(args []string) string {
	return strings.TrimSpace(`
ACK
Usage:
  kubectl cybermesh ack get [selectors]

Use selectors such as:
  --policy-id
  --workflow-id
  --trace-id
  --source-event-id
  --ack-event-id
  --request-id
  --command-id
`)
}

func helpValidators() string {
	return strings.TrimSpace(`
Validators
Usage:
  kubectl cybermesh validators [--status active|inactive]
`)
}

func helpConsensus() string {
	return strings.TrimSpace(`
Consensus
Usage:
  kubectl cybermesh consensus
`)
}

func helpSafeMode() string {
	return strings.TrimSpace(`
Safe Mode
Usage:
  kubectl cybermesh safe-mode enable|disable --reason-code <code> --reason-text <text> --yes

Safe mode is a cluster-wide mutation gate.
`)
}

func helpKillSwitch() string {
	return strings.TrimSpace(`
Kill Switch
Usage:
  kubectl cybermesh kill-switch enable|disable --reason-code <code> --reason-text <text> --yes

Kill switch is a stronger cluster-wide mutation gate and should be used carefully.
`)
}

func helpRevoke() string {
	return strings.TrimSpace(`
Revoke
Usage:
  kubectl cybermesh revoke --outbox-id <id> --reason-code <code> --reason-text <text> --yes [--classification <value>] [--workflow-id <id>]

Revokes a specific outbox row directly.
`)
}

func helpVersion() string {
	return strings.TrimSpace(`
Version
Usage:
  kubectl cybermesh version [--verbose]

Shows plugin version. Use --verbose to include build and config context.
`)
}

func helpDoctor() string {
	return strings.TrimSpace(`
Doctor
Usage:
  kubectl cybermesh doctor

Runs local configuration checks and a bounded backend reachability check.

Examples:
  kubectl cybermesh doctor
  kubectl cybermesh -o json doctor
`)
}
