# Command Reference

This is the practical map of the top-level CLI surfaces. It is not a full flag dump.
Use `kubectl-cybermesh help <command>` when you need exact syntax.

## Read Commands

### `policies`

Use `policies` to inspect policy state and perform policy-level approvals, rejects,
and revokes.

Common reads:

```powershell
kubectl-cybermesh --tenant default policies list --limit 20
kubectl-cybermesh --tenant default policies get <policy_id>
kubectl-cybermesh --tenant default policies coverage <policy_id>
kubectl-cybermesh --tenant default policies acks <policy_id>
```

Interactive:

```powershell
kubectl-cybermesh --interactive policies get <policy_id>
```

### `workflows`

Use `workflows` to inspect grouped policy activity and rollback candidates.

```powershell
kubectl-cybermesh --tenant default workflows list --limit 20
kubectl-cybermesh --tenant default workflows get <workflow_id>
```

Current note:

- fresh anomaly-driven policy flows may not have a workflow ID; use `trace
  policy`, `outbox get --policy-id`, and `ack get --policy-id` in that case.

Interactive:

```powershell
kubectl-cybermesh --interactive workflows get <workflow_id>
```

### `audit`

Use `audit` when you want to understand what an operator action changed and why.

```powershell
kubectl-cybermesh --tenant default audit get --workflow-id <workflow_id> --limit 10
kubectl-cybermesh --tenant default audit export --workflow-id <workflow_id> --limit 100
```

Interactive:

```powershell
kubectl-cybermesh --interactive audit get --workflow-id <workflow_id>
```

Current note:

- `audit` is mutation-focused. Automatic policy creation and normal ACK flow are
  visible through `trace`, `outbox`, and `ack`, not the action journal.

### `trace`

Use `trace` when you need lineage across policy, workflow, source, anomaly, or trace
selectors.

```powershell
kubectl-cybermesh --tenant default trace policy <policy_id>
kubectl-cybermesh --tenant default trace workflow <workflow_id>
kubectl-cybermesh --tenant default trace anomaly <anomaly_id>
```

Interactive:

```powershell
kubectl-cybermesh --interactive trace policy <policy_id>
```

### `outbox`

Use `outbox` to inspect delivery rows and current publish/ACK state.

```powershell
kubectl-cybermesh --tenant default outbox get --workflow-id <workflow_id>
kubectl-cybermesh --tenant default outbox get --policy-id <policy_id>
```

### `ack`

Use `ack` to inspect downstream acknowledgement history.

```powershell
kubectl-cybermesh --tenant default ack get --policy-id <policy_id>
kubectl-cybermesh --tenant default ack get --workflow-id <workflow_id>
```

Interactive:

```powershell
kubectl-cybermesh --interactive ack get --policy-id <policy_id>
```

### `control`

Use `control` to inspect the two global mutation gates and lease context.

```powershell
kubectl-cybermesh control
kubectl-cybermesh -o json control
```

Interactive:

```powershell
kubectl-cybermesh --interactive control
```

### `backlog`

Use `backlog` to see pending, retrying, and publishing outbox pressure.

```powershell
kubectl-cybermesh backlog
```

Interactive:

```powershell
kubectl-cybermesh --interactive backlog
```

### `leases`

Use `leases` to inspect dispatcher ownership and stale lease state.

```powershell
kubectl-cybermesh leases
```

Interactive:

```powershell
kubectl-cybermesh --interactive leases
```

### `validators`

Use `validators` to inspect validator fleet state.

```powershell
kubectl-cybermesh validators
kubectl-cybermesh validators --status inactive
```

Interactive:

```powershell
kubectl-cybermesh --interactive validators
```

### `consensus`

Use `consensus` to inspect leader, term, quorum, proposal history, and suspicious
node context.

```powershell
kubectl-cybermesh consensus
```

### `doctor`

Use `doctor` for a bounded health and configuration check.

```powershell
kubectl-cybermesh doctor
kubectl-cybermesh -o json doctor
```

### `version`

Use `version` for build and runtime context.

```powershell
kubectl-cybermesh version
kubectl-cybermesh version --verbose
```

### `anomalies`

Use `anomalies` to inspect the current anomaly feed when data is present.

```powershell
kubectl-cybermesh anomalies list --limit 20
kubectl-cybermesh anomalies list --severity high
```

Current note:

- the live cluster may legitimately return no anomaly rows

### `ai`

Use `ai` for AI history and suspicious-node views.

```powershell
kubectl-cybermesh ai history
kubectl-cybermesh ai suspicious-nodes
```

Interactive:

```powershell
kubectl-cybermesh --interactive ai history
```

### `monitor`

Use `monitor` when you want the live interactive consensus/outbox/ack console.

```powershell
kubectl-cybermesh monitor --workflow-id <workflow_id>
```

This command is interactive-only.

## Mutation Commands

### `safe-mode`

Use `safe-mode` to place the control plane into the narrower mutation mode or remove
that gate.

```powershell
kubectl-cybermesh safe-mode enable --reason-code operator.manual_test --reason-text "maintenance window" --yes
kubectl-cybermesh safe-mode disable --reason-code operator.manual_test --reason-text "maintenance complete" --yes
```

### `kill-switch`

Use `kill-switch` for the stronger cluster-wide mutation stop.

```powershell
kubectl-cybermesh kill-switch enable --reason-code operator.manual_test --reason-text "emergency freeze" --yes
kubectl-cybermesh kill-switch disable --reason-code operator.manual_test --reason-text "recovery complete" --yes
```

### `revoke`

Use direct `revoke` when you need to revoke a specific outbox row rather than working
through a higher-level policy action.

```powershell
kubectl-cybermesh --tenant default revoke --outbox-id <outbox_id> --reason-code operator.manual_test --reason-text "manual revoke validation" --workflow-id <workflow_id> --yes
```

### Workflow rollback

Rollback still lives under `workflows`:

```powershell
kubectl-cybermesh --tenant default workflows rollback <workflow_id> --reason-code operator.manual_test --reason-text "rollback validation" --yes
```

## Utility Commands

### `completion`

Generate shell completion:

```powershell
kubectl-cybermesh completion bash
kubectl-cybermesh completion zsh
kubectl-cybermesh completion powershell
```

### `help`

Use built-in help when you need exact syntax:

```powershell
kubectl-cybermesh help outbox
kubectl-cybermesh help trace
kubectl-cybermesh help safe-mode
```

## Notes

- use table mode for operator work
- use JSON for automation
- put `--interactive` before most commands
- `trace` also supports the local `--interactive` form
