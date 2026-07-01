#!/usr/bin/env bash

# Exit immediately if a command exits with a non-zero status
set -e

# Terminal colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
NC='\033[0m' # No Color

clear
echo -e "${BLUE}================================================================${NC}"
echo -e "${BLUE}       Janus Gateway: AWS EKS & Antigravity integration Demo    ${NC}"
echo -e "${BLUE}================================================================${NC}"
echo -e ""
echo -e "${CYAN}[1/4] Architecture Overview:${NC}"
echo -e "  * Gateway Facade: Deployed under namespace ${GREEN}janus${NC} on AWS EKS cluster."
echo -e "  * SSL/TLS Endpoint: ${GREEN}https://janus.13.134.88.9.nip.io/sse${NC} (Let's Encrypt)"
echo -e "  * Authentication: Secure Bearer Token validation."
echo -e "  * Downstream Services: Dynamic routing to LCH clearing and Treasury yield APIs."
echo -e ""
sleep 2

echo -e "${CYAN}[2/4] Workspace MCP Configuration (.agents/mcp_config.json):${NC}"
echo -e "Showing local workspace agent config that registers Janus as a remote server..."
echo -e ""
echo -e "${YELLOW}"
cat .agents/mcp_config.json
echo -e "${NC}"
sleep 2

echo -e "${CYAN}[3/4] Triggering Antigravity CLI agent session...${NC}"
echo -e "The agent will (all via the single governed gateway):"
echo -e "  1. Automatically load the ${GREEN}janus-gateway${NC} remote tools via EKS."
echo -e "  2. Activate the ${GREEN}lch-collateral-reporting${NC} governed compliance skill."
echo -e "  3. Query LCH collateral + ${GREEN}US Treasury, Bank of England, ECB FX, Eurostat${NC} deterministically."
echo -e "  4. Translate the multi-currency portfolio to GBP and compile a cross-currency margin audit."
echo -e ""
echo -e "${MAGENTA}Running: agy --print \"Generate a cross-currency collateral & multi-jurisdiction rate audit for MEM-LCH-002 using the lch-collateral-reporting skill.\"${NC}"
echo -e ""

# Execute agy command
agy --print "Generate a cross-currency collateral valuation and multi-jurisdiction rate audit for LCH member MEM-LCH-002 using the lch-collateral-reporting skill. Use the LCH, US Treasury, Bank of England, ECB FX, and Eurostat tools via the janus-gateway, and consolidate the portfolio into a GBP reporting value."

echo -e ""
echo -e "${BLUE}================================================================${NC}"
echo -e "${GREEN}✓ Demo completed successfully!${NC}"
echo -e "${BLUE}================================================================${NC}"
echo -e "How to run interactively in the TUI:"
echo -e "  1. Start the CLI in this directory: ${CYAN}agy${NC}"
echo -e "  2. Type ${CYAN}/mcp${NC} to view registered Janus gateway tools."
echo -e "  3. Ask the agent: ${CYAN}\"Generate collateral report for MEM-LCH-002\"${NC}"
echo -e "${BLUE}================================================================${NC}"
