# Overview

`kubectl-cybermesh` is the operator CLI for the CyberMesh backend API.

It has two jobs:

- inspect what the control plane is doing
- execute guarded mutations when an operator needs to intervene

This is not a raw API wrapper. The CLI is built around operator workflows:

- policy review
- workflow review
- audit and trace follow-up
- delivery and ACK investigation
- control-plane safety checks

## Command Model

Most commands fall into one of three buckets:

- read commands: inspect current state without changing anything
- mutation commands: create or toggle control actions
- utility commands: health, version, completion, help

Examples:

```powershell
kubectl-cybermesh --tenant default policies get <policy_id>
kubectl-cybermesh --tenant default workflows get <workflow_id>
kubectl-cybermesh doctor
kubectl-cybermesh version --verbose
```

## Output Model

Table output is the default and is built for operators:

- summary first
- timeline second
- interpretation next
- follow-up commands last

JSON output is for scripts and tooling:

```powershell
kubectl-cybermesh -o json doctor
kubectl-cybermesh -o json --tenant default outbox get --workflow-id <workflow_id>
```

Use JSON when you need:

- stable machine-readable output
- jq / PowerShell processing
- CI or automation

## Interactive Model

Interactive mode is not one thing. There are two patterns.

Connected interactive views:

- `policies`
- `workflows`
- `audit`
- `trace`

These are the richer linked investigation views. They are the ones to use when you
want to move through related policy, workflow, audit, and trace context.

Read-only interactive views:

- `backlog`
- `control`
- `leases`
- `ack`
- `validators`

These are focused operator screens. They are meant for fast reading and refresh, not
multi-object mutation workflows.

Interactive-only:

- `monitor`

`monitor` opens the consensus/outbox/ack console directly.

## Global Flags

The most important global flags are:

- `--tenant`
- `--base-url`
- `-o json`
- `--interactive`
- `--no-interactive`

For most commands, `--interactive` is a global flag and must come before the command:

```powershell
kubectl-cybermesh --interactive control
kubectl-cybermesh --interactive policies get <policy_id>
```

`trace` also supports the subcommand form:

```powershell
kubectl-cybermesh trace policy <policy_id> --interactive
```

## Top-Level Command Map

- `policies`: inspect and mutate policy decisions
- `workflows`: inspect grouped policy activity and rollback
- `audit`: review operator action history
- `trace`: follow lineage across policy, workflow, source, and anomaly selectors
- `outbox`: inspect control delivery rows
- `ack`: inspect downstream ACK history
- `control`: inspect global mutation gates and lease context
- `backlog`: inspect outbox backlog health
- `leases`: inspect dispatcher lease ownership
- `validators`: inspect validator fleet state
- `consensus`: inspect current consensus activity
- `anomalies`: inspect detected anomaly feed
- `ai`: inspect AI history and suspicious-node views
- `monitor`: open the live consensus/outbox/ack console
- `safe-mode`: enable or disable safe mode
- `kill-switch`: enable or disable the stronger global mutation gate
- `revoke`: revoke a specific outbox row directly
- `doctor`: run local and bounded backend health checks
- `version`: show CLI build and runtime context
- `completion`: generate shell completion
- `help`: show built-in command help

## Mutation Safety

Mutation commands are intentionally guarded.

Expect:

- `--yes`
- explicit reason fields
- confirmation prompts on a real terminal
- follow-up verification commands in the result output

Typical example:

```powershell
kubectl-cybermesh safe-mode enable --reason-code operator.manual_test --reason-text "maintenance window" --yes
```

When a mutation succeeds, the output should tell you what to check next.
