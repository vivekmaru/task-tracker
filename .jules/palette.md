## 2024-06-05 - Add confirmation dialog for destructive actions
**Learning:** For destructive actions like deleting artifacts, adding a confirmation dialog prevents accidental data loss and improves user confidence.
**Action:** When adding destructive buttons or forms (like deleting objects, closing important tickets), ensure an `onsubmit` or `onclick` handler with `confirm()` is added if a custom modal is not present.
