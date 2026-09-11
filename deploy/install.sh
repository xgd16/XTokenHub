#!/bin/sh
# XTokenHub 部署脚本（systemd：开机自启）
#
# 作用：
#   1. 安装静态二进制到 XT_BIN（同目录临时文件 + mv，原子替换）
#   2. 安装 systemd 单元并 enable，实现开机自启
#   3. 接管遗留的 nohup 启动进程（先停掉以释放监听端口）
#
# 用法（需要 root）：
#   ./install.sh /path/to/xtokenhub-pmos-aarch64
#
# 可用环境变量覆盖：
#   XT_BIN=/usr/local/bin/xtokenhub      二进制安装路径
#   XT_ROOT=/home/user/code/XTokenHub    部署根目录（含 configs/ 与 data/）
#   XT_ENV=/etc/default/xtokenhub        环境变量文件
#   XT_UNIT=xtokenhub                    单元名（不含 .service）
#   XT_PROXY=http://127.0.0.1:7890       出网代理，写入环境变量文件
#
# 幂等：可反复执行用于升级；已存在的环境变量文件不会被覆盖。

set -eu

XT_BIN=${XT_BIN:-/usr/local/bin/xtokenhub}
XT_ROOT=${XT_ROOT:-/home/user/code/XTokenHub}
XT_ENV=${XT_ENV:-/etc/default/xtokenhub}
XT_UNIT=${XT_UNIT:-xtokenhub}
XT_PROXY=${XT_PROXY:-}

UNIT_PATH="/etc/systemd/system/${XT_UNIT}.service"
DROPIN_DIR="/etc/systemd/system/${XT_UNIT}.service.d"
SRC_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
SRC_BIN=${1:-}

say() { printf '==> %s\n' "$*"; }
die() { printf '!! %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" = "0" ] || die "需要 root 权限执行"
[ -n "$SRC_BIN" ] || die "用法: $0 <二进制路径>"
[ -f "$SRC_BIN" ] || die "二进制不存在: $SRC_BIN"
[ -f "$SRC_DIR/xtokenhub.service" ] || die "缺少单元文件: $SRC_DIR/xtokenhub.service"
[ -f "$XT_ROOT/configs/config.yaml" ] || die "配置缺失: $XT_ROOT/configs/config.yaml（配置读取失败会导致进程直接退出）"

# ---------- 1. 停掉现有实例 ----------
say "停止现有实例（systemd 单元与遗留 nohup 进程）"
systemctl stop "$XT_UNIT" 2>/dev/null || true

# 旧部署方式是在部署目录里 nohup 直接跑二进制；systemd 接管前必须释放端口。
# 只认 exe 指向 xtokenhub 的进程，避免误伤调用本脚本的 shell / ssh 会话。
legacy_pids() {
  for p in $(pgrep -f xtokenhub 2>/dev/null || true); do
    [ "$p" = "$$" ] && continue
    exe=$(readlink -f "/proc/$p/exe" 2>/dev/null || true)
    case "$exe" in
      */xtokenhub|*/xtokenhub-*) printf '%s ' "$p" ;;
    esac
  done
}

if [ -n "$(legacy_pids)" ]; then
  say "发现遗留进程: $(legacy_pids)"
  kill $(legacy_pids) 2>/dev/null || true
  i=0
  while [ -n "$(legacy_pids)" ] && [ "$i" -lt 10 ]; do
    sleep 1
    i=$((i + 1))
  done
  [ -z "$(legacy_pids)" ] || { say "温和退出超时，强制结束"; kill -9 $(legacy_pids) 2>/dev/null || true; sleep 1; }
fi

# ---------- 2. 安装二进制 ----------
say "安装二进制 -> $XT_BIN"
mkdir -p "$(dirname -- "$XT_BIN")"
tmp="${XT_BIN}.new.$$"
cp "$SRC_BIN" "$tmp"
chmod 0755 "$tmp"
mv "$tmp" "$XT_BIN"   # 同文件系统内 rename，原子替换
"$XT_BIN" -version || die "二进制无法执行（架构是否匹配？）"

# ---------- 3. 准备数据目录 ----------
mkdir -p "$XT_ROOT/data"
say "数据目录: $XT_ROOT/data"

# ---------- 4. 安装 systemd 单元 ----------
say "安装单元 -> $UNIT_PATH"
sed -e "s|/home/user/code/XTokenHub|${XT_ROOT}|g" \
    -e "s|/usr/local/bin/xtokenhub|${XT_BIN}|g" \
    "$SRC_DIR/xtokenhub.service" > "$UNIT_PATH"
chmod 0644 "$UNIT_PATH"

# ---------- 5. 环境变量文件（代理 / XT_HUB_ 覆盖） ----------
# Alpine / postmarketOS 默认没有 /etc/default，先建父目录。
mkdir -p "$(dirname -- "$XT_ENV")"
if [ ! -f "$XT_ENV" ]; then
  if [ -f "$SRC_DIR/xtokenhub.env.example" ]; then
    cp "$SRC_DIR/xtokenhub.env.example" "$XT_ENV"
  else
    : > "$XT_ENV"
  fi
  chmod 0644 "$XT_ENV"
  say "生成环境变量文件: $XT_ENV"
fi

if [ -n "$XT_PROXY" ]; then
  if grep -qE '^[[:space:]]*HTTPS_PROXY=' "$XT_ENV"; then
    say "环境变量文件已有 HTTPS_PROXY，保持不变"
  else
    {
      echo ""
      echo "# 由 install.sh 写入：设备无法直连 GitHub 时价格表同步需要代理"
      echo "HTTP_PROXY=$XT_PROXY"
      echo "HTTPS_PROXY=$XT_PROXY"
      echo "NO_PROXY=localhost,127.0.0.1,192.168.0.0/16"
    } >> "$XT_ENV"
    say "已写入出网代理: $XT_PROXY"
  fi
fi

# ---------- 6. 代理未就绪时不要抢跑 ----------
# 本机 mihomo 提供代理时，排在它后面启动，否则首次价格表同步会失败
# （失败后要等一个同步周期，或到设置页手动触发）。
if [ -f "/etc/systemd/system/mihomo.service" ] || [ -f "/usr/lib/systemd/system/mihomo.service" ]; then
  mkdir -p "$DROPIN_DIR"
  cat > "$DROPIN_DIR/10-mihomo.conf" <<'CONF'
# 由 deploy/install.sh 生成：价格表同步依赖本机 mihomo 代理，排在它之后启动。
[Unit]
After=mihomo.service
Wants=mihomo.service
CONF
  say "检测到 mihomo：已添加启动顺序依赖"
fi

# ---------- 7. 限制日志大小（postmarketOS 默认无限制，6天可积累 1GB） ----------
JOURNALD_DROPIN="/etc/systemd/journald.conf.d/size-limit.conf"
if [ ! -f "$JOURNALD_DROPIN" ]; then
  mkdir -p "$(dirname -- "$JOURNALD_DROPIN")"
  cat > "$JOURNALD_DROPIN" <<'CONF'
# 由 deploy/install.sh 生成：限制日志占用空间
[Journal]
SystemMaxUse=100M
RuntimeMaxUse=50M
CONF
  say "已配置日志大小限制: SystemMaxUse=100M"
fi

# ---------- 8. 启用并启动 ----------
say "重载 systemd 并设为开机自启"
systemctl daemon-reload
systemctl enable "$XT_UNIT" >/dev/null
systemctl restart "$XT_UNIT"

# ---------- 9. 校验 ----------
say "等待服务就绪"
i=0
while [ "$i" -lt 15 ]; do
  if systemctl is-active --quiet "$XT_UNIT"; then break; fi
  sleep 1
  i=$((i + 1))
done

if ! systemctl is-active --quiet "$XT_UNIT"; then
  systemctl status "$XT_UNIT" --no-pager -l || true
  die "服务启动失败，日志: journalctl -u $XT_UNIT -n 50"
fi

say "服务状态: $(systemctl is-active "$XT_UNIT") / 开机自启: $(systemctl is-enabled "$XT_UNIT")"
printf '\n完成。常用命令：\n'
printf '  journalctl -u %s -f          实时日志\n' "$XT_UNIT"
printf '  systemctl restart %s      重启\n' "$XT_UNIT"
printf '  systemctl stop %s         停止\n' "$XT_UNIT"
