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

PROJECT_DIR=$PWD
MINER_PID_FILE=$PWD/logs/miners_pids.txt
echo -e "Current project directory: $PROJECT_DIR"

num_miner_configs=$(ls $PWD/config/miner -1q | wc -l)
echo -e "Number of miners to run: $num_miner_configs"

echo -e "Clearing existing logs and miner_pids.txt file at $MINER_PID_FILE"
rm -rf logs/
mkdir -p logs/

echo "building go miner program..."
go build -o bin/miner ./cmd/miner

for i in $( seq 1 2 ); do
    if [[ $i == 1 ]]; then
        bin/miner --config ./config/miner/miner_config.json &
        echo "$!" >> $MINER_PID_FILE
    else
        bin/miner --config ./config/miner/miner_config$i.json &
        echo "$!" >> $MINER_PID_FILE
    fi
done
wait