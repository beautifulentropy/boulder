#!/usr/bin/env bash

apt-get install -y --no-install-recommends \
  ruby \
  ruby-dev \
  gcc

gem install --no-document fpm

export ARCHIVEDIR="${PWD}"

go version
make deb
