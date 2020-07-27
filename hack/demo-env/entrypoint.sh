#!/bin/sh

dockerdCmd="dockerd -s overlay2 -D"

export DOCKER_BUILDKIT=1

if [ -n "$TMUX_ENTRYPOINT" ]; then
  tmux new -s demo -d
  tmux new-window "$dockerdCmd"
  tmux new-window
  tmux a -t demo
else
  ( $dockerdCmd &>/var/log/dockerd.log & )
  exec ash
fi




