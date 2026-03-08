# Design Tokens & 8pt Grid

## Scope
- Source of truth: `frontend/src/App.css` (`:root` token block)
- Applies to: Home / Project / Settings / System Logs / Task Detail pages

## Token Groups

### Color
- `--color-bg-warm`, `--color-bg-cool`: app background base colors
- `--color-bg-accent-warm`, `--color-bg-accent-cool`: decorative gradient accents
- `--color-surface`, `--color-surface-panel`, `--color-surface-soft`: cards, panels, neutral surfaces
- `--color-text-primary`, `--color-text-secondary`, `--color-text-muted`: text hierarchy
- `--color-border`, `--color-border-strong`: default and emphasized borders
- `--color-brand`, `--color-brand-strong`: primary action gradient
- `--color-brand-alt`, `--color-brand-alt-strong`: secondary action gradient
- `--color-success`, `--color-success-bg`, `--color-success-dot`: success states
- `--color-danger`, `--color-danger-bg`, `--color-danger-dot`: error/danger states
- `--color-focus`: focus ring
- `--color-overlay`: modal mask

### Spacing (4/8 system)
- Base scale: `4px`
- Tokens: `--space-1` to `--space-8` map to `4/8/12/16/20/24/28/32`
- Rule: use `var(--space-*)` for `margin/padding/gap`.
- Rule: avoid ad-hoc values such as `10px`, `14px` for spacing.

### Radius
- `--radius-sm` = `8px`
- `--radius-md` = `12px`
- `--radius-lg` = `16px`
- `--radius-pill` = `999px`

### Shadow
- `--shadow-sm`, `--shadow-md`, `--shadow-lg`: elevation hierarchy
- `--shadow-focus`: keyboard focus outline shadow

### Typography
- `--font-family-base`
- `--font-size-100/200/300/400/500/600`
- `--font-weight-semibold`
- `--line-height-tight`, `--line-height-base`

### Control Height
- `--control-height-md` = `40px` (default input/button minimum)
- `--control-height-lg` = `44px` (key action areas)

## Usage Rules
- Always reference tokens, do not hardcode reusable visual values in components.
- Primary controls (`input/select/textarea/button`) must be at least `40px` tall.
- Key actions in action groups (`.actions`, `.task-card-actions`, `.modal-actions`, `.actions-header`) use `44px` minimum height.
- New components should first select values from existing token scales; extend tokens only when a new semantic value is required.

## Acceptance Checklist
- Main layout spacing follows 4px increments.
- Core surfaces/text/borders/actions use tokenized colors.
- Form controls and main buttons meet minimum height standards.
- New UI blocks can reuse tokens directly without redefining ad-hoc values.
