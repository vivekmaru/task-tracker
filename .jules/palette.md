## 2025-05-22 - Visual Polish and Interactive States for Web UI
**Learning:** The simple server-rendered HTML UI had minimal CSS which lacked visual feedback for interactive elements (buttons, inputs, cards) and accessibility enhancements (focus states, role attributes for errors).
**Action:** Always verify keyboard accessibility (`focus-visible`) and interactive feedback (`hover`, `cursor: pointer`, `transition`) on raw HTML UIs to make them feel responsive and deliberate. Apply `role="alert"` for inline auth error messages.
