# Docs Visual Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebrand the Notifycat mkdocs-material site: charcoal/white base, jade accent pair, Space Grotesk/Inter/JetBrains Mono, custom landing page, theme-aware SVG diagrams, framed media, and branded social cards.

**Architecture:** Everything is theme-layer work on mkdocs-material 9.5.39 — CSS custom properties per color scheme in one `extra.css`, a Jinja template override for the homepage, inline (snippet-included) SVGs so diagrams inherit CSS variables, and plugin/config changes in `mkdocs.yml`. No Go code.

**Tech Stack:** mkdocs-material 9.5.39, pymdownx (snippets, superfences), Material `social` plugin (`mkdocs-material[imaging]`), GitHub Pages via `.github/workflows/docs.yml`.

## Global Constraints

- **No indigo anywhere.** Banned by user.
- Color tokens (exact, from spec): white theme — bg `#ffffff`, surface `#f5f6f5`, ink `#161817`, accent `#0e8a5f`, accent-emphasis `#0b6b4a`; black theme — bg `#111413`, surface `#1a1e1c`, ink `#edf1ef`, accent `#34d399`, accent-emphasis `#6ee7b7`.
- Fonts: Space Grotesk (headings), Inter (body), JetBrains Mono (code).
- Jade only on: links, primary CTA, highlighted hero word, admonition edges (tip/note/info only — warning/danger stay semantic), diagram state-nodes, the logo bell.
- Verification command for every task: `mkdocs build --strict` must pass (run via the venv from Task 1).
- Commit after every task. Conventional Commits titles. No `BREAKING CHANGE` string anywhere in commit bodies. No Claude attribution footers.
- The spec is `docs/superpowers/specs/2026-07-07-docs-visual-redesign-design.md`.

---

### Task 1: Local toolchain + baseline build

**Files:** none committed (venv lives in the session scratchpad).

- [ ] **Step 1: Create venv and install Material**

```bash
python3 -m venv "$SCRATCHPAD/docs-venv"
"$SCRATCHPAD/docs-venv/bin/pip" install -q "mkdocs-material==9.5.39"
```

- [ ] **Step 2: Baseline strict build passes before any change**

Run: `cd /Users/pavlomaksymov/lab/notifycat && "$SCRATCHPAD/docs-venv/bin/mkdocs" build --strict`
Expected: `Documentation built` with zero warnings. If the baseline fails, stop and fix that first — it's not part of this redesign.

---

### Task 2: Brand foundation — tokens, fonts, chrome (`extra.css` + `mkdocs.yml`)

**Files:**
- Create: `docs/assets/stylesheets/extra.css`
- Modify: `mkdocs.yml`

**Interfaces:**
- Produces CSS custom properties `--nc-bg`, `--nc-surface`, `--nc-ink`, `--nc-accent`, `--nc-accent-strong`, `--nc-border` — every later task (homepage template, SVG diagrams) consumes these.
- Produces class `.nc-eyebrow` (mono label) used by the homepage template.

- [ ] **Step 1: Write `docs/assets/stylesheets/extra.css`**

```css
@import url("https://fonts.googleapis.com/css2?family=Space+Grotesk:wght@500;700&display=swap");

/* ---------- Brand tokens ---------- */
[data-md-color-scheme="default"] {
  --nc-bg: #ffffff;
  --nc-surface: #f5f6f5;
  --nc-ink: #161817;
  --nc-accent: #0e8a5f;
  --nc-accent-strong: #0b6b4a;
  --nc-border: #e4e6e5;

  --md-default-bg-color: var(--nc-bg);
  --md-default-fg-color: var(--nc-ink);
  --md-code-bg-color: var(--nc-surface);
  --md-typeset-a-color: var(--nc-accent);
  --md-accent-fg-color: var(--nc-accent-strong);
  --md-primary-fg-color: var(--nc-ink);
}

[data-md-color-scheme="slate"] {
  --nc-bg: #111413;
  --nc-surface: #1a1e1c;
  --nc-ink: #edf1ef;
  --nc-accent: #34d399;
  --nc-accent-strong: #6ee7b7;
  --nc-border: #2a302d;

  --md-default-bg-color: var(--nc-bg);
  --md-default-fg-color: var(--nc-ink);
  --md-code-bg-color: var(--nc-surface);
  --md-typeset-a-color: var(--nc-accent);
  --md-accent-fg-color: var(--nc-accent-strong);
  --md-primary-fg-color: var(--nc-ink);
}

/* Header/tabs follow the page background in both schemes */
.md-header,
.md-tabs {
  background-color: var(--nc-bg);
  color: var(--nc-ink);
  border-bottom: 1px solid var(--nc-border);
}
.md-header { box-shadow: none; }
.md-tabs { border-bottom: none; }
.md-tabs__link--active { color: var(--nc-accent); font-weight: 700; }
.md-header__button.md-logo svg { height: 1.4rem; width: auto; }

/* ---------- Typography ---------- */
.md-typeset h1,
.md-typeset h2,
.md-typeset h3,
.md-typeset h4 {
  font-family: "Space Grotesk", var(--md-text-font-family, sans-serif);
  font-weight: 700;
  letter-spacing: -0.02em;
  color: var(--nc-ink);
}
.md-typeset h1 { font-weight: 700; }

.nc-eyebrow {
  font-family: var(--md-code-font-family, monospace);
  font-size: 0.62rem;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--nc-accent);
  font-weight: 600;
}

/* ---------- Links & buttons ---------- */
.md-typeset a { font-weight: 600; }
.md-typeset .md-button--primary {
  background-color: var(--nc-accent);
  border-color: var(--nc-accent);
  color: var(--nc-bg);
  border-radius: 0.4rem;
  font-weight: 700;
}
.md-typeset .md-button--primary:hover {
  background-color: var(--nc-accent-strong);
  border-color: var(--nc-accent-strong);
  color: var(--nc-bg);
}
.md-typeset .md-button {
  border-radius: 0.4rem;
  border-color: var(--nc-border);
  color: var(--nc-ink);
}
.md-typeset .md-button:hover {
  border-color: var(--nc-accent);
  color: var(--nc-accent);
  background-color: transparent;
}

/* ---------- Admonitions: tip/note/info take the accent family ---------- */
.md-typeset :is(.admonition, details):is(.tip, .note, .info) {
  border-color: var(--nc-accent);
}
.md-typeset :is(.tip, .note, .info) > :is(.admonition-title, summary) {
  background-color: color-mix(in srgb, var(--nc-accent) 10%, transparent);
}
.md-typeset :is(.tip, .note, .info) > :is(.admonition-title, summary)::before {
  background-color: var(--nc-accent);
}

/* ---------- Media framing: screenshots & videos ---------- */
.md-typeset img,
.md-typeset video {
  border-radius: 0.5rem;
  border: 1px solid var(--nc-border);
}
[data-md-color-scheme="slate"] .md-typeset img,
[data-md-color-scheme="slate"] .md-typeset video {
  box-shadow: 0 4px 18px rgba(0, 0, 0, 0.45);
}
/* Inline SVG diagrams and emoji are not screenshots — no frame */
.md-typeset svg.nc-diagram,
.md-typeset img.twemoji {
  border: none;
  border-radius: 0;
  box-shadow: none;
}

/* ---------- Grid cards ---------- */
.md-typeset .grid.cards > :is(ul, ol) > li,
.md-typeset .grid > .card {
  border-radius: 0.6rem;
  border-color: var(--nc-border);
  background-color: var(--nc-surface);
}
.md-typeset .grid.cards > :is(ul, ol) > li:hover,
.md-typeset .grid > .card:hover {
  border-color: var(--nc-accent);
  box-shadow: none;
}

/* ---------- Mermaid (future diagrams) ---------- */
:root {
  --md-mermaid-font-family: var(--md-code-font-family, monospace);
}
[data-md-color-scheme="default"],
[data-md-color-scheme="slate"] {
  --md-mermaid-node-bg-color: var(--nc-surface);
  --md-mermaid-node-fg-color: var(--nc-ink);
  --md-mermaid-label-bg-color: var(--nc-bg);
  --md-mermaid-label-fg-color: var(--nc-ink);
  --md-mermaid-edge-color: var(--nc-accent);
}
```

- [ ] **Step 2: Update `mkdocs.yml` theme block**

Replace the current `theme:` section (keep `logo`/`favicon` lines for now — Task 3 replaces the logo) and add the new markdown extensions and `extra_css`:

```yaml
theme:
  name: material
  logo: assets/logo.png
  favicon: assets/logo.png
  font:
    text: Inter
    code: JetBrains Mono
  features:
    - navigation.instant
    - navigation.tracking
    - navigation.tabs
    - navigation.sections
    - navigation.top
    - content.code.copy
    - content.action.edit
  palette:
    - scheme: default
      toggle:
        icon: material/weather-night
        name: Switch to dark mode
    - scheme: slate
      toggle:
        icon: material/weather-sunny
        name: Switch to light mode

extra_css:
  - assets/stylesheets/extra.css
```

(The `primary:`/`accent:` keys are removed entirely — indigo dies here; colors now come from `extra.css`.)

Add to `markdown_extensions` (keep all existing entries):

```yaml
  - attr_list
  - md_in_html
  - pymdownx.snippets
```

- [ ] **Step 3: Verify**

Run: `"$SCRATCHPAD/docs-venv/bin/mkdocs" build --strict`
Expected: success, no warnings.

- [ ] **Step 4: Commit**

```bash
git add docs/assets/stylesheets/extra.css mkdocs.yml
git commit -m "docs: brand foundation - jade/charcoal tokens, fonts, tabs nav"
```

---

### Task 3: Theme-aware logo

**Files:**
- Create: `overrides/.icons/notifycat.svg`
- Modify: `mkdocs.yml` (add `custom_dir`, switch `logo` → `icon.logo`)
- Modify: `docs/assets/stylesheets/extra.css` (bell color)

**Interfaces:**
- Produces `overrides/` as `theme.custom_dir` — Task 4's `home.html` lives there.
- The bell circle carries class `nc-bell`, styled from `extra.css`.

- [ ] **Step 1: Create `overrides/.icons/notifycat.svg`**

Cat silhouette in `currentColor` (inherits header ink), bell as a separate class-targeted circle:

```svg
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 22">
  <path d="M2 1 L7 7 Q12 5.5 17 7 L22 1 Q24 8 23 14 Q21.5 21 12 21 Q2.5 21 1 14 Q0 8 2 1 Z" fill="currentColor"/>
  <circle class="nc-bell" cx="16.5" cy="9.5" r="2.2" fill="#0e8a5f"/>
</svg>
```

- [ ] **Step 2: Wire it in `mkdocs.yml`**

```yaml
theme:
  name: material
  custom_dir: overrides
  icon:
    logo: notifycat
  favicon: assets/logo.png
```

(`logo: assets/logo.png` line is removed; favicon stays PNG.)

- [ ] **Step 3: Theme the bell in `extra.css`** (append)

```css
.md-header__button.md-logo .nc-bell { fill: var(--nc-accent); }
```

- [ ] **Step 4: Verify + commit**

Run: `"$SCRATCHPAD/docs-venv/bin/mkdocs" build --strict` → success.

```bash
git add overrides/.icons/notifycat.svg mkdocs.yml docs/assets/stylesheets/extra.css
git commit -m "docs: theme-aware svg logo with jade bell"
```

---

### Task 4: Homepage — hero template, comparison diagram, index.md rewrite

**Files:**
- Create: `overrides/home.html`
- Create: `docs/assets/images/diagrams/event-log-vs-status-board.svg`
- Modify: `docs/index.md`
- Modify: `docs/assets/stylesheets/extra.css` (hero styles, appended)

**Interfaces:**
- Consumes tokens from Task 2, `overrides/` from Task 3.
- `home.html` renders hero + page content; `index.md` selects it via `template: home.html` front matter.
- The SVG uses `var(--nc-*)` and carries class `nc-diagram`; it is included inline via `pymdownx.snippets` so the variables resolve.

- [ ] **Step 1: Create `overrides/home.html`**

```html
{% extends "main.html" %}

{% block tabs %}
{{ super() }}
<section class="nc-hero">
  <div class="nc-hero__inner">
    <p class="nc-eyebrow">One PR &middot; One message</p>
    <h1>One PR.<br><span>One message.</span></h1>
    <p class="nc-hero__sub">Low-noise pull request notifications for Slack. As a PR moves — reviewed, approved, merged — its one message updates in place. No scroll, no spam.</p>
    <p class="nc-hero__cta">
      <a href="compose/" class="md-button md-button--primary">Get started &rarr;</a>
      <a href="features/" class="md-button">See it in Slack</a>
    </p>
  </div>
</section>
{% endblock %}

{% block content %}
{{ page.content }}
{% endblock %}
```

- [ ] **Step 2: Append hero + feature-card styles to `extra.css`**

```css
/* ---------- Homepage hero ---------- */
.nc-hero {
  background-color: var(--nc-bg);
  text-align: center;
  padding: 3.6rem 0.8rem 2.6rem;
}
[data-md-color-scheme="slate"] .nc-hero {
  background-image: radial-gradient(ellipse 70% 90% at 50% 0%, rgba(52, 211, 153, 0.14), transparent 65%);
}
.nc-hero__inner { max-width: 42rem; margin: 0 auto; }
.nc-hero h1 {
  font-family: "Space Grotesk", sans-serif;
  font-size: 2.6rem;
  font-weight: 700;
  line-height: 1.08;
  letter-spacing: -0.03em;
  color: var(--nc-ink);
  margin: 0.6rem 0;
}
.nc-hero h1 span { color: var(--nc-accent); }
.nc-hero__sub {
  font-size: 0.85rem;
  line-height: 1.6;
  color: color-mix(in srgb, var(--nc-ink) 72%, transparent);
  max-width: 30rem;
  margin: 0 auto 1.4rem;
}
.nc-hero__cta .md-button { margin: 0 0.3rem; }
@media screen and (max-width: 44rem) {
  .nc-hero h1 { font-size: 1.9rem; }
  .nc-hero { padding-top: 2.4rem; }
}
```

- [ ] **Step 3: Create `docs/assets/images/diagrams/event-log-vs-status-board.svg`**

Side-by-side comparison, theme-aware, mono labels. Complete file:

```svg
<svg class="nc-diagram" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 760 300" font-family="JetBrains Mono, monospace" font-size="13">
  <style>
    .lbl { fill: var(--nc-ink); }
    .dim { fill: color-mix(in srgb, var(--nc-ink) 55%, transparent); }
    .evt { fill: var(--nc-surface); stroke: var(--nc-border); }
    .msg { fill: color-mix(in srgb, var(--nc-accent) 10%, transparent); stroke: var(--nc-accent); stroke-width: 1.5; }
    .edge { stroke: color-mix(in srgb, var(--nc-ink) 40%, transparent); stroke-width: 1.2; fill: none; }
    .edge-accent { stroke: var(--nc-accent); stroke-width: 1.5; fill: none; }
    .title { font-weight: 700; }
  </style>
  <text x="95" y="26" class="lbl title">Event log</text>
  <text x="95" y="46" class="dim" font-size="11">official GitHub app</text>
  <g>
    <rect class="evt" x="20" y="66"  width="150" height="34" rx="8"/><text class="lbl" x="34" y="88">PR opened</text>
    <rect class="evt" x="20" y="118" width="150" height="34" rx="8"/><text class="lbl" x="34" y="140">Review added</text>
    <rect class="evt" x="20" y="170" width="150" height="34" rx="8"/><text class="lbl" x="34" y="192">Comment added</text>
    <rect class="evt" x="20" y="222" width="150" height="34" rx="8"/><text class="lbl" x="34" y="244">PR merged</text>
  </g>
  <g>
    <path class="edge" d="M170 83 H 230"/><rect class="evt" x="234" y="66" width="120" height="34" rx="8"/><text class="dim" x="248" y="88">message 1</text>
    <path class="edge" d="M170 135 H 230"/><rect class="evt" x="234" y="118" width="120" height="34" rx="8"/><text class="dim" x="248" y="140">message 2</text>
    <path class="edge" d="M170 187 H 230"/><rect class="evt" x="234" y="170" width="120" height="34" rx="8"/><text class="dim" x="248" y="192">message 3</text>
    <path class="edge" d="M170 239 H 230"/><rect class="evt" x="234" y="222" width="120" height="34" rx="8"/><text class="dim" x="248" y="244">message 4</text>
  </g>
  <line x1="395" y1="20" x2="395" y2="280" stroke="var(--nc-border)" stroke-width="1"/>
  <text x="425" y="26" class="lbl title">Status board</text>
  <text x="425" y="46" class="dim" font-size="11">Notifycat</text>
  <g>
    <rect class="evt" x="425" y="66"  width="130" height="34" rx="8"/><text class="lbl" x="437" y="88">opened</text>
    <rect class="evt" x="425" y="118" width="130" height="34" rx="8"/><text class="lbl" x="437" y="140">reviewed</text>
    <rect class="evt" x="425" y="170" width="130" height="34" rx="8"/><text class="lbl" x="437" y="192">commented</text>
    <rect class="evt" x="425" y="222" width="130" height="34" rx="8"/><text class="lbl" x="437" y="244">merged</text>
  </g>
  <path class="edge-accent" d="M555 83  C 600 83,  600 158, 640 158"/>
  <path class="edge-accent" d="M555 135 C 595 135, 600 158, 640 158"/>
  <path class="edge-accent" d="M555 187 C 595 187, 600 160, 640 160"/>
  <path class="edge-accent" d="M555 239 C 600 239, 600 162, 640 162"/>
  <rect class="msg" x="612" y="136" width="128" height="46" rx="10"/>
  <text class="lbl" x="628" y="156">one Slack</text>
  <text class="lbl" x="628" y="172">message</text>
</svg>
```

- [ ] **Step 4: Rewrite `docs/index.md`**

Front matter selects the template and hides chrome; hero content moves to the template; the two stacked Mermaid charts are replaced by the SVG snippet; feature trio becomes grid cards; rest of the current content stays. Complete file:

```markdown
---
template: home.html
hide:
  - navigation
  - toc
---

![A Slack channel where every pull request is one message: the morning digest, a closed and a merged PR struck through, a PR under review with an :eye: marker and Start review button, and a fresh announcement](assets/images/slack_notifications.png)

Your channel becomes a status board, not an event log. Anyone can see where every PR stands at a glance, without scrolling through five notifications to work out whether something still needs eyes.

## Why teams run it

<div class="grid cards" markdown>

- :material-bell-off-outline: **Quiet**

    State changes become message updates and emoji reactions, not new posts. Dependabot bumps collapse to a single compact line. A busy repository produces one Slack line per PR — total.

- :material-eye-check-outline: **Nothing slips through**

    A morning digest resurfaces open PRs that nobody touched yesterday. The "Start review" button shows who is already reviewing. See [What you see in Slack](features.md) for the full tour.

- :material-package-variant-closed: **Easy to own**

    One Go binary, one declarative `config.yaml`, one SQLite file. The whole configuration is validated against Slack and your git host *before boot*. Two secrets, no GitHub App, no OAuth.

</div>

## The problem it solves

The usual way to connect pull requests to Slack is the official GitHub app: `/github subscribe owner/repo` plus `pulls`, `reviews`, and `comments`. It works, but every event becomes another Slack item. The events are all there; the *current state* is nowhere.

Notifycat inverts that. Your git host sends PR webhooks, Notifycat routes each repository to the right channel, and one PR keeps one message. Reviews and comments land on it as reactions; merge strikes it through.

--8<-- "docs/assets/images/diagrams/event-log-vs-status-board.svg"

## When it's not the fit

Notifycat is deliberately narrow. Pick something else if:

- **You want the full event stream in Slack.** Every review and comment as its own post is exactly what the official GitHub app does well.
- **You need GitHub and Bitbucket in one place.** A deployment serves one git host; covering both means two instances, each with its own configuration and database.
- **You need more than pull requests.** Issues, deployments, CI status — out of scope by design.
- **You post to more than one Slack workspace.** One deployment carries one bot token, so it posts to one workspace.

## Git Provider Support

| Feature | GitHub | Bitbucket |
| --- | --- | --- |
| Webhook signature verification (HMAC-SHA256) | Yes | Yes |
| Per-path / monorepo routing | Yes (needs `GITHUB_TOKEN`) | Yes (needs `BITBUCKET_TOKEN`) |
| Stuck-PR digest | Yes | Yes |
| Reactions & review flow | Yes | Yes |
| Token auth | Fine-grained PAT (Bearer) | Access token (Bearer) or scoped Atlassian API token (Basic) |

## Where next

<div class="grid cards" markdown>

- **[What you see in Slack](features.md)** — the message lifecycle, reactions, digest, and Start review button
- **[Install with Docker Compose](compose.md)** — running in ~10 minutes
- **[Configuration basics](configure.md)** — the whole model in two minutes
- **[Route repositories to channels](routing.md)** — mappings, mentions, reactions
- **[Troubleshooting](troubleshooting.md)** — fix a delivery that didn't reach Slack
- **[config.yaml reference](configuration.md)** — every key

</div>
```

Note: the H1 is gone from the markdown (the hero owns it). If `build --strict` complains about a missing H1, the page `title: Overview` comes from `nav`, which is sufficient.

- [ ] **Step 5: Verify + commit**

Run: `"$SCRATCHPAD/docs-venv/bin/mkdocs" build --strict` → success. Also `grep -c "nc-diagram" site/index.html` → at least 1 (SVG really inlined).

```bash
git add overrides/home.html docs/index.md docs/assets/images/diagrams/event-log-vs-status-board.svg docs/assets/stylesheets/extra.css
git commit -m "docs: landing page hero and status-board comparison diagram"
```

---

### Task 5: Where-next grid cards on configure.md

**Files:**
- Modify: `docs/configure.md` (lines around 52 — the `| You want to… | Go to |` table)

- [ ] **Step 1: Replace the link table with grid cards**

Read the current table rows in `docs/configure.md` and convert each `| goal | [Page](page.md) |` row into a card item, preserving link targets and goal text exactly:

```markdown
<div class="grid cards" markdown>

- **[<page title>](<page>.md)** — <goal text from the table row>

</div>
```

(One `-` item per former table row; do not invent new copy.)

- [ ] **Step 2: Verify + commit**

Run: `"$SCRATCHPAD/docs-venv/bin/mkdocs" build --strict` → success.

```bash
git add docs/configure.md
git commit -m "docs: grid cards for configure where-next links"
```

---

### Task 6: Bespoke SVG diagrams — request flow & digest timeline

**Files:**
- Create: `docs/assets/images/diagrams/request-flow.svg`
- Create: `docs/assets/images/diagrams/digest-timeline.svg`
- Modify: `docs/routing.md` (include request-flow near the top overview)
- Modify: `docs/digest.md` (include timeline after the intro)

**Interfaces:** consumes `--nc-*` tokens and class `nc-diagram` (unframed media) from Task 2.

- [ ] **Step 1: Create `docs/assets/images/diagrams/request-flow.svg`**

```svg
<svg class="nc-diagram" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 760 150" font-family="JetBrains Mono, monospace" font-size="13">
  <style>
    .lbl { fill: var(--nc-ink); }
    .dim { fill: color-mix(in srgb, var(--nc-ink) 55%, transparent); }
    .evt { fill: var(--nc-surface); stroke: var(--nc-border); }
    .msg { fill: color-mix(in srgb, var(--nc-accent) 10%, transparent); stroke: var(--nc-accent); stroke-width: 1.5; }
    .edge { stroke: color-mix(in srgb, var(--nc-ink) 40%, transparent); stroke-width: 1.2; fill: none; marker-end: url(#nc-arrow); }
  </style>
  <defs>
    <marker id="nc-arrow" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto">
      <path d="M0 0 L8 4 L0 8 Z" fill="color-mix(in srgb, var(--nc-ink) 40%, transparent)"/>
    </marker>
  </defs>
  <rect class="evt" x="14"  y="50" width="128" height="40" rx="9"/><text class="lbl" x="28" y="70">webhook</text><text class="dim" x="28" y="84" font-size="10">from git host</text>
  <path class="edge" d="M142 70 H 176"/>
  <rect class="evt" x="180" y="50" width="150" height="40" rx="9"/><text class="lbl" x="194" y="70">verify HMAC</text><text class="dim" x="194" y="84" font-size="10">signature check</text>
  <path class="edge" d="M330 70 H 364"/>
  <rect class="evt" x="368" y="50" width="128" height="40" rx="9"/><text class="lbl" x="382" y="70">route</text><text class="dim" x="382" y="84" font-size="10">repo &#8594; channel</text>
  <path class="edge" d="M496 70 H 530"/>
  <g transform="translate(506, 20)"><path d="M2 1 L5.5 5 Q8.4 4.1 11.3 5 L14.8 1 Q16.2 5.4 15.5 9.4 Q14.5 14.2 8.4 14.2 Q2.3 14.2 1.2 9.4 Q0.5 5.4 2 1 Z" fill="color-mix(in srgb, var(--nc-ink) 35%, transparent)" transform="scale(1.1)"/><circle cx="12.5" cy="7" r="1.5" fill="var(--nc-accent)"/></g>
  <rect class="msg" x="534" y="50" width="196" height="40" rx="9"/><text class="lbl" x="548" y="70">one Slack message</text><text class="dim" x="548" y="84" font-size="10">posted or updated in place</text>
</svg>
```

- [ ] **Step 2: Create `docs/assets/images/diagrams/digest-timeline.svg`**

```svg
<svg class="nc-diagram" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 760 170" font-family="JetBrains Mono, monospace" font-size="13">
  <style>
    .lbl { fill: var(--nc-ink); }
    .dim { fill: color-mix(in srgb, var(--nc-ink) 55%, transparent); }
    .evt { fill: var(--nc-surface); stroke: var(--nc-border); }
    .msg { fill: color-mix(in srgb, var(--nc-accent) 10%, transparent); stroke: var(--nc-accent); stroke-width: 1.5; }
    .axis { stroke: color-mix(in srgb, var(--nc-ink) 35%, transparent); stroke-width: 1.2; }
  </style>
  <line class="axis" x1="20" y1="120" x2="740" y2="120"/>
  <text class="dim" x="30"  y="140" font-size="11">yesterday</text>
  <text class="dim" x="330" y="140" font-size="11">overnight</text>
  <text class="dim" x="600" y="140" font-size="11">09:00 digest</text>
  <rect class="evt" x="30"  y="60" width="118" height="34" rx="8"/><text class="lbl" x="42"  y="82">#412 open</text>
  <rect class="evt" x="160" y="60" width="118" height="34" rx="8"/><text class="lbl" x="172" y="82">#415 open</text>
  <text class="dim" x="330" y="82" font-size="20">&#128564;</text>
  <text class="dim" x="368" y="82" font-size="11">no reviews, no comments</text>
  <rect class="msg" x="580" y="46" width="160" height="62" rx="10"/>
  <text class="lbl" x="594" y="68">morning digest</text>
  <text class="dim" x="594" y="86" font-size="11">#412, #415 still</text>
  <text class="dim" x="594" y="99" font-size="11">waiting for eyes</text>
</svg>
```

- [ ] **Step 3: Include the diagrams in the pages**

In `docs/routing.md`, after the first intro paragraph (before the first `##` heading), insert:

```markdown
--8<-- "docs/assets/images/diagrams/request-flow.svg"
```

In `docs/digest.md`, after the first intro paragraph, insert:

```markdown
--8<-- "docs/assets/images/diagrams/digest-timeline.svg"
```

Read each page first; place the include where it illustrates the adjacent prose, not mid-section.

- [ ] **Step 4: Verify + commit**

Run: `"$SCRATCHPAD/docs-venv/bin/mkdocs" build --strict` → success. `grep -c "nc-diagram" site/routing/index.html site/digest/index.html` → 1 each.

```bash
git add docs/assets/images/diagrams/request-flow.svg docs/assets/images/diagrams/digest-timeline.svg docs/routing.md docs/digest.md
git commit -m "docs: theme-aware request-flow and digest-timeline diagrams"
```

---

### Task 7: Social cards

**Files:**
- Modify: `docs/requirements.txt`
- Modify: `mkdocs.yml` (plugins)
- Modify: `.github/workflows/docs.yml` (imaging system libs)

- [ ] **Step 1: `docs/requirements.txt`**

```
mkdocs-material[imaging]==9.5.39
```

- [ ] **Step 2: `mkdocs.yml` plugins**

```yaml
plugins:
  - search
  - social:
      enabled: !ENV [DOCS_SOCIAL, true]
      cards_layout_options:
        background_color: "#111413"
        color: "#edf1ef"
        font_family: Space Grotesk
```

(`DOCS_SOCIAL=false` lets local builds skip card generation when cairo isn't installed.)

- [ ] **Step 3: Workflow system deps**

In `.github/workflows/docs.yml`, before the "Install MkDocs Material" step:

```yaml
      - name: Install imaging system libraries
        run: sudo apt-get update && sudo apt-get install -y libcairo2 libfreetype6 libffi-dev libjpeg-dev libpng-dev zlib1g-dev
```

- [ ] **Step 4: Verify locally**

Try: `"$SCRATCHPAD/docs-venv/bin/pip" install -q "mkdocs-material[imaging]==9.5.39"` then `"$SCRATCHPAD/docs-venv/bin/mkdocs" build --strict`.
If cairo is missing locally (import error mentioning `cairo`), run `brew list cairo || brew install cairo` — and if still failing, verify with `DOCS_SOCIAL=false "$SCRATCHPAD/docs-venv/bin/mkdocs" build --strict` and note that card generation is CI-verified.
When it does build, check `site/assets/images/social/index.png` exists.

- [ ] **Step 5: Commit**

```bash
git add docs/requirements.txt mkdocs.yml .github/workflows/docs.yml
git commit -m "docs: branded social cards"
```

---

### Task 8: Visual verification in both themes

**Files:** none new (fixes land in files from earlier tasks).

- [ ] **Step 1: Serve the site**

Run: `"$SCRATCHPAD/docs-venv/bin/mkdocs" serve -a 127.0.0.1:8801` in the background.

- [ ] **Step 2: Playwright screenshots** (use the webapp-testing skill / a Python Playwright script)

Capture, at 1440×900 and 390×844 (mobile), for **both** `data-md-color-scheme` values (toggle by clicking the palette button or setting `localStorage` key `/.__palette`):

1. `http://127.0.0.1:8801/` (homepage — hero, cards, comparison diagram, framed screenshot)
2. `http://127.0.0.1:8801/routing/` (guide page — tabs, headings, request-flow diagram, jade links)
3. `http://127.0.0.1:8801/configuration/` (reference page — code blocks, admonitions, tables)

- [ ] **Step 3: Review the screenshots yourself against the checklist**

- No indigo anywhere.
- Hero legible both themes; jade glow only on black theme.
- Screenshot framing: hairline on white, border+shadow on black.
- Diagrams flip with the theme (inline SVG vars resolving); no frame around them.
- Tabs row shows all six sections; active tab jade.
- Headings render in Space Grotesk (compare against body Inter).
- Mobile hero doesn't overflow.

Fix anything off in `extra.css`/templates, re-screenshot, then:

- [ ] **Step 4: Final strict build + commit fixes**

```bash
"$SCRATCHPAD/docs-venv/bin/mkdocs" build --strict
git add -A && git commit -m "docs: visual polish from both-theme verification"
```

(Skip the commit if no fixes were needed.)

---

### Task 9: Push branch

- [ ] **Step 1: Push**

```bash
git push -u origin docs/visual-redesign
```

- [ ] **Step 2: Report** — summarize what shipped, screenshots for the user to review when back, and note that opening the PR is left to them (or on request).
