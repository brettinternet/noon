# Regenerate from the repository root:
#   task cli:build
#   mise exec ffmpeg@8.1.1 vhs@0.11.0 -- vhs docs/demo-tui.tape
# VHS also requires ttyd and trash on PATH.

Output docs/demo-tui.gif

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
Type `printf '#!/bin/sh\nfor p in config:0.21 schedule:0.34 store:0.52; do echo "ok   example.com/app/${p%%:*}\t${p#*:}s"; done\n' > test.sh`
Enter
Type `printf '#!/bin/sh\necho "checking 4 services"\necho "ok   api     200 in 41ms"\necho "ok   web     200 in 88ms"\necho "FAIL worker timeout after 5s"\necho "ok   db      ping 3ms"\nexit 1\n' > check.sh; chmod +x test.sh check.sh`
Enter
Type `noon add tests "every day at 2am" -- ./test.sh; noon add lint "every 30m" --sh 'echo "0 issues"'; noon add check "every 15m" -- ./check.sh`
Enter
Type `noon add review "weekdays at 9am" --agent claude --prompt "Review open PRs and list risks"`
Enter
Type "noon run tests; noon run lint; noon run check; noon run lint; noon run check"
Enter
Type "(noon serve >/dev/null 2>&1 & echo $! > $DEMO_DIR/serve.pid); sleep 1; clear"
Enter
Sleep 3s
Show

Type "noon tui"
Sleep 500ms
Enter
Sleep 2s

Enter
Sleep 2s
Type "l"
Sleep 2s
Type "/"
Sleep 500ms
Type "FAIL"
Sleep 500ms
Enter
Sleep 2.5s
Escape
Sleep 1s
Escape
Sleep 1s
Escape
Sleep 1s
Down@400ms 3
Sleep 1s
Type "r"
Sleep 3s
Type "q"
Sleep 1s

Hide
Type "kill $(cat $DEMO_DIR/serve.pid); cd /; trash $DEMO_DIR"
Enter
