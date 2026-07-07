# Notifycat docs visual redesign — design spec

**Date:** 2026-07-07
**Status:** Approved (all four sections approved interactively; user waived final spec review)
**Scope:** Docs-only. No Go code, no runtime behavior. Lands on branch `docs/visual-redesign` off `main` (after the user-centric restructure, PR #168).

## Goal

Replace the stock indigo mkdocs-material look with a distinctive Notifycat brand: "bold product" visual direction, charcoal/black-and-white base, jade green accent, custom landing page, branded diagrams, and consistent treatment for white-background screenshots and videos.

Decisions were made against rendered mockups (visual companion session under `.superpowers/brainstorm/`), not descriptions. Direction chosen: **Bold product** (dark hero, heavy geometric type, rounded cards). Rejected: editorial-serif and quiet-mono directions; amber accent; neon/acid greens (fail on white backgrounds).

## Constraints

- **No indigo anywhere** (explicit user veto — palette, diagrams, social cards).
- **Two themes, designed equally:** white theme and black theme, user-toggleable.
- **Screenshots and videos have white backgrounds** — the accent must hold contrast on white, and media must sit naturally in both themes.
- Product story is "low-noise" — jade appears only where it carries meaning; everything else stays black/white/gray.

## Section 1: Brand system

### Color tokens

| Token | White theme | Black theme |
| --- | --- | --- |
| Background | `#ffffff` | `#111413` |
| Surface (cards, code blocks) | `#f5f6f5` | `#1a1e1c` |
| Ink (text) | `#161817` | `#edf1ef` |
| Accent (jade) | `#0e8a5f` | `#34d399` |
| Accent emphasis (pressed/strong) | `#0b6b4a` | `#6ee7b7` |

Jade is a paired token: deep ink-green on white (AA contrast for links/buttons), brighter sibling on black. The black-theme hero gets a subtle jade radial glow; the white theme stays clean.

### Typography

- **Space Grotesk** (500/700/800) — headings and hero. Loaded via CSS `@import`/`<link>` since Material's `theme.font` only takes two families.
- **Inter** — body and UI, via `theme.font.text`.
- **JetBrains Mono** — code blocks and labels, via `theme.font.code`.

### Logo

Rebuilt as a theme-aware SVG: ink-colored cat silhouette (flips with theme), bell recolored jade. Existing `logo.png` stays as favicon.

### Accent discipline

Jade appears only on: links, primary CTA, highlighted hero word, admonition edges, diagram state-nodes, the bell. Nothing else.

## Section 2: Homepage + page chrome

### Homepage (`overrides/home.html`, Material template override)

- Hero: "One PR. One message." in Space Grotesk 800, jade highlight on "One message.", subline from current intro. CTAs: **Get started →** (jade, → Docker Compose install) and **See it in Slack** (outline, → features page).
- Proof strip: real `slack_notifications.png` directly under the hero in the framed media treatment.
- Three feature cards: Quiet / Nothing slips through / Easy to own.
- "The problem it solves" with the redrawn before/after diagram (Section 3).
- Remaining current `index.md` content (git-provider table, "when it's not the fit", where-next) moves down-page as styled sections. Nothing deleted.

### Chrome on all pages

- `navigation.tabs`: What is Notifycat · Install · Use · Troubleshoot · Reference · Upgrading.
- New SVG logo in header; light/dark toggle stays.
- Doc pages keep standard sidebar/TOC layout, inherit all tokens (Space Grotesk headings, jade links, restyled admonitions, JetBrains Mono code, framed media).
- "Where next"-style link tables become Material grid cards.

## Section 3: Diagrams & media

### Mermaid restyling

Custom theme-aware Mermaid init via `extra_javascript`. Visual language: event nodes solid ink fill; state/outcome nodes jade outline on jade-tinted surface; edges gray with jade reserved for "update" paths; labels JetBrains Mono. No diagram content changes.

### Bespoke SVG diagrams (theme-aware via CSS variables)

1. **Homepage hero diagram** — "event log vs. status board" comparison, one polished side-by-side graphic replacing the two stacked Mermaid charts.
2. **Request-flow diagram** (routing/configure pages) — webhook → signature check → routing → one Slack message, cat as subtle midpoint marker.
3. **Digest timeline** (digest page) — PRs going stale overnight, morning digest resurfacing them.

### Screenshot & video treatment

Global CSS on content media: rounded corners; hairline border on white theme; border + soft shadow on black theme. No per-image markup.

### Social cards

`social` plugin with custom layout: charcoal background, cat logo, jade accent, Space Grotesk title. Adds imaging deps (`pillow`, `cairosvg`) to `docs/requirements.txt` and the docs CI job.

## Section 4: Implementation & verification

### Files

- `overrides/home.html` (repo root, next to `docs/`) — landing template; `mkdocs.yml` gains `theme.custom_dir: overrides`. The template must live outside `docs/` so mkdocs doesn't publish it as a raw page.
- `docs/assets/stylesheets/extra.css` — tokens as CSS custom properties for both schemes + all component styling.
- `docs/assets/javascripts/mermaid-config.js` — theme-aware Mermaid init.
- `docs/assets/logo.svg` + `docs/assets/images/diagrams/*.svg`.
- `mkdocs.yml` — drop indigo for custom palette on `default`/`slate` schemes, add `navigation.tabs`, fonts, `social` plugin, `extra_css`/`extra_javascript`, `custom_dir`.
- `docs/requirements.txt` + docs deploy workflow — imaging deps.

### Verification

1. `mkdocs build --strict` passes.
2. `mkdocs serve` + Playwright screenshots of homepage, one guide page, one reference page — both themes.
3. Manual: theme toggle round-trip, mobile-width hero, Mermaid legibility in both themes, social card render.
