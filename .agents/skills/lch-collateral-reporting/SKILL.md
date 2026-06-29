---
name: lch-collateral-reporting
description: Generates governed, structured financial reports and collateral valuations for LCH Group members using the MCP API Gateway facade.
---

# LCH Ltd: Collateral Analysis & Reporting Skill

Use this skill when the user asks to analyze member collateral holdings, calculate haircuts, cross-reference Treasury rates, or compile formal compliance and margin valuation reports.

---

## 1. Trigger Conditions
Activate this skill automatically when user queries involve:
*   *"Show collateral holdings for member X"*
*   *"Generate a margin/collateral report"*
*   *"Calculate net collateral valuation after haircuts"*
*   *"Audit Treasury yields against collateral rates"*

---

## 2. Information Gathering Workflow
When triggered, follow this deterministic sequence to compile data. Do not skip steps or make up/hallucinate data points:

1.  **Retrieve Collateral Portfolio**:
    Call `lch_get_non_cash_collateral` with the specified `member_id` to get a list of active assets, ISIN codes, market values, and haircut percentages.
2.  **Retrieve Yield / Interest Baseline**:
    Call `ustreasury_get_avg_interest_rates` to check baseline interest rates. If checking currency translations, query `ustreasury_get_rates_of_exchange` for foreign exchange rates.
3.  **Perform Valuations**:
    For each asset, calculate:
    $$\text{Net Collateral Value} = \text{Market Value} \times \left(1 - \frac{\text{Haircut \%}}{100}\right)$$
    Accumulate totals grouped by asset type (e.g. Government Bonds) and currency.

---

## 3. Reporting and Document Generation Templates
Format all outputs as high-quality, professional markdown documents. Avoid casual phrasing. Use the following outline:

### Structure

# LCH Ltd: Member Collateral Valuation & Audit Report
**Date**: [Insert Current Date]  
**Member ID**: [Insert Member ID]  
**Status**: [ACTIVE | ACTION REQUIRED]

## 1. Executive Summary
Briefly summarize the clearing member's position. Highlight the total market value and total net collateral value (after haircuts).
> [!NOTE]
> Add a note validating if the net collateral valuation satisfies the clearing house's minimum margin threshold.

## 2. Collateral Portfolio Breakdown
Present a clean, markdown table summarizing the assets:

| Asset Name | ISIN | Type | Market Value (EUR) | Haircut | Net Collateral Value (EUR) |
| :--- | :--- | :--- | :--- | :---: | :--- |
| [Name] | [ISIN] | [Type] | [Value] | [H %] | [Calculated Net] |

## 3. Yield & Treasury Rates Verification
State the current average interest rate on U.S. Treasury marketable securities and verify if the yields warrant asset rebalancing.
*   **Treasury Average Rate**: [Rate %]
*   **Rate Audit Status**: [Verified / Out of bounds]

## 4. Compliance & Risk Statement
Add a standard compliance paragraph:
> *This report is API-mediated and dynamically generated from LCH Ltd downstream clearing systems. All calculations are audit-logged and deterministic.*
