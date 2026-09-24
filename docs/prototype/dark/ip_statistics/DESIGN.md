---
name: Cupertino Safety Gateway
colors:
  surface: '#121317'
  surface-dim: '#121317'
  surface-bright: '#38393d'
  surface-container-lowest: '#0d0e12'
  surface-container-low: '#1a1b1f'
  surface-container: '#1e1f23'
  surface-container-high: '#292a2e'
  surface-container-highest: '#343539'
  on-surface: '#e3e2e7'
  on-surface-variant: '#c1c6d7'
  inverse-surface: '#e3e2e7'
  inverse-on-surface: '#2f3034'
  outline: '#8b90a0'
  outline-variant: '#414755'
  surface-tint: '#adc6ff'
  primary: '#adc6ff'
  on-primary: '#002e69'
  primary-container: '#4b8eff'
  on-primary-container: '#00285c'
  inverse-primary: '#005bc1'
  secondary: '#53e16f'
  on-secondary: '#003911'
  secondary-container: '#05b046'
  on-secondary-container: '#003a11'
  tertiary: '#ffb874'
  on-tertiary: '#4b2800'
  tertiary-container: '#d47b00'
  on-tertiary-container: '#412200'
  error: '#ffb4ab'
  on-error: '#690005'
  error-container: '#93000a'
  on-error-container: '#ffdad6'
  primary-fixed: '#d8e2ff'
  primary-fixed-dim: '#adc6ff'
  on-primary-fixed: '#001a41'
  on-primary-fixed-variant: '#004493'
  secondary-fixed: '#72fe88'
  secondary-fixed-dim: '#53e16f'
  on-secondary-fixed: '#002107'
  on-secondary-fixed-variant: '#00531c'
  tertiary-fixed: '#ffdcbf'
  tertiary-fixed-dim: '#ffb874'
  on-tertiary-fixed: '#2d1600'
  on-tertiary-fixed-variant: '#6a3b00'
  background: '#121317'
  on-background: '#e3e2e7'
  surface-variant: '#343539'
typography:
  display:
    fontFamily: Plus Jakarta Sans
    fontSize: 34px
    fontWeight: '700'
    lineHeight: 41px
    letterSpacing: -0.02em
  display-mobile:
    fontFamily: Plus Jakarta Sans
    fontSize: 28px
    fontWeight: '700'
    lineHeight: 34px
    letterSpacing: -0.015em
  title-1:
    fontFamily: Plus Jakarta Sans
    fontSize: 28px
    fontWeight: '700'
    lineHeight: 34px
    letterSpacing: -0.015em
  title-2:
    fontFamily: Plus Jakarta Sans
    fontSize: 22px
    fontWeight: '600'
    lineHeight: 28px
    letterSpacing: -0.01em
  title-3:
    fontFamily: Plus Jakarta Sans
    fontSize: 20px
    fontWeight: '600'
    lineHeight: 25px
    letterSpacing: -0.008em
  headline:
    fontFamily: Plus Jakarta Sans
    fontSize: 17px
    fontWeight: '600'
    lineHeight: 22px
    letterSpacing: -0.005em
  body:
    fontFamily: Plus Jakarta Sans
    fontSize: 16px
    fontWeight: '400'
    lineHeight: 21px
    letterSpacing: -0.003em
  callout:
    fontFamily: Plus Jakarta Sans
    fontSize: 15px
    fontWeight: '400'
    lineHeight: 20px
    letterSpacing: -0.002em
  subheadline:
    fontFamily: Plus Jakarta Sans
    fontSize: 14px
    fontWeight: '500'
    lineHeight: 18px
  footnote:
    fontFamily: Plus Jakarta Sans
    fontSize: 13px
    fontWeight: '400'
    lineHeight: 18px
  caption-1:
    fontFamily: Plus Jakarta Sans
    fontSize: 12px
    fontWeight: '500'
    lineHeight: 16px
  caption-2:
    fontFamily: Plus Jakarta Sans
    fontSize: 11px
    fontWeight: '600'
    lineHeight: 13px
  code-body:
    fontFamily: JetBrains Mono
    fontSize: 13px
    fontWeight: '400'
    lineHeight: 18px
  code-badge:
    fontFamily: JetBrains Mono
    fontSize: 11px
    fontWeight: '500'
    lineHeight: 14px
    letterSpacing: 0.02em
rounded:
  sm: 0.25rem
  DEFAULT: 0.5rem
  md: 0.75rem
  lg: 1rem
  xl: 1.5rem
  full: 9999px
spacing:
  gutter: 1.25rem
  gutter-mobile: 0.75rem
  margin: 2rem
  margin-mobile: 1rem
  space-xs: 0.25rem
  space-sm: 0.5rem
  space-md: 1rem
  space-lg: 1.5rem
  space-xl: 2.25rem
---

## Brand & Style

The design system establishes a high-trust, mission-critical console for AI safety, payload inspection, and request routing. Grounded in the architectural principles of Apple’s Human Interface Guidelines (HIG), it blends the focus of macOS productivity tools with the fluid refinement of iPadOS and iOS.

### Aesthetic Foundation
- **Tactile Vibrancy & Materials:** Translucent backdrops, precise light-catch borders (1px hairline borders), and multi-tiered materials (`regular`, `thick`, and `ultra-thin` materials) establish layered visual context.
- **Precision Engineering:** Functional density paired with generous negative space. Interfaces never feel cluttered despite housing dense audit tables, policy graphs, and real-time telemetry.
- **Restraint & Purpose:** Color is strictly semantic and functional. The interface leans on monochrome system materials, reserving high-energy system hues exclusively for gateway status, safety ratings, latency warnings, and direct user actions.

## Colors

The system uses Apple-grade dynamic system colors mapped to light and dark modes with continuous semantic roles.

### Semantic Accents
- **System Blue (`#007AFF` / Dark: `#0A84FF`):** Primary action tier, selected states, interactive gateway routes, and informational status.
- **System Green (`#34C759` / Dark: `#30D158`):** Clean prompt tokens, passed safety filters, running microservices, 200 OK responses.
- **System Orange (`#FF9500` / Dark: `#FF9F0A`):** Heuristic warnings, elevated toxicity scores, rate-limit thresholds.
- **System Red (`#FF3B30` / Dark: `#FF453A`):** Prompt injection detected, immediate policy drops, blocked requests, fatal gateway exceptions.
- **System Pink / Magenta (`#FF2D55` / Dark: `#FF375F`):** PII detection, jailbreak signatures, sensitive compliance audits.

### Dynamic Canvas & Material Backgrounds (Dark Mode Default)
- **System Background (`#000000` / Elevated: `#1C1C1E`):** Base canvas layer for canvas screens and full-screen telemetry views.
- **Secondary System Background (`#1C1C1E` / Elevated: `#2C2C2E`):** Distinct card surfaces, grouped table backgrounds, and inspector panels.
- **Tertiary System Background (`#2C2C2E` / Elevated: `#3A3A3C`):** Nested form containers, segmented control tracks, and search fields.
- **Separators & Fills:** 
  - Hairline Border: `rgba(255, 255, 255, 0.12)` (Dark), `rgba(0, 0, 0, 0.08)` (Light).
  - Subtle Fill: `rgba(120, 120, 128, 0.16)`.

## Typography

The type system prioritizes neutral legibility, optical balance, and high numeric readability across dense log feeds.

- **Primary UI Typeface (`Plus Jakarta Sans`):** Selected for its geometric discipline, neo-grotesque neutrality, and balanced apertures that emulate Apple's San Francisco framework.
- **Monospace Typeface (`JetBrains Mono`):** Applied to vector embeddings, token payloads, UUIDs, HTTP request traces, regex filter rules, and performance metrics (latency ms / throughput rps).
- **Proportional Tracking:** Tight negative letter tracking on large headings (`-0.02em`) yields a sleek, authoritative stance, transitioning to neutral and positive tracking for small labels and code segments to maintain legibility.

## Layout & Spacing

The layout is built on an 8-point spatial system, incorporating fluid columns with fixed side navigation bars mirroring macOS Sonoma and iPadOS Split Views.

### Layout Mechanics
- **Split-View Canvas:** 
  - Sidebar: Fixed 260px (desktop), collapsible to a 68px icon bar or sliding sheet on iPad/tablet.
  - Primary Workspace: 12-column fluid grid, dynamically accommodating inspection drawers.
  - Detail Inspector: 360px floating or docked tray for examining selected safety violations.
- **Margins & Gutters:**
  - Desktop: 32px outer canvas margin with 20px grid gutters.
  - Mobile / Compact: 16px outer margin with 12px gutters.
- **Vertical Rhythm:** Grouped content containers utilize standardized padding tiers: `16px` for standard inspection cards, `20px` to `24px` for top-level policy overviews.

## Elevation & Depth

Depth is established through Apple-inspired optical materials, background blurs, and layered elevations rather than heavy drop shadows.

### Material Hierarchy
1. **Base Layer (Elevation 0):** Pure `#000000` (Dark) or `#F2F2F7` (Light). Used for the root background canvas.
2. **Material Cards (Elevation 1):** Solid `#1C1C1E` (Dark) or `#FFFFFF` (Light) backed with `backdrop-filter: blur(20px)`. Outlined by a crisp `1px` inner or outer border of `rgba(255, 255, 255, 0.08)`.
3. **Floating Overlays & Popovers (Elevation 2):** `#2C2C2E` with an ambient, diffused shadow: `0 12px 32px -4px rgba(0, 0, 0, 0.48)`, bordered with `rgba(255, 255, 255, 0.16)`.
4. **Modal Sheets & System Drawers (Elevation 3):** Highest tier, dimming the rest of the canvas with a `rgba(0, 0, 0, 0.4)` scrim.

### Hairline Borders
Every elevated card, badge, and input surface incorporates an ultra-thin 1px border. In dark mode, it acts as a subtle top-lit edge; in light mode, as a soft boundary line defining component boundaries against the canvas.

## Shapes

The design system uses continuous curvature (squircle geometry) to deliver an authentic Cupertino feel across all containers and interactive elements.

- **Primary Cards & Grouped Views:** `18px` to `22px` border radius (`rounded-xl`).
- **Secondary Containers & Inner Modules:** `12px` to `14px` border radius (`rounded-lg`).
- **Interactive Controls (Inputs, Buttons, Segmented Pickers):** `8px` to `10px` border radius (`rounded-md`).
- **Pill Tags, Status Dots, and Toggle Switches:** Full pill geometry (`rounded-full` / `9999px`).

## Components

### Buttons
- **Primary:** Apple System Blue (`#007AFF`) filled button. White text, semi-bold font weight, `10px` radius, subtle top-highlight border (`rgba(255, 255, 255, 0.2)`). Height: `36px` (desktop), `44px` (touch-friendly targets).
- **Secondary (Tonal / Gray):** Subdued surface fill `rgba(120, 120, 128, 0.24)` with full-contrast label. Transitions to `rgba(120, 120, 128, 0.36)` on hover.
- **Destructive:** System Red (`#FF453A`) tinted fill with high-contrast foreground text for irrevocable gateway rule purges.

### Cupertino Segmented Controls
- **Track:** Enclosed capsule in `rgba(120, 120, 128, 0.2)` with `8px` radius and `2px` internal padding.
- **Thumb:** Elevated sliding pill (`#636366` in dark mode or `#FFFFFF` in light mode) casting a fine shadow `0 2px 6px rgba(0,0,0,0.2)`. Used for toggling between "Audit Stream", "Rule Engine", and "Latency Graphs".

### Status Badges & Pill Tags
- **Safety Pill:** Height `22px`, font size `11px` (`JetBrains Mono`), padding `2px 8px`. Formed with a 12% tint background of the respective semantic color, accompanied by a solid 6px dot indicator:
  - Clean: `#30D158` (Green)
  - Flagged: `#FF9F0A` (Orange)
  - Blocked: `#FF453A` (Red)
  - PII Detected: `#FF375F` (Pink)

### Grouped Form Rows & Policy Fields
- Grouped inset rows mirroring iOS system preferences. 
- Left-aligned title (`Plus Jakarta Sans`, regular), right-aligned metadata, toggle switch, or Cupertino chevron accessory.
- Hairline dividers inset by `16px` to match the label baseline, avoiding edge-to-edge separation.

### High-Density Data Tables
- Header: Monospace or low-contrast uppercase `caption-2` with alternating sorting indicators.
- Row Structure: Compact `40px` height with cell-level truncation, monospace payload digests, and hover-triggered quick action buttons (e.g., "Replay Prompt", "Quarantine Client IP").