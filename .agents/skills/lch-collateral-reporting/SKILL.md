---
name: lch-collateral-reporting
description: Generates governed, structured cross-currency collateral valuations and multi-jurisdiction rate audits for LCH Group members using the MCP API Gateway facade.
---

# LCH Ltd: Collateral Analysis & Reporting Skill

Use this skill when the user asks to analyze member collateral holdings, calculate haircuts, cross-reference interest rates, translate multi-currency collateral, or compile formal compliance and margin valuation reports.

---

## 1. Trigger Conditions
Activate this skill automatically when user queries involve:
*   *"Show collateral holdings for member X"*
*   *"Generate a margin/collateral report"*
*   *"Calculate net collateral valuation after haircuts"*
*   *"Audit collateral against UK/US/EU reference rates"*

---

## 2. Information Gathering Workflow
When triggered, follow this deterministic sequence to compile data. Do not skip steps or make up/hallucinate data points. Every figure must come from a tool call — all served through the single governed janus-gateway facade:

1.  **Retrieve Collateral Portfolio**:
    Call `lch_get_non_cash_collateral` with the specified `member_id` to get active assets, ISIN codes, market values, and haircut percentages.
2.  **Retrieve Multi-Jurisdiction Rate Baselines**:
    *   `ustreasury_get_avg_interest_rates` — U.S. Treasury marketable average rates.
    *   `boe_get_bank_rate` — the official UK Bank of England Bank Rate (CSV; take the latest value).
    *   `eurostat_get_hicp_inflation` (geo `EA`) — euro-area HICP annual inflation (real-value context for EUR sovereign collateral).
3.  **Retrieve Cross-Currency FX**:
    Call `fx_get_reference_rates` (base `EUR`, symbols `USD,GBP`) for the ECB euro reference rates; use them to translate the multi-currency portfolio into a single GBP reporting value. `ecb_get_usd_eur_rate` may corroborate the USD/EUR rate.
4.  **Perform Valuations**:
    For each asset, calculate Net Collateral Value = Market Value × (1 − Haircut% / 100). Accumulate totals grouped by asset type and currency, then convert to a common **GBP** reporting value using the ECB FX rates from step 3.

---

## 3. Reporting and Document Generation Templates
Format all outputs as high-quality, professional markdown. Avoid casual phrasing. Use the following outline:

### Structure

# LCH Ltd: Cross-Currency Collateral Valuation & Multi-Jurisdiction Rate Audit
**Date**: [Insert Current Date]
**Member ID**: [Insert Member ID]
**Reporting currency**: GBP · **Status**: [ACTIVE | ACTION REQUIRED]

## 1. Executive Summary
Summarize the member's position: total market value, total net collateral (after haircuts), and the consolidated GBP value after FX translation.
> [!NOTE]
> State whether the net collateral valuation satisfies the clearing house's minimum margin threshold. Note that the margin *requirement* (IM/VM) is not exposed by the available tools.

## 2. Collateral Portfolio Breakdown
| Asset Name | ISIN | Type | Ccy | Market Value | Haircut | Net Collateral Value |
| :--- | :--- | :--- | :---: | ---: | :---: | ---: |
| [Name] | [ISIN] | [Type] | [Ccy] | [Value] | [H %] | [Calculated Net] |

## 3. Cross-Currency Translation (ECB reference rates)
State the ECB EUR→USD and EUR→GBP rates used, then present each position and the portfolio total converted to **GBP**.
*   **EUR/USD**: [rate] · **EUR/GBP**: [rate] (source: ECB via gateway)
*   **Consolidated collateral (GBP)**: [value]

## 4. Multi-Jurisdiction Rate & Macro Audit
| Reference | Value | Source (via gateway) |
| :--- | :---: | :--- |
| US Treasury avg (Notes) | [%] | U.S. Treasury |
| UK Bank Rate | [%] | Bank of England |
| Euro-area HICP inflation | [%] | Eurostat |

Comment on the collateral's coupon vs. the relevant reference rate and any inflation drag on the EUR sovereign position.

## 5. Compliance & Risk Statement
> *This report is API-mediated and dynamically generated. Collateral is sourced from LCH Ltd clearing systems; rates and FX are sourced from the U.S. Treasury, the Bank of England, the ECB, and Eurostat — all mediated through the janus governed gateway. All calls are audit-logged and deterministic; the LLM never touches downstream credentials.*
