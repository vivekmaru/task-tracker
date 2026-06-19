## 2025-05-22 - Visual Polish and Interactive States for Web UI
**Learning:** The simple server-rendered HTML UI had minimal CSS which lacked visual feedback for interactive elements (buttons, inputs, cards) and accessibility enhancements (focus states, role attributes for errors).
**Action:** Always verify keyboard accessibility (`focus-visible`) and interactive feedback (`hover`, `cursor: pointer`, `transition`) on raw HTML UIs to make them feel responsive and deliberate. Apply `role="alert"` for inline auth error messages.

## 2024-06-19 - Missing HTML5 form validation in creation flows
**Learning:** The generic `input` helper used for filters lacks `required` attributes, causing creation forms (Workspace, Project) to rely entirely on backend validation and full page reloads for empty submissions. We also noticed the "Reason" field is required for transition forms, which might be overkill but is required by the backend API currently.
**Action:** Use explicit HTML5 validation (`required`, `aria-required="true"`) for mutation forms instead of the generic filter input helper, to provide instant inline feedback.
