#!/bin/bash

error() {
    local msg="$*"
    [ -n "$msg" ] && echo -e "ERROR: $msg" 1>&2
    exit 1 
}

main() {
    local target="$1"
    local name="$2"

    [ -z "$target" ] && error "no target specified"
    [ -z "$name" ] && error "no name specified"

    tmux capture-pane -t "$target" -e -p -J -S -50 > "$name.ansi.capture"
    tmux capture-pane -t "$target" -p -J -S -50 > "$name.capture"
}


main "$@"
