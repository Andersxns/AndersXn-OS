#!/usr/bin/env bash
# AndersXn OS module: Jellyfin with hardware transcoding.
set -euo pipefail
source "${AXOS_MODULE_DIR:-/usr/lib/andersxn/modules}/_lib.sh"

ax_add_repo jellyfin \
    "https://repo.jellyfin.org/debian" \
    "https://repo.jellyfin.org/jellyfin_team.gpg.key" \
    main

ax_apt jellyfin-server jellyfin-web

media=/srv/andersxn/data/media
mkdir -p "$media"/{movies,shows,music}

# --- hardware transcoding ---------------------------------------------------
# Which acceleration is possible depends on the silicon actually present. Each
# branch installs only what that path needs; guessing wrong here costs the
# operator a Jellyfin that falls back to software transcode without saying why.
arch="$(ax_arch)"

if ax_has_gpu; then
    ax_step "configuring hardware transcoding"

    if [[ "$arch" == "amd64" ]]; then
        # VA-API covers both Intel Quick Sync and AMD VCN.
        ax_apt vainfo intel-media-va-driver-non-free mesa-va-drivers
        accel="vaapi"
    else
        # arm64 boards vary far too much for a single answer; V4L2 M2M is the
        # one interface that is common across the Raspberry Pi and Rockchip
        # families that actually turn up in homelabs.
        ax_apt vainfo mesa-va-drivers
        accel="v4l2m2m"
    fi

    if lspci 2>/dev/null | grep -qi nvidia && dpkg -l | grep -q nvidia-driver; then
        accel="nvenc"
        ax_info "NVIDIA driver detected - selecting NVENC"
    fi

    # Jellyfin reads this on first start; it is overwritten by the web UI once
    # the operator changes anything in Playback settings.
    install -Dm0644 -o jellyfin -g jellyfin /dev/stdin \
        /etc/jellyfin/encoding.xml <<XML
<?xml version="1.0" encoding="utf-8"?>
<EncodingOptions>
  <HardwareAccelerationType>$accel</HardwareAccelerationType>
  <EnableHardwareEncoding>true</EnableHardwareEncoding>
  <EnableTonemapping>true</EnableTonemapping>
  <EnableDecodingColorDepth10Hevc>true</EnableDecodingColorDepth10Hevc>
  <AllowHevcEncoding>true</AllowHevcEncoding>
  <HardwareDecodingCodecs>
    <string>h264</string>
    <string>hevc</string>
    <string>vp9</string>
  </HardwareDecodingCodecs>
</EncodingOptions>
XML

    # The service account needs the render node, and on some kernels the video
    # group as well.
    for g in render video; do
        getent group "$g" >/dev/null 2>&1 && usermod -aG "$g" jellyfin
    done

    # Expose /dev/dri to a service that is otherwise sandboxed away from it.
    install -Dm0644 /dev/stdin /etc/systemd/system/jellyfin.service.d/10-andersxn-gpu.conf <<'UNIT'
[Service]
DeviceAllow=/dev/dri rw
SupplementaryGroups=render video
PrivateDevices=no
UNIT

    ax_ok "hardware transcoding configured ($accel)"
else
    ax_warn "no /dev/dri render node found - Jellyfin will transcode in software"
fi

chown -R jellyfin:jellyfin "$media" 2>/dev/null || true
ax_add_user_to_groups jellyfin

ax_enable jellyfin.service
ax_ok "Jellyfin installed - finish setup at http://<host>:8096"
