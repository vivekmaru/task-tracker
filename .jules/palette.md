## 2024-06-05 - Add confirmation dialog for destructive actions
**Learning:** For destructive actions like deleting artifacts, adding a confirmation dialog prevents accidental data loss and improves user confidence.
**Action:** When adding destructive buttons or forms (like deleting objects, closing important tickets), ensure an `onsubmit` or `onclick` handler with `confirm()` is added if a custom modal is not present.

## 2025-05-22 - Visual Polish and Interactive States for Web UI
**Learning:** The simple server-rendered HTML UI had minimal CSS which lacked visual feedback for interactive elements (buttons, inputs, cards) and accessibility enhancements (focus states, role attributes for errors).
**Action:** Always verify keyboard accessibility (`focus-visible`) and interactive feedback (`hover`, `cursor: pointer`, `transition`) on raw HTML UIs to make them feel responsive and deliberate. Apply `role="alert"` for inline auth error messages.

## 2025-05-23 - Interactive States for Web UI
**Learning:** The simple server-rendered HTML UI had minimal CSS which lacked visual feedback for active elements and disabled states. This means users don't get immediate confirmation when a button is pressed or if it's currently disabled.
**Action:** Always verify that interactive elements like buttons have `:active` and `:disabled` styles in custom CSS to make them feel responsive, deliberate, and accessible.

## 2026-06-26 - Placeholder UX in Form Controls
**Learning:** When adding text guidance inside an input field, it was mistakenly rendered as the `value` attribute instead of the `placeholder` attribute. This results in poor UX where users must manually delete the descriptive text before they can enter their own input, which affects the smoothness of interacting with forms.
**Action:** Always verify that input hints are set using the `placeholder` attribute instead of `value`. Check custom form generation functions (`writeTicketActionForm`, `writeProposedActionForm`) for this pattern.

## 2025-05-23 - Add confirmation dialog for proposed work rejection/archiving
**Learning:** Destructive actions generated via helper functions (like `writeProposedActionForm`) did not have the same safety nets (confirmation dialogs and visual indicators) as similar actions generated elsewhere (like `writeTicketActionForm`). This inconsistency can lead to accidental data loss in the triage queue.
**Action:** When adding or maintaining UI generation helpers, ensure that all destructive branches consistently apply `class="destructive"` and an `onsubmit` confirmation dialog.
