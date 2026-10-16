# Competitor fact sheet, checked 2026-10-16

The source for every statement about another product on `/compare` and its
pages (`frontend/src/site/compare/`). Each fact was read on the vendor's own
page on the date above; third-party sources are marked. A fact that is not
in this file does not go on a page. Re-check every row before a page's
`updated` date moves, and at least quarterly.

Rules the pages keep (UK comparative advertising: CAP Code section 3, and the
Business Protection from Misleading Marketing Regulations 2008): like for
like (hosted against hosted), verifiable (a link per fact), no competitor
price unless the vendor publishes one, no logos or screenshots of their
products, no "best". OpenV's own limits are stated as plainly as its
strengths. About another product the pages state only what its vendor
says it does; "we could not find X" stays in this file and never becomes
"they lack X" on a page.

Scope: tools a team would use as a hosted service instead of hosted OpenV.
Self-hosted-only and open-source tools are out of scope by decision.

## IBM DOORS Next (IBM Engineering Lifecycle Management)

| Item | Fact | Source |
|---|---|---|
| Products | DOORS Next is IBM's web-based requirements tool, also part of the ELM suite; DOORS (classic, 9.7.x) remains for high-compliance work | https://www.ibm.com/products/requirements-management |
| Lifecycle | DOORS Next 7.0.3 left standard support on 30 Sep 2026; extended support to 30 Sep 2028. Newer releases (7.1, 7.2) exist | https://www.ibm.com/support/pages/node/7031968 |
| Price | Quote only | https://www.ibm.com/products/engineering-lifecycle-management/pricing |
| Hosted | ELM as SaaS, single-tenant, 99.9% SLA, two tiers by user count | https://www.ibm.com/products/engineering-lifecycle-management/pricing |
| Self-managed | Perpetual licence, customer-run | same |
| Traceability | Suspect / valid / invalid link status; streams, baselines, global configurations | https://www.ibm.com/docs/SSYMRC_7.2/com.ibm.jazz.vvc.doc/topics/c_linkval.html |
| Tests | Separate product, IBM Engineering Test Management, in the ELM suite | https://www.ibm.com/products/ibm-engineering-test-management |
| ReqIF | Import and export (.reqif/.reqifz); CSV and Excel in and out; Word import | https://www.ibm.com/docs/en/SSYMRC_7.0.2/com.ibm.rational.rrm.help.doc/topics/t_import_export.html |
| ReqIF limit | A ReqIF package cannot migrate several components or streams at once | https://ibm.com/support/knowledgecenter/SSYMRC_7.0.2/com.ibm.rational.rrm.help.doc/topics/t_reqif_intro.html |
| AI | Requirement quality scoring and rewording suggestions, natural-language query, summaries; IBM says experts approve the quality suggestions; model not named | https://www.ibm.com/products/requirements-management |
| Variants | Global configuration management for variants and product lines | https://www.ibm.com/downloads/documents/us-en/16951f139ce170df |
| Safety | Says it supports ASPICE, ISO 26262, DO-178C; no TÜV certificate or qualification kit found | https://www.ibm.com/products/requirements-management |
| E-signatures | Listed for DOORS classic; not found for DOORS Next | https://www.ibm.com/products/requirements-management |
| Trial | Free trial linked, length not stated | https://www.ibm.com/products/engineering-lifecycle-management |

## Jama Connect (Jama Software)

| Item | Fact | Source |
|---|---|---|
| Product | One product with Creator, Stakeholder, Reviewer and Test Runner licence types; add-ons Jama Connect Advisor and Interchange | https://www.jamasoftware.com/pricing |
| Price | Quote only; the pricing page says hosting and reviewer licences are free | https://www.jamasoftware.com/pricing |
| Hosted | Jama Connect Cloud (Jama-run SaaS) and AWS GovCloud (US). The second reviewer found no mention of Customer Validated Cloud on this page, so it is not used | https://www.jamasoftware.com/deployment-options/ |
| Self-managed | Jama Connect Self-Hosted on Kubernetes (Replicated KOTS), air-gap possible | https://support.jamasoftware.com/hc/en-us/articles/24072328211085 |
| Traceability | Custom relationship types and rules, suspect links, impact analysis, baseline comparison | https://help.jamasoftware.com/ah/en/manage-content/baselines/compare-baselines.html |
| Tests | Built in: test cases, runs, cycles | https://help.jamasoftware.com/ah/en/test/test-runs.html |
| ReqIF | Through Jama Connect Interchange, naming DOORS, DOORS Next, Codebeamer, Polarion as partners | https://www.jamasoftware.com/media/2024/06/jama-connect-interchange-for-ReqIF-datasheet.pdf |
| Word, Excel | Word and Excel import and export | https://support.jamasoftware.com/hc/en-us/sections/47011460363533 |
| AI | Advisor: INCOSE/EARS checks and rewrites, drafts test cases; engineers accept suggestions; paid add-on for cloud customers; model not named | https://help.jamasoftware.com/ah/en/jama-connect-advisor-.html |
| Reviews | Review Center with moderator, reviewer, approver roles; signing re-authenticates; Jama does not claim Part 11 compliance on its own | https://www.jamasoftware.com/media/2023/12/jama-connect-and-fda-21-cfr-part-11.pdf |
| Safety | TÜV SÜD certificate Z10 096476 0002 Rev. 02 was valid until 31 Mar 2026 (expired as of this check; do not state as current) | https://www.jamasoftware.com/media/2024/01/tuv-sud-certificate-26.pdf |
| Frameworks | Frameworks for medical devices (IEC 62304, ISO 14971, ISO 13485) and airborne systems (DO-178C, DO-254, ARP4754) | https://www.jamasoftware.com/solution-overview/jama-connect-airborne-systems-solution-overview/ |
| Trial | Free trial, length not stated; no free tier | https://www.jamasoftware.com/jama-connect-free-trial/ |
| Ownership | Francisco Partners announced its acquisition of Jama in March 2024 | https://www.franciscopartners.com/media/francisco-partners-to-acquire-jama-software-for-12b |

## Siemens Polarion

| Item | Fact | Source |
|---|---|---|
| Products | Self-managed Polarion ALM, REQUIREMENTS, QA; cloud Polarion X. (The plans page showed no plan list on the second check, so plan names are not used) | https://www.siemens.com/en-us/products/polarion/ |
| Price | No price published; the Polarion X plans page offers Contact Sales and a trial (second check, 2026-10-16) | https://www.siemens.com/en-us/products/polarion/polarion-x/plans-pricing/ |
| Hosted | Polarion X, Siemens-hosted SaaS with everything in on-premises Polarion | https://www.siemens.com/en-us/products/polarion/polarion-x/ |
| Traceability | Custom link roles, suspect links, document and project baselines | https://docs.plm.automation.siemens.com/content/polarion/20/help/en_US/user_and_administration_help/user_guide/work_with_documents/document_baselines.html |
| Tests | Test management is part of Polarion ALM and Polarion QA (the edition-by-edition detail was not confirmed on the second check, so it is not used) | https://www.siemens.com/en-us/products/polarion/ |
| ReqIF | Built-in ReqIF exchange; Word and Excel round trip; Word and PDF export | https://www.siemens.com/en-us/products/polarion/requirements/ |
| AI | Polarion Copilot, an add-on available on Polarion X Standard, Advanced, Automotive and Premium (INCOSE checks, similar items, consistency) | https://blogs.sw.siemens.com/polarion/polarion-x-2512-whats-new-and-noteworthy/ |
| E-signatures | Stakeholders can e-sign specifications as reviewed or approved | https://www.siemens.com/en-us/products/polarion/requirements/ |
| Variants | Polarion Variant Configurator, which replaces Polarion VARIANTS in release 2606 | https://blogs.sw.siemens.com/polarion/polarion-alm-2606-whats-new-and-noteworthy/ |
| Safety | TÜV Nord qualification for IEC 61508 / ISO 26262 (2012 post) and SGS TÜV Saar report for ISO 26262:2018 (2019 post); no version named, so not stated on pages | https://blogs.sw.siemens.com/polarion/sgs-tuv-saar-certification-and-iso-262622018/ |
| Trial | Free trial: Polarion X in the cloud, or a download of self-managed Polarion; length not stated | https://www.siemens.com/en-us/products/polarion/trials/ |

## ReqView (Eccam s.r.o.)

| Item | Fact | Source |
|---|---|---|
| Editions | FREE, PRO, TEAM, ENTERPRISE; v2.22.4 | https://www.reqview.com/pricing/ |
| Price | PRO €440 and TEAM €600 per user per year; ENTERPRISE €1,800 per floating client per year, minimum 3; subscriptions only; VAT not stated | https://www.reqview.com/pricing/ |
| Hosting | Desktop app with local storage; projects on a shared or cloud drive or in Git/SVN; a browser version at desktop.reqview.com; no vendor-hosted project data described | https://www.reqview.com/doc/faqs/ |
| Collaboration | Shared drive or Git/SVN; editing locks a document | https://www.reqview.com/doc/git-collaboration/ |
| Traceability | Custom link types, suspect links, history and version compare; tagged Git commits as baselines | https://www.reqview.com/doc/requirements-traceability-links/ |
| Tests | Test cases as linked documents, traceability matrix, verified/passed percentages; test runs not described | https://www.reqview.com/doc/example-project/ |
| ReqIF | Import and export; Word, Excel/CSV import; DOCX, XLSX, PDF, HTML, CSV export | https://www.reqview.com/doc/import-reqif/ |
| Reviews | Comments and a review table; approval by status attribute; e-signatures not mentioned | https://www.reqview.com/doc/review-document/ |
| Free | FREE edition: 1 document, 150 objects; 14-day trial | https://www.reqview.com/pricing/ |
| Platforms | Windows 11+, macOS 15+ (Apple Silicon), Ubuntu 22.04+, Debian 12+; web app in Chrome, Edge, Firefox | https://www.reqview.com/download/ |

## Jira and Confluence (Atlassian)

| Item | Fact | Source |
|---|---|---|
| Plans | Free (up to 10 users), Standard, Premium, Enterprise | https://www.atlassian.com/software/jira/pricing |
| Price | Published per user per month; read from page data, not confirmed as displayed, so not quoted on pages | same |
| Data Center | No longer sold to new customers since 30 Mar 2026; end of life 28 Mar 2029 | https://www.atlassian.com/licensing/data-center-end-of-life |
| Requirements | No "Requirement" work type by default; custom work types and link types | https://support.atlassian.com/jira-cloud-administration/docs/what-are-issue-types/ |
| Baselines, matrix | No Atlassian page describes native requirement baselines or a trace matrix; Marketplace apps (R4J) sell them | https://marketplace.atlassian.com/apps/1213064/easerequirements-requirements-management-for-jira-r4j |
| Tests | Through Marketplace apps such as Xray and Zephyr | https://marketplace.atlassian.com/apps/1211769/xray-test-management-for-jira |
| ReqIF | No native support found; third-party apps only | https://marketplace.atlassian.com/apps/1220602/reqif4jira |
| E-signatures | Through apps, e.g. Vera Electronic Signatures for Jira | https://marketplace.atlassian.com/apps/1220599/vera-electronic-signatures-for-jira |
| AI | Rovo on Standard, Premium and Enterprise: plain-language search, drafting and summaries, suggested child work items, likely duplicates | https://support.atlassian.com/organization-administration/docs/understand-atlassian-intelligence-features-in-products |

## Others (hub page only)

| Tool | Price | Hosted | ReqIF | Tests built in | Source |
|---|---|---|---|---|---|
| PTC Codebeamer | Quote only | Codebeamer+ (SaaS) | Import and export | Yes | https://www.ptc.com/en/products/codebeamer |
| Visure Requirements ALM | Quote only | Visure Cloud | Import and export | Yes | https://visuresolutions.com/pricing |
| Perforce ALM (formerly Helix ALM) | Quote only | Not stated | Import; export unverified | Yes | https://www.perforce.com/products/helix-alm ; rename: https://www.perforce.com/blog/alm/helix-alm-now-perforce-alm |
| Modern Requirements4DevOps | Quote only | Runs inside Azure DevOps | Unverified | Unverified | https://www.modernrequirements.com/pricing/ |
| Matrix Req (Matrix One) | Quote only | Included in subscription | Unverified | Partly | https://matrixone.health/get-pricing |
| Inflectra SpiraTeam | Published (USD, per concurrent-user tier) | Cloud hosted by Inflectra | Import; export unverified | Yes | https://inflectra.com/Purchase/Price-List.aspx |

## OpenV (for the same rows)

From the product and its own site copy (`frontend/src/landing/content.ts`,
`frontend/src/site/topics/`), which were checked claim by claim against the
code on 2026-10-13/14.
