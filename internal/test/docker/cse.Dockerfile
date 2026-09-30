# Dockerfile for running Client-Side Encryption (CSE) tests.
#
# Replicates the libmongocrypt build stage from the repository root Dockerfile
# and stages the resulting install so cgo can build with the "cse" tag.
#
# Build from the repository root:
#
#   docker build -f internal/test/docker/cse.Dockerfile -t mongo-go-driver-cse .
#
# sha found via this command: docker inspect --format='{{index .RepoDigests 0}}' golang:1.26.4-trixie
FROM golang:1.26.4-trixie@sha256:76a29248dedcd75870e95cbd90cc8cb356db082404ac7d3a5803f276c3ba79c9

RUN apt-get -qq update && \
  apt-get -qqy install --no-install-recommends \
  git \
  ca-certificates \
  curl \
  build-essential \
  libssl-dev \
  pkg-config \
  python3 \
  python3-packaging \
  python-is-python3 && \
  rm -rf /var/lib/apt/lists/*

COPY etc/install-libmongocrypt.sh /root/install-libmongocrypt.sh
RUN cd /root && bash ./install-libmongocrypt.sh

COPY . /mongo-go-driver
RUN rm -rf /mongo-go-driver/install && ln -s /root/install /mongo-go-driver/install

# The test runner bind-mounts the host repository over /mongo-go-driver, which
# hides the install symlink above, so point pkg-config at /root/install from a
# wrapper that lives outside the repository.
RUN printf '#!/bin/sh\nexec pkg-config --define-variable=prefix=/root/install/libmongocrypt "$@"\n' \
  > /usr/local/bin/libmongocrypt-pkg-config && \
  chmod +x /usr/local/bin/libmongocrypt-pkg-config
ENV PKG_CONFIG=/usr/local/bin/libmongocrypt-pkg-config
ENV PKG_CONFIG_PATH=/root/install/libmongocrypt/lib64/pkgconfig:/root/install/libmongocrypt/lib/pkgconfig
ENV LD_LIBRARY_PATH=/root/install/libmongocrypt/lib64:/root/install/libmongocrypt/lib

WORKDIR /mongo-go-driver
