# Design QA — 0.1.0

Target: the selected graphite horizontal-card concept and warm-ivory icon-grid concept, combined with independently selectable palette, layout and icon presentation. Reference canvases were normalized to 1440 × 1024 for comparison. The 0.1.0 release offered only the top category bar. The local 0.2.0 development build adds a left category sidebar independently of card layout, a clock, and per-category organize buttons. In-app browser checks covered switching between category positions, entering and leaving group organization, and confirming horizontal category scrolling without page overflow at 390px.

## Iterations

- Initial pass: the search field had an unwanted inner border. Fixed selector specificity; later desktop/mobile captures show a single search boundary.
- Interaction pass: editing a bookmark appended it to its group. Preserve its original position; moving to another group still appends to that group.
- Robustness pass: metadata and unsupported icons must keep a usable default. Added bounded common ICO decoding and a bundled icon fallback, including image-load error handling.
- Dense-navigation pass: group tabs scroll rather than widening the document. Organize remains available at the edge.
- Final comparison: full reference/implementation boards and focused card crops were opened together. No remaining P0/P1/P2 layout defects found for the agreed combined direction.

## Required fidelity surfaces

- Typography: native system Chinese/Latin fonts, 14px desktop card titles, compact mobile titles, clear group-heading hierarchy. Smaller production typography than the generated art is intentional; it avoids oversized controls and supports longer real names.
- Spacing/layout: five card columns at 1440px with the standard card configuration; compact mobile two-column cards or three-column icon grid. iPad reflows columns; 4K keeps a bounded 1840px content region instead of stretching each card. No document horizontal overflow observed at 390, 834, 1440 or 3840px.
- Colors/tokens: graphite/sage and ivory/terracotta match the selected directions; ocean and system variants share geometry. Palette, density, icon tile and layout are independent settings.
- Images: actual fetched or uploaded raster icons plus the documented bundled fallback. The implementation does not ship the mockup's invented domains or fabricated logo data. Background API/upload images are bounded and re-encoded; blur affects one background layer.
- Copy/content: real concise controls, Chinese/English labels, empty/search states, editable titles and descriptions. No metric tiles, marketing hero, multi-account UI or LAN/WAN toggle.

## Interaction and responsive verification

In-app browser used exclusively, per user instruction. Checked login, metadata fallback and manual editing, save, drag sorting and refresh persistence, backdrop clicks retaining dialogs, theme combinations, opacity/blur, local image API background application, language toggle, mobile cards/grid and iPad/4K widths. Server tests additionally cover upload validation, Sun-Panel import/merge, privacy boundaries, backup wrong-password rollback and cross-container restoration. Desktop viewport resizing is not a substitute for physical-device touch testing.

Local comparison artifacts and private operational evidence are retained outside this repository. Deployment/API checks are documented separately from visual fidelity; they are not used as substitutes for browser inspection.

final result: passed
