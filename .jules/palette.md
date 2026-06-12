## 2025-05-22 - Visual Polish and Interactive States for Web UI
**Learning:** The simple server-rendered HTML UI had minimal CSS which lacked visual feedback for interactive elements (buttons, inputs, cards) and accessibility enhancements (focus states, role attributes for errors).
**Action:** Always verify keyboard accessibility (`focus-visible`) and interactive feedback (`hover`, `cursor: pointer`, `transition`) on raw HTML UIs to make them feel responsive and deliberate. Apply `role="alert"` for inline auth error messages.
## 2024-06-12 - Fix placeholder attribute binding in raw HTML
**Learning:** Using raw HTML strings (`fmt.Fprintf`) for template rendering increases the risk of attribute binding errors (e.g., assigning a placeholder value to the `value` attribute). This degrades UX by forcing users to manually clear text before entering input.
**Action:** Always verify that input helpers correctly map arguments to their intended HTML attributes (e.g., `placeholder` vs `value`), especially when migrating or building standard forms without a robust templating system.
