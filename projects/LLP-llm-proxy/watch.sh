#!/usr/bin/env bash
# watch.sh — run LLP as a background daemon in a tmux session (same pattern as
# GML's watch.sh). The session survives your shell, so LLP keeps running.
#
# Usage:
#   ./watch.sh status            # is it running? where are the logs?
#   ./watch.sh start             # build + start in the background
#   ./watch.sh stop              # stop it
#   ./watch.sh restart           # stop + start (rotates the log)
#   ./watch.sh logs [--follow]   # show the log (tail -f with --follow)
#   ./watch.sh attach            # attach to the tmux session (Ctrl+B then D to detach)
#
# Log: ~/.local/share/llp/llp.log   (rotated on restart)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LOG_DIR="${HOME}/.local/share/llp"
SESSION="llp"
LOGFILE="${LOG_DIR}/llp.log"

is_running() { tmux has-session -t "$SESSION" 2>/dev/null; }

# data API address as logged at startup (default 127.0.0.1:4000)
listen_addr() {
  [[ -f "$LOGFILE" ]] && grep -oP '(?<=data API on )\S+' "$LOGFILE" | tail -1
}

do_status() {
  if is_running; then
    local pid size addr
    pid="$(tmux list-panes -t "$SESSION" -F '#{pane_pid}' 2>/dev/null | head -1)"
    size=""; [[ -f "$LOGFILE" ]] && size=", log: $(du -h "$LOGFILE" | cut -f1)"
    echo "  llp: running (session: $SESSION, pid: ${pid}${size})"
    addr="$(listen_addr)"
    if [[ -n "$addr" ]]; then
      local health
      health="$(curl -s --max-time 3 "http://${addr}/healthz" >/dev/null 2>&1 && echo ok || echo unreachable)"
      echo "       data API http://${addr} (healthz: ${health})"
    fi
    grep -oP '(?<=control socket )\S+' "$LOGFILE" 2>/dev/null | tail -1 | sed 's/^/       control socket /'
    echo ""
    echo "  logs [--follow] | attach | stop | restart"
  else
    if [[ -f "$LOGFILE" ]]; then
      echo "  llp: stopped (log: $(du -h "$LOGFILE" | cut -f1))"
    else
      echo "  llp: stopped"
    fi
  fi
}

do_start() {
  if is_running; then
    echo "  llp: already running (use restart to replace)"
    return 0
  fi
  mkdir -p "$LOG_DIR"
  if [[ -f "$LOGFILE" && -s "$LOGFILE" ]]; then
    local started
    started="$(head -1 "$LOGFILE" | grep -oP '(?<=^# started ).*' || echo unknown)"
    mv "$LOGFILE" "${LOG_DIR}/llp-${started}--$(date +%Y%m%dT%H%M%S).log"
  fi
  local now
  now="$(date +%Y%m%dT%H%M%S)"
  echo "  llp: starting..."
  tmux new-session -d -s "$SESSION" "echo '# started $now' > '$LOGFILE'; '${SCRIPT_DIR}/run.sh' 2>&1 | tee -a '$LOGFILE'"
  sleep 2
  if is_running; then
    echo "  llp: started (log: $LOGFILE)"
    do_status
  else
    echo "  llp: failed to start — see: ./watch.sh logs" >&2
    [[ -f "$LOGFILE" ]] && tail -8 "$LOGFILE" >&2
    exit 1
  fi
}

do_stop() {
  if is_running; then
    tmux kill-session -t "$SESSION"
    echo "  llp: stopped"
  else
    echo "  llp: not running"
  fi
}

usage() {
  sed -n '2,14p' "$0" | sed 's/^# \{0,1\}//'
}

case "${1:-}" in
  status) do_status ;;
  start)  do_start ;;
  stop)   do_stop ;;
  restart) do_stop; do_start ;;
  logs)
    if [[ ! -f "$LOGFILE" ]]; then echo "no log yet ($LOGFILE)"; exit 1; fi
    if [[ "${2:-}" == "--follow" || "${2:-}" == "-f" ]]; then exec tail -f "$LOGFILE"; else tail -200 "$LOGFILE"; fi
    ;;
  attach)
    if is_running; then exec tmux attach -t "$SESSION"; else echo "llp is not running"; exit 1; fi
    ;;
  -h|--help|help|"") usage ;;
  *) echo "error: unknown command '$1'" >&2; echo "" >&2; usage; exit 1 ;;
esac
