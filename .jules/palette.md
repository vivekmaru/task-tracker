## 2025-05-22 - Visual Polish and Interactive States for Web UI
**Learning:** The simple server-rendered HTML UI had minimal CSS which lacked visual feedback for interactive elements (buttons, inputs, cards) and accessibility enhancements (focus states, role attributes for errors).
**Action:** Always verify keyboard accessibility (`focus-visible`) and interactive feedback (`hover`, `cursor: pointer`, `transition`) on raw HTML UIs to make them feel responsive and deliberate. Apply `role="alert"` for inline auth error messages.

## 2025-05-22 - Proper Input Placeholders vs Values
**Learning:** In the web UI's action forms (like ticket and proposed actions), the `value` attribute was incorrectly used to pass placeholder text, meaning users had to manually delete the pre-filled text before typing their reason.
**Action:** Always use the `placeholder` attribute for hints and instructional text on inputs, and reserve the `value` attribute for actual user data or pre-populated defaults that the user is intended to submit.
