#!/bin/bash
# =============================================================================
# MXGT 发布包打包 / GitHub Release 上传脚本
#
# 命名格式（规则：文件名 + 版本号 + 时间）：
#   MXGT_v5.14.4_202609080303.zip   （MXGTV + 版本号 + 北京时间 2026yyMMddHHmm）
#
# 用法：
#   ./release.sh                      # 仅打包到 release/ 目录
#   GITHUB_TOKEN=xxx ./release.sh -r  # 打包 + 创建 GitHub Release + 上传资产
#   -r / --release                    上传到 GitHub Release
#
# 依赖：php、git、zip、unzip、curl、python3
# =============================================================================
set -euo pipefail
cd "$(dirname "$0")"

# --- 1. 读取版本号 -----------------------------------------------------------
VER=$(php -r '$_=include "version.php"; exit(0);' 2>/dev/null || true)
VER=$(php -r '$_=include "version.php"; echo ltrim($_["version"], "v");')
FULLVER=$(php -r '$_=include "version.php"; echo $_["version"];')
BRANCH=$(php -r '$_=include "version.php"; echo $_["branch"] ?? "main";')
COMMIT=$(php -r '$_=include "version.php"; echo $_["commit"] ?? "";')

# --- 2. 时间戳（北京时间）--------------------------------------------------
TS=$(TZ=Asia/Shanghai date +%Y%m%d%H%M)
ZIP="release/MXGT_v${VER}_${TS}.zip"
mkdir -p release

# --- 3. 打包（只含 git 追踪源码，排除 data.db 与 release 自身）------
echo ">> 打包中..."
git ls-files | grep -v '^db/data\.db$' | grep -v '^release/' | zip -@ "$ZIP" >/dev/null
COUNT=$(unzip -Z1 "$ZIP" | wc -l)
SIZE=$(du -h "$ZIP" | cut -f1)
echo ">> 已生成: $ZIP"
echo ">> 版本: $FULLVER | 分支: $BRANCH | 提交: $COMMIT"
echo ">> 文件数: $COUNT | 大小: $SIZE"

# 未指定 -r 则到此结束
if [[ "${1:-}" != "-r" && "${1:-}" != "--release" ]]; then
    echo ">> 完成（加上 -r 可同时上传到 GitHub Release）"
    exit 0
fi

# --- 4. 创建 GitHub Release 并上传资产 --------------------------------------
TOKEN="${GITHUB_TOKEN:-}"
if [ -z "$TOKEN" ]; then
    echo "!! 缺少 GITHUB_TOKEN，跳过上传" >&2
    exit 1
fi

REPO="${MXGT_REPO:-ssmhdssmhd/MXGT}"
TAG="v${VER}"
echo ">> 创建 Release: $REPO $TAG"

BODY="MXGT ${FULLVER} 发布包\n- 发布包: $(basename "$ZIP")\n- 构建时间: $(TZ=Asia/Shanghai date '+%Y-%m-%d %H:%M' 北京时间)\n- 版本号格式: MXGTV.${VER} ${TS}"
BODY_JSON=$(printf '%s' "$BODY" | python3 -c 'import sys,json;print(json.dumps({"tag_name":"v'+$VER+'","name":"v'+$VER+'","body":sys.stdin.read(),"draft":false,"prerelease":false}))')

REL=$(curl -sk -X POST \
    -H "Authorization: token $TOKEN" \
    -H "Accept: application/vnd.github+json" \
    "https://api.github.com/repos/$REPO/releases" \
    -d "$BODY_JSON")
RELEASE_ID=$(printf '%s' "$REL" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("id",""))')

if [ -z "$RELEASE_ID" ]; then
    echo "!! 创建 Release 失败：" >&2
    printf '%s' "$REL" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("message",""))' >&2
    exit 1
fi

echo ">> 上传资产 (Release #$RELEASE_ID)..."
curl -sk -X POST \
    -H "Authorization: token $TOKEN" \
    -H "Content-Type: application/zip" \
    -H "Accept: application/vnd.github+json" \
    "https://uploads.github.com/repos/$REPO/releases/$RELEASE_ID/assets?name=$(basename "$ZIP")" \
    --data-binary @"$ZIP" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(">> 已上传:",d.get("name"),d.get("size"),"bytes")'

echo ">> 下载链接: https://github.com/$REPO/releases/download/$TAG/$(basename "$ZIP")"