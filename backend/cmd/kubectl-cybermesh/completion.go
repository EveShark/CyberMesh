package main

import (
	"fmt"
	"strings"
)

func shellCompletion(shell string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "bash":
		return bashCompletionScript(), nil
	case "zsh":
		return zshCompletionScript(), nil
	case "powershell", "pwsh":
		return powershellCompletionScript(), nil
	default:
		return "", fmt.Errorf("unsupported shell %q (expected bash, zsh, or powershell)", shell)
	}
}

func bashCompletionScript() string {
	return strings.TrimSpace(`_kubectl_cybermesh()
{
    local cur prev words cword
    _init_completion || return

    local commands="anomalies ai backlog policies workflows audit control completion trace monitor outbox ack leases validators consensus safe-mode kill-switch revoke version doctor help"
    local global_flags="--base-url --token --api-key --tenant --mtls-cert --mtls-key --ca-file -o --output --interactive --no-interactive -h --help"

    if [[ ${cword} -eq 1 ]]; then
        COMPREPLY=( $(compgen -W "${commands} ${global_flags}" -- "$cur") )
        return
    fi

    case "${words[1]}" in
        anomalies)
            COMPREPLY=( $(compgen -W "list --limit --severity -h --help" -- "$cur") )
            ;;
        ai)
            if [[ ${cword} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "history suspicious-nodes -h --help" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "-h --help" -- "$cur") )
            fi
            ;;
        backlog)
            COMPREPLY=( $(compgen -W "-h --help" -- "$cur") )
            ;;
        policies)
            if [[ ${cword} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "list get coverage acks revoke approve reject -h --help" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "--limit --status --workflow-id --reason-code --reason-text --classification --yes -h --help" -- "$cur") )
            fi
            ;;
        workflows)
            if [[ ${cword} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "list get rollback -h --help" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "--limit --status --reason-code --reason-text --classification --yes -h --help" -- "$cur") )
            fi
            ;;
        audit)
            if [[ ${cword} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "get export -h --help" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "--policy-id --workflow-id --action-type --actor --window --limit -h --help" -- "$cur") )
            fi
            ;;
        control)
            COMPREPLY=( $(compgen -W "-h --help" -- "$cur") )
            ;;
        trace)
            if [[ ${cword} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "policy trace anomaly sentinel source workflow -h --help" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "--interactive -h --help" -- "$cur") )
            fi
            ;;
        monitor)
            COMPREPLY=( $(compgen -W "--policy-id --workflow-id --status -h --help" -- "$cur") )
            ;;
        outbox)
            if [[ ${cword} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "get -h --help" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "--policy-id --anomaly-id --request-id --command-id --workflow-id --trace-id --sentinel-event-id --source-id --source-type -h --help" -- "$cur") )
            fi
            ;;
        ack)
            if [[ ${cword} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "get -h --help" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "--policy-id --trace-id --source-event-id --sentinel-event-id --ack-event-id --request-id --command-id --workflow-id -h --help" -- "$cur") )
            fi
            ;;
        leases)
            COMPREPLY=( $(compgen -W "-h --help" -- "$cur") )
            ;;
        validators)
            COMPREPLY=( $(compgen -W "--status -h --help" -- "$cur") )
            ;;
        consensus|doctor)
            COMPREPLY=( $(compgen -W "-h --help" -- "$cur") )
            ;;
        version)
            COMPREPLY=( $(compgen -W "--verbose -h --help" -- "$cur") )
            ;;
        safe-mode|kill-switch)
            if [[ ${cword} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "enable disable -h --help" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "--reason-code --reason-text --yes -h --help" -- "$cur") )
            fi
            ;;
        revoke)
            COMPREPLY=( $(compgen -W "--outbox-id --reason-code --reason-text --classification --workflow-id --yes -h --help" -- "$cur") )
            ;;
        completion)
            COMPREPLY=( $(compgen -W "bash zsh powershell -h --help" -- "$cur") )
            ;;
        *)
            COMPREPLY=( $(compgen -W "${global_flags}" -- "$cur") )
            ;;
    esac
}

complete -F _kubectl_cybermesh kubectl-cybermesh
complete -F _kubectl_cybermesh kubectl-cybermesh.exe`)
}

func zshCompletionScript() string {
	return strings.TrimSpace(`#compdef kubectl-cybermesh

local -a commands
commands=(
  'anomalies:List anomalies'
  'ai:Inspect AI history and suspicious nodes'
  'backlog:Inspect outbox backlog'
  'policies:Inspect and mutate policies'
  'workflows:Inspect and rollback workflows'
  'audit:Browse audit logs'
  'control:Inspect control gates'
  'completion:Generate shell completions'
  'trace:Follow lineage'
  'monitor:Interactive monitor'
  'outbox:Inspect outbox rows'
  'ack:Inspect ACK rows'
  'leases:Inspect control leases'
  'validators:List validators'
  'consensus:Show consensus status'
  'safe-mode:Toggle safe mode'
  'kill-switch:Toggle kill switch'
  'revoke:Revoke outbox row'
  'version:Show version'
  'doctor:Run diagnostics'
  'help:Show help'
)

if (( CURRENT == 2 )); then
  _describe 'command' commands
  return
fi

case "${words[2]}" in
  policies)
    _arguments '2:subcommand:(list get coverage acks revoke approve reject)' ;;
  workflows)
    _arguments '2:subcommand:(list get rollback)' ;;
  audit)
    _arguments '2:subcommand:(get export)' ;;
  trace)
    _arguments '2:subcommand:(policy trace anomaly sentinel source workflow)' ;;
  safe-mode|kill-switch)
    _arguments '2:subcommand:(enable disable)' ;;
  completion)
    _arguments '2:shell:(bash zsh powershell)' ;;
  anomalies)
    _arguments '2:subcommand:(list)' ;;
  ai)
    _arguments '2:subcommand:(history suspicious-nodes)' ;;
  outbox)
    _arguments '2:subcommand:(get)' ;;
  ack)
    _arguments '2:subcommand:(get)' ;;
  backlog|leases)
    _arguments '2:subcommand:()' ;;
esac`)
}

func powershellCompletionScript() string {
	return strings.TrimSpace(`Register-ArgumentCompleter -CommandName kubectl-cybermesh, kubectl-cybermesh.exe -ScriptBlock {
    param($commandName, $parameterName, $wordToComplete, $commandAst, $fakeBoundParameters)

    $tokens = $commandAst.CommandElements | ForEach-Object { $_.Extent.Text }

    $top = @('anomalies','ai','backlog','policies','workflows','audit','control','completion','trace','monitor','outbox','ack','leases','validators','consensus','safe-mode','kill-switch','revoke','version','doctor','help')
    $globals = @('--base-url','--token','--api-key','--tenant','--mtls-cert','--mtls-key','--ca-file','-o','--output','--interactive','--no-interactive','-h','--help')

    if ($tokens.Count -le 1) {
        @($top + $globals) | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
        }
        return
    }

    switch ($tokens[1]) {
        'ai'         { $c = @('history','suspicious-nodes','-h','--help') }
        'backlog'    { $c = @('-h','--help') }
        'policies'   { $c = @('list','get','coverage','acks','revoke','approve','reject','--limit','--status','--workflow-id','--reason-code','--reason-text','--classification','--yes','-h','--help') }
        'workflows'  { $c = @('list','get','rollback','--limit','--status','--reason-code','--reason-text','--classification','--yes','-h','--help') }
        'audit'      { $c = @('get','export','--policy-id','--workflow-id','--action-type','--actor','--window','--limit','-h','--help') }
        'trace'      { $c = @('policy','trace','anomaly','sentinel','source','workflow','--interactive','-h','--help') }
        'safe-mode'  { $c = @('enable','disable','--reason-code','--reason-text','--yes','-h','--help') }
        'kill-switch'{ $c = @('enable','disable','--reason-code','--reason-text','--yes','-h','--help') }
        'completion' { $c = @('bash','zsh','powershell','-h','--help') }
        'anomalies'  { $c = @('list','--limit','--severity','-h','--help') }
        'outbox'     { $c = @('get','--policy-id','--anomaly-id','--request-id','--command-id','--workflow-id','--trace-id','--sentinel-event-id','--source-id','--source-type','-h','--help') }
        'ack'        { $c = @('get','--policy-id','--trace-id','--source-event-id','--sentinel-event-id','--ack-event-id','--request-id','--command-id','--workflow-id','-h','--help') }
        'leases'     { $c = @('-h','--help') }
        'monitor'    { $c = @('--policy-id','--workflow-id','--status','-h','--help') }
        'validators' { $c = @('--status','-h','--help') }
        'consensus'  { $c = @('-h','--help') }
        'version'    { $c = @('--verbose','-h','--help') }
        'doctor'     { $c = @('-h','--help') }
        'revoke'     { $c = @('--outbox-id','--reason-code','--reason-text','--classification','--workflow-id','--yes','-h','--help') }
        default      { $c = $globals }
    }

    $c | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
        [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
    }
}`)
}
