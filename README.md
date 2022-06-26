# lsdc2-serverwrap

Standalone wrapper for linux game server, providing S3 persistance and packet
sniffing monitoring. It is designed to be an Docker entry point.

    export LSDC2_BUCKET=my-worlds
    export LSDC2_ZIPFROM=$HOME/savedir
    export LSDC2_PERSIST_FILES="valheim.db;valheim.fwl"
    export LSDC2_SNIFF_IFACE=eth0
    export LSDC2_SNIFF_FILTER="udp port 1234"

    ./serverwrap start-server.sh -port 1234

This command above will:
1. Fetch the key `valheim` in the `my-worlds` bucket
1. Extract the archive under `$HOME/savedir`
1. Start the process `start-server.sh -port 1234`
1. Sniff packets on interface `eth0` with the [BPF filter](https://www.tcpdump.org/manpages/pcap-filter.7.html) `udp port 1234`
1. Signal the process after a timeout without packets received
1. Archive the files `valheim.db` and `valheim.fwl` in the S3 bucket
