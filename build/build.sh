#!/bin/bash

build_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
docker build -f $build_dir/Dockerfile $build_dir/.. -t lsdc2/serverwrap
docker run --rm -v $build_dir:/mnt lsdc2/serverwrap