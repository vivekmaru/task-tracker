## 2025-05-22 - Visual Polish and Interactive States for Web UI
**Learning:** The simple server-rendered HTML UI had minimal CSS which lacked visual feedback for interactive elements (buttons, inputs, cards) and accessibility enhancements (focus states, role attributes for errors).
**Action:** Always verify keyboard accessibility (`focus-visible`) and interactive feedback (`hover`, `cursor: pointer`, `transition`) on raw HTML UIs to make them feel responsive and deliberate. Apply `role="alert"` for inline auth error messages.

## 2025-05-23 - Interactive States for Web UI
**Learning:** The simple server-rendered HTML UI had minimal CSS which lacked visual feedback for active elements and disabled states. This means users don't get immediate confirmation when a button is pressed or if it's currently disabled.
**Action:** Always verify that interactive elements like buttons have `:active` and `:disabled` styles in custom CSS to make them feel responsive, deliberate, and accessible.
