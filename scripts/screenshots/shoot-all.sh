#!/usr/bin/env bash
# Every screenshot in docs/screenshots, from laneway -demo, into shots/.
set -u
cd "$(dirname "${BASH_SOURCE[0]}")"
./shoot.sh board     170 22 4
./shoot.sh list      170 19 4 t
./shoot.sh panel     200 57 4 t wait:1 '#' wait:1.5 lit:DEMO-4 Enter wait:3
./shoot.sh help      170 41 4 '?'
./shoot.sh burndown  170 20 4 C wait:4
./shoot.sh roadmap   170 20 4 R wait:3 Space wait:2
./shoot.sh planning  170 22 4 P wait:3
./shoot.sh inbox     170 24 4 I wait:4
./shoot.sh standup   170 29 4 U wait:4
./shoot.sh worklogs  170 12 4 W wait:3
./shoot.sh mywork    170 19 4 O wait:3
./shoot.sh jql       170 14 4 Q wait:1 lit:'assignee = currentUser() AND status = "In ' wait:2
./shoot.sh palette   170 30 4 : wait:1
./shoot.sh settings  170 34 4 , wait:1
tmux -L lane kill-server 2>/dev/null
