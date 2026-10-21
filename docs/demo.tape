# Regenerate from the repository root:
#   task cli:build
#   mise exec ffmpeg@8.1.1 vhs@0.11.0 -- vhs docs/demo.tape
# VHS also requires ttyd and trash on PATH.

Output docs/demo.gif

Require git
Require trash

Set Shell "bash"
Set FontSize 22
Set Width 1400
Set Height 650
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 30ms
Set Framerate 20

Hide
Type "export PATH=$PWD/bin:$PATH PS1='$ ' DEMO_DIR=$(mktemp -d /tmp/noon-demo.XXXXXX)"
Enter
Type "export HOME=$DEMO_DIR XDG_CONFIG_HOME=$DEMO_DIR/.config XDG_STATE_HOME=$DEMO_DIR/.state; mkdir $DEMO_DIR/app; cd $DEMO_DIR/app; git init -q"
Enter
Type `printf '#!/bin/sh\nfor p in config:0.21 schedule:0.34 store:0.52; do sleep 0.4; echo "ok   example.com/app/${p%%:*}\t${p#*:}s"; done\n' > test.sh; chmod +x test.sh; clear`
Enter
Show

Type `noon add tests "every day at 2am" -- ./test.sh`
Sleep 500ms
Enter
Sleep 3s

Type "noon run tests"
Sleep 500ms
Enter
Sleep 3s

Type "noon"
Sleep 500ms
Enter
Sleep 5s

Hide
Type "cd /; trash $DEMO_DIR"
Enter
