#!/usr/bin/env bash
# =====================================================================
# YonaCode ($YGO) Ultra-Fast Ledger Snapshot Bootstrap Script
# Usage:
#   curl -sSL https://raw.githubusercontent.com/duyvn878-ui/YonaCode/main/sync_ledger_fast.sh | bash
# =====================================================================

set -e

GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${CYAN}"
echo "======================================================================="
echo "       🚀 YONACODE ($YGO) ULTRA-FAST LEDGER SNAPSHOT BOOTSTRAP         "
echo "======================================================================="
echo -e "${NC}"

TARGET_DIR="${1:-$(pwd)}"
cd "$TARGET_DIR"

echo -e "${YELLOW}[1/3] Downloading official Bootstrap Ledger Snapshot (YonaCode_Ledger_Data.zip)...${NC}"
curl -sSL -o YonaCode_Ledger_Data.zip "https://github.com/duyvn878-ui/YonaCode/releases/download/v2.0.0/YonaCode_Ledger_Data.zip"

echo -e "${YELLOW}[2/3] Extracting RocksDB state database into ./node/scl/...${NC}"
mkdir -p node/scl
unzip -o YonaCode_Ledger_Data.zip -d "$TARGET_DIR" > /dev/null
rm -f YonaCode_Ledger_Data.zip

echo -e "${YELLOW}[3/3] Setting file permissions...${NC}"
chmod -R 755 node/scl 2>/dev/null || true

echo -e "${GREEN}"
echo "======================================================================="
echo "   🎉 ULTRA-FAST LEDGER BOOTSTRAP COMPLETED SUCCESSFULLY!              "
echo "   Database installed at: ${TARGET_DIR}/node/scl"
echo "   You can now launch YonaCode / scl_server to sync immediately!"
echo "======================================================================="
echo -e "${NC}"
