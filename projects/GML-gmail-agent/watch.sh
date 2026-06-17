#!/usr/bin/env bash
# watch.sh — manage GML daemon tmux sessions
#
# Usage:
#   ./watch.sh status                    # show all daemons
#   ./watch.sh start [daemon] [--interval N] [--model M]  # start one or all
#   ./watch.sh stop [daemon]             # stop one or all
#   ./watch.sh restart [daemon] [--interval N]            # restart one or all
#   ./watch.sh logs <daemon> [--follow]  # show log file (--follow to tail)
#   ./watch.sh attach <daemon>           # attach to session (Ctrl+B D to detach)
#
# Daemons: analysis, knowledge, rules (or "all")
# Logs:    ~/.local/share/gml/<daemon>.log

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LOG_DIR="${HOME}/.local/share/gml"
DAEMONS=(analysis knowledge rules)

daemon_session() { echo "gml-$1"; }
daemon_command() {
  case "$1" in
    analysis)  echo "watch-analysis" ;;
    knowledge) echo "watch-knowledge" ;;
    rules)     echo "watch-rules" ;;
  esac
}

daemon_log() { echo "${LOG_DIR}/$1.log"; }

is_running() { tmux has-session -t "$(daemon_session "$1")" 2>/dev/null; }

resolve_daemons() {
  local name="${1:-all}"
  if [[ "$name" == "all" ]]; then
    echo "${DAEMONS[@]}"
  else
    for d in "${DAEMONS[@]}"; do
      if [[ "$d" == "$name" ]]; then
        echo "$name"
        return
      fi
    done
    echo "error: unknown daemon '$name' (use: ${DAEMONS[*]})" >&2
    return 1
  fi
}

do_status() {
  local any_running=false
  for d in "${DAEMONS[@]}"; do
    local session logfile size_info
    session="$(daemon_session "$d")"
    logfile="$(daemon_log "$d")"
    if is_running "$d"; then
      local pid
      pid="$(tmux list-panes -t "$session" -F '#{pane_pid}')"
      size_info=""
      if [[ -f "$logfile" ]]; then
        size_info=", log: $(du -h "$logfile" | cut -f1)"
      fi
      echo "  $d: running (session: $session, pid: $pid${size_info})"
      any_running=true
    else
      if [[ -f "$logfile" ]]; then
        echo "  $d: stopped (log: $(du -h "$logfile" | cut -f1))"
      else
        echo "  $d: stopped"
      fi
    fi
  done
  if $any_running; then
    echo ""
    echo "  logs <daemon>  | attach <daemon> | stop [daemon] | restart [daemon]"
  fi
}

do_start() {
  local daemon="$1"
  shift
  local session cmd logfile
  session="$(daemon_session "$daemon")"
  cmd="$(daemon_command "$daemon")"
  logfile="$(daemon_log "$daemon")"

  if is_running "$daemon"; then
    echo "  $daemon: already running (use restart to replace)"
    return 0
  fi

  mkdir -p "$LOG_DIR"

  if [[ -f "$logfile" && -s "$logfile" ]]; then
    local started ended
    started="$(head -1 "$logfile" | grep -oP '(?<=^# started ).*' || echo "unknown")"
    ended="$(date +%Y%m%dT%H%M%S)"
    mv "$logfile" "${LOG_DIR}/${daemon}-${started}--${ended}.log"
  fi

  local now
  now="$(date +%Y%m%dT%H%M%S)"

  local launch_cmd="${SCRIPT_DIR}/run-task.sh $cmd $*"
  echo "  $daemon: starting..."

  tmux new-session -d -s "$session" "echo '# started $now' > $logfile; $launch_cmd 2>&1 | tee -a $logfile"
  echo "  $daemon: started (log: $logfile)"
}

do_stop() {
  local daemon="$1"
  local session
  session="$(daemon_session "$daemon")"

  if is_running "$daemon"; then
    tmux kill-session -t "$session"
    echo "  $daemon: stopped"
  else
    echo "  $daemon: not running"
  fi
}

usage() {
  echo "Usage: ./watch.sh <command> [daemon] [args...]"
  echo ""
  echo "Commands:"
  echo "  status                    Show all daemons"
  echo "  start [daemon] [--interval N] [--model M]  Start one or all (N = minutes)"
  echo "  stop [daemon]             Stop one or all"
  echo "  restart [daemon] [--interval N] [--model M]  Restart one or all"
  echo "  logs <daemon> [--follow]  Show log file (--follow to tail -f)"
  echo "  attach <daemon>           Attach to session (Ctrl+B D to detach)"
  echo ""
  echo "Daemons: analysis, knowledge, rules (default: all)"
  echo "Logs:    ${LOG_DIR}/<daemon>.log"
}

case "${1:-}" in
  status)
    do_status
    ;;

  start)
    shift
    targets=($(resolve_daemons "${1:-all}"))
    [[ "${1:-all}" != "all" ]] && shift
    for d in "${targets[@]}"; do
      do_start "$d" "$@"
    done
    ;;

  stop)
    shift
    targets=($(resolve_daemons "${1:-all}"))
    for d in "${targets[@]}"; do
      do_stop "$d"
    done
    ;;

  restart)
    shift
    targets=($(resolve_daemons "${1:-all}"))
    [[ "${1:-all}" != "all" ]] && shift
    for d in "${targets[@]}"; do
      do_stop "$d"
      do_start "$d" "$@"
    done
    ;;

  logs)
    shift
    daemon="${1:?error: logs requires a daemon name (analysis, knowledge, rules)}"
    resolve_daemons "$daemon" >/dev/null
    shift
    logfile="$(daemon_log "$daemon")"
    if [[ ! -f "$logfile" ]]; then
      echo "$daemon: no log file yet ($logfile)"
      exit 1
    fi
    if [[ "${1:-}" == "--follow" || "${1:-}" == "-f" ]]; then
      exec tail -f "$logfile"
    else
      tail -200 "$logfile"
    fi
    ;;

  attach)
    shift
    daemon="${1:?error: attach requires a daemon name (analysis, knowledge, rules)}"
    resolve_daemons "$daemon" >/dev/null
    session="$(daemon_session "$daemon")"
    if is_running "$daemon"; then
      exec tmux attach -t "$session"
    else
      echo "$daemon is not running"
      exit 1
    fi
    ;;

  -h|--help|help)
    usage
    ;;

  "")
    usage
    ;;

  *)
    echo "error: unknown command '$1'" >&2
    echo "" >&2
    usage
    exit 1
    ;;
esac
