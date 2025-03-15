#!/bin/bash

script_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
src_dir=$script_dir/..

export LSDC2_SNIFF_IFACE=eth0
export LSDC2_SNIFF_FILTER="tcp port 443"
export LSDC2_CWD=
export LSDC2_UID=1000
export LSDC2_GID=1000
export LSDC2_PERSIST_FILES="scripts;README.md"
export LSDC2_BUCKET=munpri
export LSDC2_KEY=testserverwrap
export LSDC2_ZIP=
export LSDC2_ZIPFROM=$src_dir
export LSDC2_SNIFF_TIMEOUT=
export LSDC2_SNIFF_DELAY=
export LSDC2_EMPTY_TIMEOUT=
export LSDC2_LOG_STDERR=true
export LSDC2_LOG_STDOUT=true
export LSDC2_LOG_FILTER=

$src_dir/serverwrap bash -c 'while true; do echo "Line written at $(date)"; sleep 1; done'
