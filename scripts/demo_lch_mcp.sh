#!/usr/bin/env bash

# Exit immediately if a command exits with a non-zero status
set -e

# Terminal colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0;0m' # No Color

echo -e "${BLUE}================================================================${NC}"
echo -e "${BLUE}          LCH Group Concorde MCP Gateway Stdio Demo             ${NC}"
echo -e "${BLUE}================================================================${NC}"

# Ensure database exists and is seeded
if [ ! -f "mcp-gateway.db" ]; then
    echo -e "${YELLOW}[1/4] SQLite database not found. Compiling gateway and seeding...${NC}"
else
    echo -e "${GREEN}[1/4] SQLite database detected. Rebuilding binaries...${NC}"
fi

go build -o mcp-gateway main.go
echo -e "${GREEN}✓ mcp-gateway compiled successfully.${NC}"

# Define the client token
export MCP_GATEWAY_TOKEN="lch_member_test_token_889"
export DATABASE_PATH="./mcp-gateway.db"

echo -e "\n${YELLOW}[2/4] Testing Tool Discovery (tools/list)...${NC}"
echo -e "Sending JSON-RPC 'tools/list' request to gateway stdin..."

# Construct tools/list JSON-RPC payload
DISCOVER_PAYLOAD='{"jsonrpc":"2.0","method":"tools/list","id":1}'

# Execute gateway in stdio mode, pipe payload, and format JSON output
DISCOVER_RES=$(echo "$DISCOVER_PAYLOAD" | ./mcp-gateway -stdio 2>/dev/null)

echo -e "${CYAN}--- JSON-RPC Request Sent ---${NC}"
echo "$DISCOVER_PAYLOAD"
echo -e "\n${GREEN}--- JSON-RPC Response Received ---${NC}"
echo "$DISCOVER_RES" | grep -v "Session" || true

echo -e "\n${YELLOW}[3/4] Testing Tool Execution (tools/call -> lch_get_non_cash_collateral)...${NC}"
echo -e "Sending JSON-RPC 'tools/call' request with parameters..."

# Construct tools/call JSON-RPC payload
CALL_PAYLOAD='{"jsonrpc":"2.0","method":"tools/call","params":{"name":"lch_get_non_cash_collateral","arguments":{"member_id":"MEM-LCH-002"}},"id":2}'

# Execute gateway in stdio mode and pipe payload
CALL_RES=$(echo "$CALL_PAYLOAD" | ./mcp-gateway -stdio 2>/dev/null)

echo -e "${CYAN}--- JSON-RPC Request Sent ---${NC}"
echo "$CALL_PAYLOAD"
echo -e "\n${GREEN}--- JSON-RPC Response Received ---${NC}"
echo "$CALL_RES" | grep -v "Session" || true

echo -e "\n${YELLOW}[4/4] Python SDK / Antigravity integration snippet:${NC}"
cat << 'EOF'
from antigravity_sdk import Agent, ToolRegistry

# 1. Establish SSE listener stream with token auth
registry = ToolRegistry.from_mcp_sse(
    url="http://localhost:8899/sse",
    headers={"Authorization": "Bearer lch_member_test_token_889"}
)

# 2. Equip AI Agent with dynamically loaded LCH tools
agent = Agent(
    name="LCH Collateral Analyst",
    instructions="Process and summarize member non-cash collateral data.",
    tools=registry.list_tools()
)
EOF

echo -e "${BLUE}================================================================${NC}"
echo -e "${GREEN}Demo finished successfully. Run this script to present to architects.${NC}"
echo -e "${BLUE}================================================================${NC}"
