# Dev image for the native Linux app (GTK 4 + libadwaita + VTE), on the
# oldest supported base (Ubuntu 24.04). It also has WebKitGTK, so the Wails
# build can be built and benchmarked next to it, and Xvfb for headless checks.
#
#   docker build -t wtf-gtk-dev:ubuntu24 linux
FROM golang:1.25 AS go

FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
      build-essential pkg-config ca-certificates git \
      libgtk-4-dev libadwaita-1-dev libvte-2.91-gtk4-dev libgirepository1.0-dev \
      libwebkitgtk-6.0-dev \
      xvfb xauth xdotool x11-utils x11-apps imagemagick dbus-x11 libgl1-mesa-dri mesa-utils \
      fonts-dejavu-core fonts-jetbrains-mono librsvg2-common adwaita-icon-theme openssh-client \
      bash zsh vim htop tmux procps lsof perl gawk \
    && rm -rf /var/lib/apt/lists/*
COPY --from=go /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:/root/go/bin:$PATH
