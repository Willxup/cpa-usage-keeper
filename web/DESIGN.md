---
version: alpha
name: CPA Usage Keeper Design System
description: A warm, data-dense operations dashboard system for CPA usage analytics, request health, pricing, and credential monitoring.

colors:
  primary: "#8B8680"
  primary-light: "#8B8680"
  primary-dark: "#8B8680"
  primary-hover: "#7F7A74"
  primary-hover-light: "#7F7A74"
  primary-hover-dark: "#9A948E"
  primary-active: "#726D67"
  primary-active-light: "#726D67"
  primary-active-dark: "#A6A099"
  primary-foreground: "#FFFFFF"
  primary-foreground-dark: "#151412"
  background: "#FAF9F5"
  background-light: "#FAF9F5"
  surface: "#F0EEE8"
  surface-light: "#F0EEE8"
  surface-muted: "#E9E6DF"
  surface-muted-light: "#E9E6DF"
  surface-hover-light: "#E9E6DF"
  surface-raised: "#FFFDF9"
  surface-raised-light: "#FFFDF9"
  background-dark: "#151412"
  surface-dark: "#1D1B18"
  surface-muted-dark: "#262320"
  surface-hover-dark: "#2E2A26"
  surface-raised-dark: "#2A2723"
  floating-surface-light: "#FFFDF9"
  floating-border-light: "#D8D3CA"
  floating-surface-dark: "#2A2723"
  floating-border-dark: "#4A443D"
  text-primary: "#2D2A26"
  text-primary-light: "#2D2A26"
  text-secondary: "#6D6760"
  text-secondary-light: "#6D6760"
  text-muted: "#A29C95"
  text-muted-light: "#A29C95"
  text-primary-dark: "#F6F4F1"
  text-secondary-dark: "#C9C3BB"
  text-muted-dark: "#9C958D"
  border: "#E3E1DB"
  border-light: "#E3E1DB"
  border-strong: "#D5D2CB"
  border-strong-light: "#D5D2CB"
  border-hover-light: "#CECAC4"
  border-dark: "#3A3530"
  border-strong-dark: "#4A453F"
  border-hover-dark: "#5A544D"
  focus-ring: "#8B8680"
  focus-ring-light: "#8B8680"
  focus-ring-dark: "#A6A099"
  success: "#10B981"
  attention: "#E0AA14"
  error: "#C65746"
  error-strong: "#8A3A30"
  warning-surface-light: "rgba(198, 87, 70, 0.12)"
  warning-border-light: "rgba(198, 87, 70, 0.35)"
  warning-text-light: "#C65746"
  warning-surface-dark: "rgba(198, 87, 70, 0.22)"
  warning-border-dark: "rgba(198, 87, 70, 0.45)"
  warning-text-dark: "#F1B0A6"
  success-badge-bg-light: "#D1FAE5"
  success-badge-text-light: "#065F46"
  success-badge-border-light: "#6EE7B7"
  success-badge-bg-dark: "rgba(6, 78, 59, 0.30)"
  success-badge-text-dark: "#6EE7B7"
  success-badge-border-dark: "#059669"
  failure-badge-bg-light: "rgba(198, 87, 70, 0.14)"
  failure-badge-text-light: "#8A3A30"
  failure-badge-border-light: "rgba(198, 87, 70, 0.35)"
  failure-badge-bg-dark: "rgba(198, 87, 70, 0.24)"
  failure-badge-text-dark: "#F1B0A6"
  failure-badge-border-dark: "rgba(198, 87, 70, 0.50)"
  count-badge-bg-light: "rgba(139, 134, 128, 0.18)"
  count-badge-bg-dark: "rgba(139, 134, 128, 0.28)"
  count-badge-text-light: "#726D67"
  count-badge-text-dark: "#A6A099"
  chart-blue: "#3B82F6"
  chart-violet: "#8B5CF6"
  chart-green: "#22C55E"
  chart-orange: "#F97316"
  chart-teal: "#14B8A6"

typography:
  display-xl:
    fontFamily: "SF Pro Text, Segoe UI, sans-serif"
    fontSize: 64px
    fontWeight: 800
    lineHeight: 0.92
    letterSpacing: -0.055em
  heading-xl:
    fontFamily: "SF Pro Text, Segoe UI, sans-serif"
    fontSize: 32px
    fontWeight: 800
    lineHeight: 1.1
    letterSpacing: -0.03em
  heading-lg:
    fontFamily: "SF Pro Text, Segoe UI, sans-serif"
    fontSize: 24px
    fontWeight: 700
    lineHeight: 1.1
    letterSpacing: -0.03em
  heading-md:
    fontFamily: "SF Pro Text, Segoe UI, sans-serif"
    fontSize: 18px
    fontWeight: 700
    lineHeight: 1.25
  body-md:
    fontFamily: "SF Pro Text, Segoe UI, sans-serif"
    fontSize: 15px
    fontWeight: 400
    lineHeight: 1.7
  body-sm:
    fontFamily: "SF Pro Text, Segoe UI, sans-serif"
    fontSize: 13px
    fontWeight: 400
    lineHeight: 1.6
  label-md:
    fontFamily: "SF Pro Text, Segoe UI, sans-serif"
    fontSize: 13px
    fontWeight: 700
    lineHeight: 1.2
    letterSpacing: 0.02em
  label-sm:
    fontFamily: "SF Pro Text, Segoe UI, sans-serif"
    fontSize: 12px
    fontWeight: 700
    lineHeight: 1.2
    letterSpacing: 0.02em
  metric-lg:
    fontFamily: "SF Pro Text, Segoe UI, sans-serif"
    fontSize: 36px
    fontWeight: 700
    lineHeight: 1.1
    letterSpacing: -0.03em
  mono-sm:
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace"
    fontSize: 12px
    fontWeight: 600
    lineHeight: 1.4

spacing:
  xs: 4px
  sm: 8px
  md: 16px
  lg: 24px
  xl: 32px
  2xl: 48px

rounded:
  sm: 4px
  md: 8px
  lg: 12px
  xl: 24px
  full: 9999px

components:
  button-primary:
    backgroundColor: "{colors.primary-active-light}"
    textColor: "{colors.primary-foreground}"
    rounded: "{rounded.md}"
    padding: 12px
    typography: "{typography.label-md}"
  button-primary-hover:
    backgroundColor: "{colors.primary-hover-light}"
  button-primary-light:
    backgroundColor: "{colors.primary-active-light}"
    textColor: "{colors.primary-foreground}"
    rounded: "{rounded.md}"
    padding: 12px
    typography: "{typography.label-md}"
  button-primary-light-hover:
    backgroundColor: "{colors.primary-hover-light}"
  button-primary-dark:
    backgroundColor: "{colors.primary-active-dark}"
    textColor: "{colors.primary-foreground-dark}"
    rounded: "{rounded.md}"
    padding: 12px
    typography: "{typography.label-md}"
  button-primary-dark-hover:
    backgroundColor: "{colors.primary-dark}"
  button-secondary:
    backgroundColor: "{colors.surface-muted-light}"
    textColor: "{colors.text-primary-light}"
    rounded: "{rounded.md}"
    padding: 12px
    typography: "{typography.label-md}"
  button-secondary-light:
    backgroundColor: "{colors.surface-muted-light}"
    textColor: "{colors.text-primary-light}"
    rounded: "{rounded.md}"
    padding: 12px
    typography: "{typography.label-md}"
  button-secondary-light-hover:
    backgroundColor: "{colors.surface-hover-light}"
  button-secondary-dark:
    backgroundColor: "{colors.surface-muted-dark}"
    textColor: "{colors.text-primary-dark}"
    rounded: "{rounded.md}"
    padding: 12px
    typography: "{typography.label-md}"
  button-secondary-dark-hover:
    backgroundColor: "{colors.surface-hover-dark}"
  button-danger:
    backgroundColor: "{colors.error-strong}"
    textColor: "{colors.primary-foreground}"
    rounded: "{rounded.md}"
    padding: 12px
    typography: "{typography.label-md}"
  button-danger-light:
    backgroundColor: "{colors.error-strong}"
    textColor: "{colors.primary-foreground}"
    rounded: "{rounded.md}"
    padding: 12px
    typography: "{typography.label-md}"
  button-danger-light-hover:
    backgroundColor: "{colors.error}"
  button-danger-dark:
    backgroundColor: "{colors.error-strong}"
    textColor: "{colors.primary-foreground}"
    rounded: "{rounded.md}"
    padding: 12px
    typography: "{typography.label-md}"
  button-danger-dark-hover:
    backgroundColor: "{colors.error}"
  pill-shell:
    backgroundColor: "{colors.surface-muted-light}"
    rounded: "{rounded.full}"
    padding: 4px
  pill-shell-light:
    backgroundColor: "{colors.surface-muted-light}"
    rounded: "{rounded.full}"
    padding: 4px
  pill-shell-dark:
    backgroundColor: "{colors.surface-muted-dark}"
    rounded: "{rounded.full}"
    padding: 4px
  pill-active:
    backgroundColor: "{colors.surface-raised-light}"
    textColor: "{colors.text-primary-light}"
    rounded: "{rounded.full}"
    padding: 8px
    typography: "{typography.label-sm}"
  pill-active-light:
    backgroundColor: "{colors.surface-raised-light}"
    textColor: "{colors.text-primary-light}"
    rounded: "{rounded.full}"
    padding: 8px
    typography: "{typography.label-sm}"
  pill-active-dark:
    backgroundColor: "{colors.surface-dark}"
    textColor: "{colors.text-primary-dark}"
    rounded: "{rounded.full}"
    padding: 8px
    typography: "{typography.label-sm}"
  card:
    backgroundColor: "{colors.surface-light}"
    textColor: "{colors.text-primary-light}"
    rounded: "{rounded.lg}"
    padding: 24px
  card-light:
    backgroundColor: "{colors.surface-light}"
    textColor: "{colors.text-primary-light}"
    rounded: "{rounded.lg}"
    padding: 24px
  card-dark:
    backgroundColor: "{colors.surface-dark}"
    textColor: "{colors.text-primary-dark}"
    rounded: "{rounded.lg}"
    padding: 24px
  card-floating:
    backgroundColor: "{colors.floating-surface-light}"
    textColor: "{colors.text-primary-light}"
    rounded: "{rounded.xl}"
    padding: 20px
  card-floating-light:
    backgroundColor: "{colors.floating-surface-light}"
    textColor: "{colors.text-primary-light}"
    rounded: "{rounded.xl}"
    padding: 20px
  card-floating-dark:
    backgroundColor: "{colors.floating-surface-dark}"
    textColor: "{colors.text-primary-dark}"
    rounded: "{rounded.xl}"
    padding: 20px
  input:
    backgroundColor: "{colors.background-light}"
    textColor: "{colors.text-primary-light}"
    rounded: "{rounded.md}"
    padding: 12px
    typography: "{typography.body-sm}"
  input-light:
    backgroundColor: "{colors.background-light}"
    textColor: "{colors.text-primary-light}"
    rounded: "{rounded.md}"
    padding: 12px
    typography: "{typography.body-sm}"
  input-dark:
    backgroundColor: "{colors.surface-dark}"
    textColor: "{colors.text-primary-dark}"
    rounded: "{rounded.md}"
    padding: 12px
    typography: "{typography.body-sm}"
  select-trigger-light:
    backgroundColor: "{colors.surface-light}"
    textColor: "{colors.text-primary-light}"
    rounded: "{rounded.md}"
    padding: 12px
    typography: "{typography.body-sm}"
  select-trigger-dark:
    backgroundColor: "{colors.surface-dark}"
    textColor: "{colors.text-primary-dark}"
    rounded: "{rounded.md}"
    padding: 12px
    typography: "{typography.body-sm}"
  status-badge-success-light:
    backgroundColor: "{colors.success-badge-bg-light}"
    textColor: "{colors.success-badge-text-light}"
    rounded: "{rounded.full}"
    padding: 8px
    typography: "{typography.label-sm}"
  status-badge-success-dark:
    backgroundColor: "{colors.success-badge-bg-dark}"
    textColor: "{colors.success-badge-text-dark}"
    rounded: "{rounded.full}"
    padding: 8px
    typography: "{typography.label-sm}"
  status-badge-failure-light:
    backgroundColor: "{colors.failure-badge-bg-light}"
    textColor: "{colors.failure-badge-text-light}"
    rounded: "{rounded.full}"
    padding: 8px
    typography: "{typography.label-sm}"
  status-badge-failure-dark:
    backgroundColor: "{colors.failure-badge-bg-dark}"
    textColor: "{colors.failure-badge-text-dark}"
    rounded: "{rounded.full}"
    padding: 8px
    typography: "{typography.label-sm}"
---

# CPA Usage Keeper Design System

## Overview

CPA Usage Keeper is an operational analytics console, not a marketing site and not a generic neon AI dashboard. The interface should feel calm, deliberate, trustworthy, and data-dense, with enough warmth to reduce operator fatigue during long monitoring sessions.

The default visual personality is warm paper and muted stone: soft beige surfaces, low-saturation taupe actions, compact typography, and restrained depth. Use this to make heavy telemetry views feel composed rather than sterile. The login page may feel slightly more atmospheric, but the authenticated console should stay disciplined and legible.

This product has three closely related page families:

- an auth entry surface with a large editorial headline and a constrained access card;
- an administrator console with tabs, filters, charts, tables, health views, and settings;
- a read-only API Key overview that mirrors the admin shell but removes management-heavy affordances.

Avoid drifting into bright SaaS blue branding, glossy enterprise chrome, or decorative AI gradients. The product should read as an operations workbench with polished restraint.

## Colors

The palette is built around neutral surfaces first and brand/action color second.

- `background`, `surface`, `surface-muted`, and `surface-raised` define the warm light theme. Most of the interface chrome should live here.
- `primary`, `primary-hover`, and `primary-active` are for confirmed actions, active pills, active tabs, and compact emphasis. They are not a general decoration color.
- `success` is for healthy or completed states.
- `attention` is for medium quota bands or neutral caution, not for destructive errors.
- `error` is for destructive actions, validation failures, unhealthy status, and failed request signaling.
- `error-strong` is the darker destructive fill for solid buttons that need white text to remain comfortably legible.

The product already supports a dark theme. Use `background-dark`, `surface-dark`, `surface-muted-dark`, `surface-hover-dark`, `surface-raised-dark`, `text-primary-dark`, `text-secondary-dark`, `text-muted-dark`, `border-dark`, `border-strong-dark`, and `border-hover-dark` when authoring dark-specific surfaces. The white theme is a flatter variant of the light theme: it keeps the same taupe action color and typography but removes the paper tint and uses cleaner white surfaces.

Unsuffixed color tokens remain the canonical warm-light defaults for backward compatibility and quick drafting. For any new reusable theme-aware primitive, prefer the explicit `*-light` and `*-dark` token pairs so the mapping is machine-readable rather than implied by prose.

Theme-mode rules:

- Light mode should read as warm paper and soft stone, using tinted neutrals and restrained chrome.
- Dark mode should preserve the same taupe action identity, but deepen the surface stack and increase text contrast rather than simply inverting the light palette.
- White mode is an implementation variant of light mode, not a third visual family. Reuse light-mode component rules unless a flatter border or background treatment is explicitly needed.

Chart and telemetry accents should come from the restrained multi-accent set rather than the taupe primary:

- `chart-blue` for request and traffic-oriented series;
- `chart-violet` for token-heavy or composition-oriented series;
- `chart-green` for healthy throughput or positive trend lines;
- `chart-orange` for cost, latency pressure, or hot-path throughput;
- `chart-teal` for cache, efficiency, or support metrics.

Do not flood cards, tables, or generic chart fills with `primary`. In this system, taupe means action or selection, not raw data encoding.

## Typography

Use `SF Pro Text, Segoe UI, sans-serif` for all interface typography. The tone should feel native, compact, and operational rather than branded or expressive.

- `display-xl` is reserved for the login hero and other rare top-level statements.
- `heading-xl` and `heading-lg` are for major section titles, card headers, and feature blocks.
- `heading-md` is for compact section labels, top-bar badges, and high-signal subheaders.
- `body-md` and `body-sm` support explanatory copy, helper text, and dense metadata.
- `label-md` and `label-sm` drive pills, tabs, compact controls, badges, and toolbar actions.
- `metric-lg` is for prominent KPIs or single-number emphasis inside stat cards.
- `mono-sm` is for API keys, model names, quota values, timestamps, identifiers, and other technical strings that benefit from stable alignment.

Keep prose short. This is a telemetry-heavy product, so hierarchy should come from weight, spacing, and contrast before it comes from oversized type. Do not use large display sizing inside dense data cards.

## Layout

Use an 8px spacing rhythm. Most control internals should stay in the `xs` to `md` range, cards in the `lg` range, and page shells or major separations in the `xl` to `2xl` range.

Responsive behavior is structural, not ornamental:

- Mobile: single-column stacking, reduced blur, and preserved content hierarchy. Do not hide critical telemetry just to avoid horizontal scrolling.
- Tablet: allow two-column groupings where the comparison benefit is real.
- Desktop: use wide but bounded workbench layouts. The authenticated pages should feel like a centered dashboard shell rather than a full-bleed canvas.

Preserve the current product patterns:

- auth surfaces use a split narrative-plus-form layout on desktop and stack on mobile;
- authenticated shells use a sticky top control bar with grouped pills on the right;
- dense tab rows and data regions may scroll horizontally when collapsing them would destroy meaning;
- filters should stay adjacent to the charts, tables, or sections they affect;
- read-only API Key views should keep the same shell grammar as admin pages so the product still feels unified.

All control clusters should tolerate short multilingual labels such as `EN`, `中`, and `繁` without breaking alignment.

## Elevation & Depth

Prefer borders, tonal contrast, and a small amount of shadow over aggressive depth.

Standard cards use quiet borders and light shadow. Floating surfaces such as the sticky top bar, auth card, popovers, dialogs, and tooltip-like panels may use `surface-raised`, stronger shadow, and glass blur. Keep these effects subtle and localized.

Blurred glass is allowed only where the current product already uses it:

- sticky top bars;
- login and floating access cards;
- transient overlays or floating support surfaces.

When blur would reduce clarity, turn it off. Reduced-motion and reduced-transparency preferences should fall back to solid surfaces and clean borders. On smaller screens, prefer stable readability over atmosphere.

Avoid heavy glows, deep drop shadows, glossy gradients across whole surfaces, or fake 3D layering. The background may carry faint radial or grid texture, but the content areas should stay disciplined.

## Shapes

This system uses two main shape families:

- `rounded.md` and `rounded.lg` for core controls, inputs, buttons, tables, and content cards;
- `rounded.full` for pill shells, active chips, compact toggles, identity badges, and toolbar actions.

Use `rounded.xl` for hero or floating shells that intentionally feel softer and more premium than the standard data cards. This should be the exception family for auth panels, sticky frosted bars, and elevated overlays, not the default radius for every component.

Keep corners soft and consistent. Do not mix sharp, material-like, and bubble-like radii on the same page.

## Components

### Buttons

Standalone buttons follow the core button family: 8px radius, compact padding, visible focus, and stable width during loading. Use one primary button per decision area.

- Primary buttons use taupe and are reserved for the most important committed action in a section.
- Secondary buttons use muted surfaces and borders for safe alternatives.
- Destructive buttons use `error` and explicit language.
- Ghost buttons are acceptable for low-emphasis utilities, but they still need clear hover and focus feedback.

Do not turn every toolbar utility into a primary button. In this product, excessive primaries make the console noisy.

For theme-aware shared controls, use the explicit paired tokens:

- `button-primary-light` / `button-primary-dark`
- `button-secondary-light` / `button-secondary-dark`
- `button-danger-light` / `button-danger-dark`

The dark variants may invert text treatment relative to light mode when needed to preserve contrast. Accessibility wins over strict color-role symmetry.

### Pills, Tabs, and Switchers

Pill grammar is a signature of this product. Theme toggles, language controls, tab bars, refresh controls, logout switches, and many compact filters share the same pattern:

- an outer `pill-shell` with border and subtle inset depth;
- an inner active pill that lifts slightly and becomes more opaque;
- compact label sizing and strong selection contrast.

Keep active state obvious without becoming saturated. The active pill should feel raised and selected, not brightly branded.

Use `pill-shell-light` / `pill-shell-dark` and `pill-active-light` / `pill-active-dark` for reusable segmented controls. Do not leave a shared switcher on unsuffixed defaults once it is intended to work across both themes.

### Cards and Floating Surfaces

Use standard cards for stats, charts, settings blocks, credential sections, and request-detail containers. Cards should group related content, not create decoration for its own sake.

Floating or hero surfaces may use `card-floating` semantics: larger radius, cleaner raised surface, and stronger but still soft shadow. This is appropriate for login, sticky control bars, and modal-like support surfaces.

Avoid deep nesting of cards. When hierarchy is needed inside a card, prefer internal dividers, spacing, and compact sub-panels before creating another full card border.

Use `card-light` / `card-dark` for standard workbench surfaces and `card-floating-light` / `card-floating-dark` for sticky bars, auth cards, and elevated overlays.

### Forms and Filters

Labels are always visible. Placeholder text may assist but must never replace a label. Errors should appear inline, close to the field, and explain recovery in compact language.

Inputs and selects should use the neutral surface family and visible focus halos derived from `focus-ring`. Compact filters should align with nearby data regions and may adopt the pill grammar when they behave like segmented controls.

Use `input-light` / `input-dark` and `select-trigger-light` / `select-trigger-dark` for theme-aware shared field primitives. Placeholder or disabled text should move to the corresponding muted text token instead of reusing body text color.

Custom date ranges, API Key filters, and credential controls should remain compact and structured. Read-only views still need clear filtering affordances even when write actions are removed.

### Tables, Charts, and Telemetry Views

This product is allowed to be data-dense, but it must remain scannable.

- Metric cards may use one accent color per card family, but the structural chrome stays neutral.
- Tables should rely on alignment, spacing, sticky headers, and restrained badges before additional color.
- Numeric columns and timestamps should align consistently.
- Horizontal scrolling is acceptable for dense tables and charts when the alternative would hide key telemetry.
- Tooltips, legends, and heatmap states must remain legible on both pointer and keyboard interaction paths.

Charts should use the chart accent family, not the primary taupe, for routine data series. Primary taupe is for controls and stateful emphasis, not for arbitrary lines or bars.

### Navigation, Status, and Overlays

Top-level navigation and active tabs should be visibly distinct at a glance. Icon-only actions require accessible names. Status must never rely on color alone; pair color with text, iconography, or count.

Dialogs, reset confirmations, and credential inspection overlays should keep clear titles, direct actions, and consistent action ordering. Focus management should remain intact, and destructive confirmations should explicitly name the affected object or scope.

Use the explicit status badge pairs for durable state chips:

- `status-badge-success-light` / `status-badge-success-dark`
- `status-badge-failure-light` / `status-badge-failure-dark`

## Do's and Don'ts

- Do keep the default light theme warm, paper-like, and low-saturation.
- Do define explicit `-light` and `-dark` component tokens for every new reusable control, card, or badge that must survive theme switching.
- Do preserve the pill-shell plus raised-active-pill pattern for compact switchers and tabs.
- Do use taupe primary for action and selection, not as a generic chart color.
- Do keep data views neutral and let accent colors encode meaning sparingly.
- Do preserve visible labels, focus states, and keyboard reachability.
- Do use monospace for keys, technical identifiers, and quota-style numeric strings.
- Do disable or reduce blur on mobile and reduced-transparency paths.
- Don't replace the warm neutral system with generic white-and-blue enterprise styling.
- Don't spread glass blur, gradients, or glow across normal data cards.
- Don't use placeholder text as a field label.
- Don't collapse important telemetry just to avoid horizontal scrolling.
- Don't add multiple competing primary actions inside the same card or toolbar group.
- Don't rely on color alone to communicate health, failure, or quota state.
- Don't introduce new shared components that infer dark mode only from prose or implementation guesswork; encode the light/dark pairing in tokens.
