# Design System — UI/UX Guidelines

## Color Palette
- Primary: `#6366F1` (Indigo 500)
- Secondary: `#EC4899` (Pink 500)
- Background: `#0F172A` (Slate 900, dark mode)
- Surface: `#1E293B` (Slate 800)
- Text: `#F8FAFC` (Slate 50)
- Muted: `#94A3B8` (Slate 400)

## Typography
- Font: Inter (Google Fonts)
- Headings: `font-bold tracking-tight`
- Body: `text-base leading-relaxed`

## Spacing
- Base unit: 4px (Tailwind's default)
- Section padding: `p-6` or `p-8`
- Card padding: `p-4`
- Gap between items: `gap-4`

## Components

### Cards
```html
<div class="bg-slate-800 rounded-xl p-4 border border-slate-700 
            hover:border-indigo-500 transition-colors">
  <!-- content -->
</div>
```

### Buttons
```html
<button class="bg-indigo-500 hover:bg-indigo-600 text-white 
               px-4 py-2 rounded-lg font-medium transition-colors">
  Action
</button>
```

## Accessibility
- All interactive elements MUST have `aria-label`
- Color contrast ratio: minimum 4.5:1 (WCAG AA)
- Focus rings: `focus:ring-2 focus:ring-indigo-500`
- Keyboard navigation for all flows
