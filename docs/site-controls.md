# Shared controls

`static/css/site-controls.css` makes ticket checkout's controls the site default:
6px button corners, IBM Plex Sans labels, orange primary actions, quiet secondary
actions, and visible focus rings. Standard primary actions use 22px bold labels;
navigation, homepage buttons and dense workspaces use the compact 16px version.
Compact orange is darker to keep white labels readable. Size and color variant
selectors use `:where()` so a compact rule cannot split a variant's foreground
and background colors. `.is-secondary.is-on-dark` uses white text on dark sections.

The shared `head_meta` includes the stylesheet for the regular site. Standalone
shop, wizard, signing and admin pages include it directly. Include it once on
new standalone screens, after page-specific styles. Existing button classes
are mapped in the stylesheet so their behavior and layouts can stay in place.

For new controls:

```html
<button type="submit" class="ui-button">Save changes</button>
<a class="ui-button is-secondary" href="/dashboard">Back</a>
<button type="button" class="ui-button is-compact">Copy link</button>
<button type="submit" class="ui-button is-danger">Remove item</button>
```

Use native `disabled` for buttons and `aria-disabled="true"` plus appropriate
interaction handling for links. Styles do not change click handling or enable
disabled actions. Input validation should set `aria-invalid="true"`.

Text fields and selects use checkout's white surface, 48px minimum height,
16px type, straight corners, and orange focus ring. Option cards, steppers,
calendar days, media controls and tabs keep their individual layouts. Existing
display rules, including hidden controls, are preserved.

Antarctica pages (`body.antarctica` and `.expedition-admin`) and the homepage's
`.home-expedition` feature retain their design. `[data-site-controls="off"]`
is an explicit component opt-out. The stylesheet only affects screen media,
so printed receipts, reports and schedules retain their formatting.
