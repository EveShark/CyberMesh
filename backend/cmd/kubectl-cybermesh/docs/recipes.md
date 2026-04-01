# Recipes

This guide is for the moments when you already know what you want to do and just
need the command.

Use these examples as starting points. Replace IDs, workflow names, and tenants with
your own values.

For live validation, prefer IDs from the current cluster:

```powershell
kubectl-cybermesh anomalies list --limit 5
kubectl-cybermesh --tenant default outbox get --limit 5
kubectl-cybermesh --tenant default ack get --limit 5
```

## Quick Health Check

Check the CLI, backend reachability, and current control state:

```powershell
kubectl-cybermesh doctor
kubectl-cybermesh version --verbose
kubectl-cybermesh --no-interactive control
kubectl-cybermesh --no-interactive backlog
kubectl-cybermesh --no-interactive leases
```

If you want the raw payloads:

```powershell
kubectl-cybermesh -o json doctor
kubectl-cybermesh -o json control
```

## Follow A Policy End To End

Start with the policy:

```powershell
kubectl-cybermesh --tenant default --no-interactive policies get <policy_id>
```

Then follow the linked workflow, audit, trace, outbox, and ACKs:

```powershell
kubectl-cybermesh --tenant default --no-interactive workflows get <workflow_id>
kubectl-cybermesh --tenant default --no-interactive audit get --workflow-id <workflow_id> --limit 10
kubectl-cybermesh --tenant default --no-interactive trace policy <policy_id>
kubectl-cybermesh --tenant default --no-interactive outbox get --workflow-id <workflow_id>
kubectl-cybermesh --tenant default --no-interactive ack get --policy-id <policy_id>
```

If the policy came from a fresh anomaly scenario and has no workflow ID yet, use:

```powershell
kubectl-cybermesh --tenant default --no-interactive trace policy <policy_id>
kubectl-cybermesh --tenant default --no-interactive outbox get --policy-id <policy_id>
kubectl-cybermesh --tenant default --no-interactive ack get --policy-id <policy_id>
```

## Open The Connected Interactive Views

Use these when you want the richer linked investigation flow:

```powershell
kubectl-cybermesh --interactive policies get <policy_id>
kubectl-cybermesh --interactive workflows get <workflow_id>
kubectl-cybermesh --interactive audit get --workflow-id <workflow_id>
kubectl-cybermesh --interactive trace policy <policy_id>
```

Shared keys:

- `?` help
- `j` / `k` move
- arrows move
- `PgUp` / `PgDn` scroll
- `Ctrl+U` / `Ctrl+D` faster scroll
- `q` quit

## Open The Read-Only Operator Screens

Use these for quick live inspection:

```powershell
kubectl-cybermesh --interactive control
kubectl-cybermesh --interactive backlog
kubectl-cybermesh --interactive leases
kubectl-cybermesh --interactive ack get --policy-id <policy_id>
kubectl-cybermesh --interactive validators
```

`monitor` is interactive-only:

```powershell
kubectl-cybermesh monitor --workflow-id <workflow_id>
```

## Check A Workflow That Is Stuck

Start with the workflow:

```powershell
kubectl-cybermesh --tenant default --no-interactive workflows get <workflow_id>
```

Then look at outbox and audit:

```powershell
kubectl-cybermesh --tenant default --no-interactive outbox get --workflow-id <workflow_id>
kubectl-cybermesh --tenant default --no-interactive audit get --workflow-id <workflow_id> --limit 20
kubectl-cybermesh --tenant default --no-interactive trace workflow <workflow_id>
```

If `audit get --workflow-id ...` returns nothing, that can still be normal when
no manual control mutation has happened yet.

If you want the live console:

```powershell
kubectl-cybermesh monitor --workflow-id <workflow_id>
```

## Inspect Outbox Pressure

Backlog first:

```powershell
kubectl-cybermesh backlog
```

Then inspect a specific workflow:

```powershell
kubectl-cybermesh --tenant default --no-interactive outbox get --workflow-id <workflow_id>
```

If rows are pending for a long time:

```powershell
kubectl-cybermesh --no-interactive control
kubectl-cybermesh --no-interactive leases
kubectl-cybermesh validators
kubectl-cybermesh consensus
```

## Inspect ACK Failures

```powershell
kubectl-cybermesh --tenant default --no-interactive ack get --policy-id <policy_id>
kubectl-cybermesh --tenant default --no-interactive policies acks <policy_id>
kubectl-cybermesh --tenant default --no-interactive trace policy <policy_id>
kubectl-cybermesh --tenant default --no-interactive audit get --policy-id <policy_id> --limit 10
```

If the policy was auto-generated and never manually approved, rejected, revoked,
or rolled back, the audit query may legitimately return no rows.

## Put The Control Plane In Safe Mode

```powershell
kubectl-cybermesh safe-mode enable --reason-code operator.manual_test --reason-text "maintenance window" --yes
kubectl-cybermesh --no-interactive control
kubectl-cybermesh backlog
```

Disable it after the check:

```powershell
kubectl-cybermesh safe-mode disable --reason-code operator.manual_test --reason-text "maintenance complete" --yes
kubectl-cybermesh --no-interactive control
```

## Use The Kill Switch Carefully

Enable:

```powershell
kubectl-cybermesh kill-switch enable --reason-code operator.manual_test --reason-text "emergency freeze" --yes
kubectl-cybermesh --no-interactive control
```

Disable:

```powershell
kubectl-cybermesh kill-switch disable --reason-code operator.manual_test --reason-text "recovery complete" --yes
kubectl-cybermesh --no-interactive control
```

## Directly Revoke A Published Outbox Row

Inspect the workflow first:

```powershell
kubectl-cybermesh --tenant default --no-interactive outbox get --workflow-id <workflow_id>
```

Then revoke a specific row:

```powershell
kubectl-cybermesh --tenant default revoke --outbox-id <outbox_id> --reason-code operator.manual_test --reason-text "manual revoke validation" --workflow-id <workflow_id> --yes
```

Then verify:

```powershell
kubectl-cybermesh --tenant default --no-interactive outbox get --workflow-id <workflow_id>
kubectl-cybermesh --tenant default --no-interactive workflows get <workflow_id>
kubectl-cybermesh --tenant default --no-interactive trace policy <policy_id>
kubectl-cybermesh --tenant default --no-interactive audit get --workflow-id <workflow_id> --limit 10
```

## Use JSON In Scripts

PowerShell example:

```powershell
$control = kubectl-cybermesh -o json control | ConvertFrom-Json
$control.control_mutation_safe_mode
$control.control_mutation_kill_switch
```

Another example:

```powershell
$outbox = kubectl-cybermesh -o json --tenant default outbox get --workflow-id <workflow_id> | ConvertFrom-Json
$outbox.rows | Select-Object id, status, workflow_id, created_at
```

## Sanity-Check Help

If you forget the exact syntax:

```powershell
kubectl-cybermesh help outbox
kubectl-cybermesh help trace
kubectl-cybermesh help safe-mode
kubectl-cybermesh help kill-switch
kubectl-cybermesh help revoke
```

## Common Gotchas

`--interactive` is usually a global flag:

```powershell
kubectl-cybermesh --interactive control
kubectl-cybermesh --interactive policies get <policy_id>
```

Not:

```powershell
kubectl-cybermesh control --interactive
```

Mutations usually need tenant context:

```powershell
kubectl-cybermesh --tenant default revoke --outbox-id <id> --reason-code operator.manual_test --reason-text "test" --yes
```

If anomalies are empty, that may be normal for the current cluster:

```powershell
kubectl-cybermesh anomalies list --limit 20
kubectl-cybermesh -o json anomalies list --limit 20
```
