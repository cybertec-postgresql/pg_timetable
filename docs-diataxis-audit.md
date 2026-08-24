# Diátaxis Audit — `docs/` (pg_timetable)

Date: 2026-08-19
Scope: all files in `docs/` plus `mkdocs.yml` nav. Framework: [Diátaxis](https://diataxis.fr).

## 1. Executive Summary

The doc set is unusually mature for its size: most files are already single-purpose and correctly
labeled by nav section (`Tutorial` / `How-to Guides` / `Reference` / `Concept`). The secret-store
trio (`how-to-use-secret-store.md` + `secret_store.md` + `explanation-secret-store-security-model.md`)
is a **gold-standard example** of quadrant separation with clean cross-links and should be the
template for future feature docs.

Two structural problems dominate the findings:

1. **Tutorial-starved, how-to/reference-heavy.** 1 tutorial vs. 7 how-to guides vs. 7 reference
   docs. Three major features added recently (YAML chains, secret store, OpenTelemetry) have
   how-to + reference coverage but **no tutorial** extends the onboarding path past the original
   single-task-chain lesson.
2. **Reference content duplicated across 2–3 files instead of linked once.** BUILTIN task
   parameter shapes and YAML chain examples are documented near-identically in
   `how-to-write-yaml-chains.md`, `yaml-format.md`, and `reference-commands-tasks-chains.md`,
   creating drift risk (already three sources of truth for the same JSON schema).

No file is catastrophically mis-classified. Findings below are ordered by severity, not by file order.

## 2. Inventory & Classification

| File | Nav section | Quadrant (confidence) | Notes |
|---|---|---|---|
| `tutorial-first-chain.md` | Tutorial | **Tutorial** (high) | Compliant: sequential, "we'll", no branching, minimal explanation. Model example. |
| `installation.md` | How-to | **How-to** (high) | Compliant. Contains one explanatory admonition (pgcrypto note) — acceptable, one paragraph. |
| `how-to-schedule-common-jobs.md` | How-to | **How-to** (high) | Compliant, pure example catalog, links out for full parameter reference. |
| `samples.md` | How-to | **Reference/How-to blur** (medium) | Descriptive voice ("This sample demonstrates...") not imperative; reads as an annotated example index (Reference-leaning) rather than goal-directed steps. Placement under "How-to Guides" is defensible but weak. |
| `migration.md` | How-to | **How-to** (high) | Minor explanation leak (rationale for autonomous-task caveat) — acceptable, single sentence. |
| `how-to-write-yaml-chains.md` | How-to | **How-to** (medium) | Longest doc (313 lines). "Overview" bullet list of benefits borders on teaching/marketing framing. "Task Parameters" section re-documents BUILTIN JSON shapes already owned by `reference-commands-tasks-chains.md`. Violates "How-to IS NOT a showcase of capabilities." |
| `how-to-use-secret-store.md` | How-to | **How-to** (high) | Well-scoped numbered steps. "Recommendations" section is advisory/opinion (explanation-flavored) embedded in a how-to — minor blur, low severity. |
| `how-to-enable-opentelemetry.md` | How-to | **How-to** (high) | Compliant, scenario-based quick-starts, links to reference for exhaustive detail. Model example alongside the tutorial. |
| `api.md` | Reference | **Reference** (high) | Compliant, austere, matches actual `api.go` routes (`/liveness`, `/readiness`, `/startchain`, `/stopchain`). |
| `reference-cli-options.md` | Reference | **Reference** (high) | Compliant flag dump, verified against `cmdparser.go`. |
| `reference-commands-tasks-chains.md` | Reference | **Reference** (medium) | Mostly compliant. One instructional aside: *"You can temporarily skip a single step... by toggling the `live` flag"* + SQL snippet — narrative/how-to voice inside reference (mild Anti-Pattern 3). |
| `yaml-format.md` | Reference | **Reference** (high) | Compliant field-mapping tables and validation rules. Examples overlap `how-to-write-yaml-chains.md` (see §3.2). |
| `database_schema.md` | Reference | **Reference** (high) | Compliant; correctly delegates `add_job()` cross-link from the how-to instead of duplicating it. ER diagram is stale (see §3.3). |
| `secret_store.md` | Reference | **Reference** (high) | Dense but compliant — no instructions, only description. |
| `reference-opentelemetry.md` | Reference | **Reference** (high) | Compliant, good split from the how-to (facts vs. procedures). |
| `background.md` | Concept | **Explanation** (medium) | Thin: mostly an external blog-post link list plus two sentences. Valid quadrant, low information density — a completeness gap more than a violation. |
| `explanation-scheduling-model.md` | Concept | **Explanation** (high) | Compliant, concise, correctly links to reference instead of listing fields. |
| `explanation-secret-store-security-model.md` | Concept | **Explanation** (high) | Compliant discursive threat-model discussion; dense/technical but no procedures. |
| `index.md` | Home | **Mixed (README pattern)** (expected) | Landing page mixing feature list, links, contributing/support/authors. Per Diátaxis guidance for README files, this is an acceptable pattern *if* it stays brief and links out — it does, but "Learn More" under-represents the doc set (see §3.4). |
| `license.md` | Developer | N/A (legal) | Snippet-includes `LICENSE`. Not a Diátaxis quadrant; fine as-is. |
| `requirements-doc.txt` | — | N/A (build tooling) | pip requirements for `mkdocs`/`mike`. Not documentation content; misplaced inside the content tree (see §3.5). |
| `timetable_schema.png` | Reference (embedded) | N/A (asset) | Stale relative to schema text (see §3.3). |

## 3. Findings, Ranked by Severity

### 3.1 [P0] Tutorial coverage does not track feature growth

Nav counts: **1 Tutorial**, **7 How-to**, **7 Reference**, **3 Explanation**. The single tutorial
(`tutorial-first-chain.md`) teaches a one-task SQL chain via `add_job()`. Three feature areas added
since — YAML-authored chains, the secret store, OpenTelemetry — each got a how-to and reference
(two of three got an explanation too), but a first-time user adopting any of them has no guided
lesson, only "already-competent user" documentation. This is the classic Diátaxis imbalance:
reference/how-to-heavy, tutorial-poor.

**Recommendation:** Add a second tutorial, e.g. "Your First YAML Chain" (learning-oriented,
narrower than `how-to-write-yaml-chains.md`, no branching, single worked example ending in a
visible result). Do not add tutorials for OTel or the secret store unless onboarding data shows
new users adopting them without prior chain experience — those are advanced/operator features
better served by the existing how-to guides.

### 3.2 [P0] Triplicated BUILTIN/YAML parameter examples

The JSON parameter shape for `SQL`, `PROGRAM`, and each `BUILTIN` command (`Sleep`, `Log`,
`SendMail`, `Download`, `CopyFromFile`, …) is fully documented in
`reference-commands-tasks-chains.md` ("Parameter value format"). `how-to-write-yaml-chains.md`
re-documents the same shapes under "Task Parameters" with YAML syntax, and `yaml-format.md`
partially repeats them again under "Examples". Three sources of truth for one schema.

**Recommendation:** Keep the parameter *value schema* solely in
`reference-commands-tasks-chains.md`. In `how-to-write-yaml-chains.md`, replace the "Task
Parameters" section body with 1–2 short YAML examples plus a link:
*"For every BUILTIN command's parameter shape, see [Parameter value format](reference-commands-tasks-chains.md#parameter-value-format)."*
This is the same delegation pattern the file already uses correctly for the minimal example
(links to `yaml-format.md#simple-sql-job`).

### 3.3 [P1] ER diagram (`timetable_schema.png`) is stale (fixed, file removed)

`timetable_schema.png` is 10 months old; `secret_store.md` and the `timetable.secret` migration
(`00820`) are recent additions. The diagram almost certainly predates the secret store table and
no longer reflects the schema described in `database_schema.md` and `secret_store.md`.

**Recommendation:** Regenerate the ER diagram to include `timetable.secret`, or add a note in
`database_schema.md` scoping the diagram to the core schema and pointing to `secret_store.md` for
the table added later.

### 3.4 [P1] `index.md` "Learn More" under-represents the doc set

Only four links (one Tutorial, one How-to, one Reference, one Concept) are surfaced, out of 18
quadrant documents. A new visitor landing on the homepage sees no mention of YAML chains, the
secret store, or OpenTelemetry at all.

**Recommendation:** Either trust the nav sidebar entirely and shrink "Learn More" to a single
sentence pointing at the nav, or expand it to one representative link per major feature area
(chain scheduling, YAML authoring, secrets, observability) rather than one per quadrant.

### 3.5 [P2] Filename convention inconsistency

Newer files use quadrant-prefixed names (`how-to-*`, `reference-*`, `explanation-*`,
`tutorial-*`); older files don't (`api.md`, `background.md`, `database_schema.md`,
`installation.md`, `migration.md`, `samples.md`, `secret_store.md`, `yaml-format.md`). The nav
labels compensate today, but the filename can no longer be used to infer quadrant, which makes
future audits and contributor navigation harder as the set grows.

**Recommendation:** Not urgent enough to justify a mass rename (breaks external links, git blame).
Adopt the `how-to-`/`reference-`/`explanation-`/`tutorial-` prefix for all *new* files going
forward; leave existing filenames alone.

### 3.6 [P2] `requirements-doc.txt` is build tooling, not content

Two-line pip requirements file (`mike`, `mkdocs-material`) lives inside the documentation content
tree. Harmless today (excluded from nav), but it clutters the docs audit surface and could
confuse a future glob-based doc tool.

**Recommendation:** Move to repo root as `docs-requirements.txt` or fold into an existing root
`requirements*.txt`/CI step.

### 3.7 [P2] Minor narrative leaks inside otherwise-compliant docs

- `reference-commands-tasks-chains.md`: *"You can temporarily skip a single step... by toggling
  the `live` flag"* + SQL snippet reads as a mini how-to embedded in reference. Reword to a
  neutral fact: *"Setting `live = FALSE` on a task row skips its execution without deleting it."*
  Keep the SQL as an illustrative example, not an instruction.
- `how-to-use-secret-store.md` "Recommendations" section (`Prefer .pgpass over ${secret:...}`,
  etc.) is advisory/opinion content. Low severity — how-to guides may state a preferred path — but
  if it grows, split into the existing `explanation-secret-store-security-model.md`.
- `how-to-write-yaml-chains.md` "Overview" bullet list of benefits is brief (4 bullets) but is
  "why use this feature" framing rather than "how to use it." Low severity; trim to one sentence
  or move to a future YAML explanation doc if one is created.

### 3.8 [P2] `samples.md` classification blur

Written in descriptive third person ("This sample demonstrates...") rather than imperative
how-to voice, and each entry is just a snippet-include of a `.sql` file. It functions more like an
annotated reference index than a goal-oriented guide, yet lives under "How-to Guides" in the nav.

**Recommendation:** No split needed — low stakes — but consider reframing entries as imperative
("Send an email when a check fails: ...") to match its How-to placement, or move it under
Reference as an "Examples" appendix if a future reorganization happens.

## 4. What's Already Working Well (do not disturb)

- **Secret store trio**: `how-to-use-secret-store.md` (How-to) → `secret_store.md` (Reference) →
  `explanation-secret-store-security-model.md` (Explanation), all cross-linked, no duplication.
  Use as the template for the next feature that needs docs.
- **OTel pair**: `how-to-enable-opentelemetry.md` and `reference-opentelemetry.md` cleanly split
  scenario-driven quick-starts from exhaustive flag/attribute tables, with a single link between
  them in each direction.
- **`database_schema.md` → `how-to-schedule-common-jobs.md`**: the how-to links to the reference's
  `add_job()` parameter table by anchor instead of re-listing parameters. Correct pattern.
- **`tutorial-first-chain.md`**: textbook-compliant tutorial — no branching, minimal explanation,
  visible result at every step, delegates alternatives ("For Docker or building from source, see
  Installation") instead of offering them inline.

## 5. Prioritized Action List

1. **P0** — Deduplicate BUILTIN/YAML parameter documentation: trim `how-to-write-yaml-chains.md`
   "Task Parameters" to link into `reference-commands-tasks-chains.md`.
2. **P0** — Write a second tutorial for YAML-authored chains to close the onboarding gap for a
   shipped, documented-but-unlearnable feature path.
3. **P1** — Regenerate/rescope `timetable_schema.png` relative to the secret store addition.
4. **P1** — Trim or expand `index.md` "Learn More" to match actual doc-set breadth.
5. **P2** — Reword the `live`-flag aside in `reference-commands-tasks-chains.md` to neutral voice.
6. **P2** — Relocate `requirements-doc.txt` out of `docs/`.
7. **P2** — Adopt quadrant-prefixed filenames for new docs only; no retroactive rename.
