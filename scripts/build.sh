#!/bin/bash

script_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
src_dir=$script_dir/..

podman build -f $script_dir/Dockerfile -t lsdc2/serverwrap:build-image $src_dir
podman run \
    --rm \
    -v $src_dir:/go/src \
    --workdir /go/src \
    lsdc2/serverwrap:build-image \
    /bin/bash -c 'go get -u ./... && go build --ldflags "-w -s -extldflags \"-static\"" . '
