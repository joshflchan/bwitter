#!/bin/bash
# Script to run multiple miner nodes. Make sure that configs are set properly and
# each config has a respective public/private key pair in the keys/ directory
function ctrl_c() {
    echo "** Trapped CTRL-C... killing all miner processes"
    while IFS="" read -r p || [ -n "$p" ]
    do
        printf '%s\n' "$p"
        kill $p
    done < $MINER_PID_FILE
}
trap ctrl_c SIGTERM
trap ctrl_c SIGINT
trap ctrl_c EXIT

PROJECT_DIR=$PWD
 # used to track PIDS of subprocesses to cleanup at end
MINER_PID_FILE=$PWD/logs/miners_pids.txt
echo -e "Current project directory: $PROJECT_DIR"

# counts the number of miners to run based on config files in config/miner/ directory
num_miner_configs=$(ls $PWD/config/miner -1q | wc -l)
echo -e "Number of miners to run: $num_miner_configs"

# refreshes state directory before running
echo -e "Clearing existing logs and miner_pids.txt file at $MINER_PID_FILE"
rm -rf logs/
mkdir -p logs/

# `go build` because `go run` will run two processes (go and main) instead of just the executable
echo "building go miner program..."
go build -o bin/miner ./cmd/miner

for i in $( seq 1 3); do
    if [[ $i == 1 ]]; then
        bin/miner --config ./config/miner/miner_config.json &
        echo "$!" >> $MINER_PID_FILE
        echo "========================================================================"
        echo "sleeping for 3 seconds... so that first miner can mine geneis block; remove if want to test case where multiple nodes mine genesis block"
        echo "========================================================================"
        sleep 3
    else
        bin/miner --config ./config/miner/miner_config$i.json &
        echo "$!" >> $MINER_PID_FILE
        sleep 3
    fi
done
wait # don't remove this line
