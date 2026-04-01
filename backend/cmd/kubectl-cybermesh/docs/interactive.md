# Interactive Mode

Interactive mode exists for operators who want more than a one-shot command result.

There are two styles in the CLI:

- connected investigation views
- read-only operator screens

## Connected Views

Use these when you want related objects and richer navigation:

- `policies`
- `workflows`
- `audit`
- `trace`

Examples:

```powershell
kubectl-cybermesh --interactive policies get <policy_id>
kubectl-cybermesh --interactive workflows get <workflow_id>
kubectl-cybermesh --interactive audit get --workflow-id <workflow_id>
kubectl-cybermesh --interactive trace policy <policy_id>
```

These are the views to use when you want the older connected operator experience:

- browse related policies
- inspect linked workflow context
- open audit slices
- follow trace lineage

## Read-Only Screens

Use these when you want a focused live screen:

- `backlog`
- `control`
- `leases`
- `ack`
- `validators`

Examples:

```powershell
kubectl-cybermesh --interactive backlog
kubectl-cybermesh --interactive control
kubectl-cybermesh --interactive leases
kubectl-cybermesh --interactive ack get --policy-id <policy_id>
kubectl-cybermesh --interactive validators
```

These are not the same as the connected policy/workflow views. They are narrower by
design and mostly optimized for reading and refresh.

## Monitor

`monitor` is different from the rest of the CLI.

It is intentionally interactive-only:

```powershell
kubectl-cybermesh monitor --workflow-id <workflow_id>
```

Use it when you want:

- consensus context
- outbox rows
- ACK rows
- one live console

## Common Shortcuts

These keys work across most interactive screens:

- `?`: show help
- `q`: quit
- `j` / `k`: move
- `up` / `down`: move
- `PgUp` / `PgDn`: scroll
- `Ctrl+U` / `Ctrl+D`: faster scroll
- `r`: refresh

Additional keys on some screens:

- `tab`: switch panes or tabs
- `esc`: close help overlays and certain detail states

## How To Force Interactive Mode

For most commands, `--interactive` is a global flag:

```powershell
kubectl-cybermesh --interactive control
kubectl-cybermesh --interactive policies get <policy_id>
```

`trace` supports both:

```powershell
kubectl-cybermesh --interactive trace workflow <workflow_id>
kubectl-cybermesh trace workflow <workflow_id> --interactive
```

If you put `--interactive` after a command that does not parse it locally, you may
just get normal output.

## When To Use Interactive

Use interactive mode when:

- you are investigating related objects
- you want to scroll through rows live
- you want the operator console feel rather than a one-off report

Use normal table output when:

- you want something easy to paste into chat or tickets
- you are writing runbooks
- you are validating exact output quickly

Use JSON when:

- another tool needs to consume the output
- you are automating
- you want the raw payload shape
