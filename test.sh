#!/usr/bin/env bash

agentaddr=$1
advaddr=$2
clusteraddr=$3
nodename=$4

export AGENT_ADDR=${agentaddr}
# export AGENT_ADV_PORT=${advport}
# export AGENT_BIND_PORT=${bindport}
export AGENT_ADV_ADDR=${advaddr}
export AGENT_CLUSTER_ADDR=${clusteraddr}
export AGENT_NODENAME=${nodename}

./dist/agent
