#!/bin/bash
set -ex
modprobe tcp_bbr
sysctl net.core.default_qdisc=fq
sysctl net.ipv4.tcp_congestion_control=bbr
