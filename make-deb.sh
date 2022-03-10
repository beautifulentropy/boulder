#!/usr/bin/env bash

#
# Setup Dependencies
#

sudo apt-get install -y --no-install-recommends \
  ruby \
  ruby-dev \
  gcc

# Download and unpack our production go version.
GO_VERSION=$(cat docker-compose.yml | grep "TAG:-" | sed 's/.*-go//' | sed 's/_.*//')
GO_INSTALL_PATH=$(which go)
wget -O go.tgz "https://dl.google.com/go/go${GO_VERSION}.linux-amd64.tar.gz"
sudo tar -C /usr/local -xzf go.tgz
ls -l `which go`
rm go.tgz
export PATH=/usr/local/bin:$PATH

# Install fpm, this is used in our Makefile to package boulder as a deb or rpm.
sudo gem install --no-document fpm

#
# Build and Release
#

# Set $ARCHIVEDIR to our current directory. If left unset our Makefile will set
# it to /tmp.
export ARCHIVEDIR="${PWD}"
which go
make deb
