# Regenerate from the repository root:
#   task cli:build
#   mise exec ffmpeg@8.1.1 vhs@0.11.0 -- vhs docs/demo-logs.tape
# VHS also requires ttyd and trash on PATH.

Output docs/demo-logs.gif

Require git
Require trash

Set Shell "bash"
Set FontSize 22
Set Width 1400
Set Height 560
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 30ms
Set Framerate 20

Hide
Type "export PATH=$PWD/bin:$PATH PS1='$ ' DEMO_DIR=$(mktemp -d /tmp/noon-demo.XXXXXX)"
Enter
Type "export HOME=$DEMO_DIR XDG_CONFIG_HOME=$DEMO_DIR/.config XDG_STATE_HOME=$DEMO_DIR/.state; mkdir $DEMO_DIR/app; cd $DEMO_DIR/app; git init -q"
Enter
Type `printf '#!/bin/sh\necho "checking 4 services"\nsleep 0.6; echo "ok   api     200 in 41ms"\nsleep 0.6; echo "ok   web     200 in 88ms"\nsleep 0.6; echo "FAIL worker timeout after 5s"\nsleep 0.6; echo "ok   db      ping 3ms"\nexit 1\n' > check.sh; chmod +x check.sh`
Enter
Type `noon add check "every 15m" -- ./check.sh >/dev/null`
Enter
Type "(noon serve >/dev/null 2>&1 & echo $! > $DEMO_DIR/serve.pid); sleep 1; clear"
Enter
Sleep 2s
Show

Type "noon run check --detach"
Sleep 500ms
Enter
Sleep 2s

Type "noon logs 1 --follow"
Sleep 500ms
Enter
Sleep 4s

Type "noon history --status failed"
Sleep 500ms
Enter
Sleep 2.5s

Type "noon logs check --match FAIL --context 1"
Sleep 500ms
Enter
Sleep 5s

Hide
Type "kill $(cat $DEMO_DIR/serve.pid); cd /; trash $DEMO_DIR"
Enter
