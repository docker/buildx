#!/bin/sh

tmux new -s demo -d
tmux new-window 'dockerd -s overlay2 -D'
tmux new-window
tmux a -t demo


