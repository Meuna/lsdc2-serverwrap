#!/bin/bash

script_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
src_dir=$script_dir/..

mkdir -p $HOME/go/pkg

podman build -f $script_dir/Dockerfile -t lsdc2/serverwrap:build-image $src_dir
podman run \
    --rm \
    -v $src_dir:/go/src \
    -v $HOME/go/pkg:/go/pkg \
    --workdir /go/src \
    lsdc2/serverwrap:build-image \
    /bin/bash -c 'go get ./... && go build --ldflags "-w -s -extldflags \"-static\"" . '
