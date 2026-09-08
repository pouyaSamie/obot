FROM docker.arvancloud.ir/library/golang:1.27-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc

RUN apk add --no-cache \
    bash \
    build-base \
    git \
    make \
    nodejs \
    npm \
    tar

RUN npm install --global pnpm@11.7.0 --registry=https://registry.npmmirror.com

WORKDIR /work
