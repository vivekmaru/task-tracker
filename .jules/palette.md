## 2025-05-22 - Visual Polish and Interactive States for Web UI
**Learning:** The simple server-rendered HTML UI had minimal CSS which lacked visual feedback for interactive elements (buttons, inputs, cards) and accessibility enhancements (focus states, role attributes for errors).
**Action:** Always verify keyboard accessibility (`focus-visible`) and interactive feedback (`hover`, `cursor: pointer`, `transition`) on raw HTML UIs to make them feel responsive and deliberate. Apply `role="alert"` for inline auth error messages.

## 2025-05-23 - Interactive States for Web UI
**Learning:** The simple server-rendered HTML UI had minimal CSS which lacked visual feedback for active elements and disabled states. This means users don't get immediate confirmation when a button is pressed or if it's currently disabled.
**Action:** Always verify that interactive elements like buttons have `:active` and `:disabled` styles in custom CSS to make them feel responsive, deliberate, and accessible.

## 2026-06-26 - Placeholder UX in Form Controls
**Learning:** When adding text guidance inside an input field, it was mistakenly rendered as the `value` attribute instead of the `placeholder` attribute. This results in poor UX where users must manually delete the descriptive text before they can enter their own input, which affects the smoothness of interacting with forms.
**Action:** Always verify that input hints are set using the `placeholder` attribute instead of `value`. Check custom form generation functions (`writeTicketActionForm`, `writeProposedActionForm`) for this pattern.
