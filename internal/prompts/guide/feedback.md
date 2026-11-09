# Feedback

The feedback inbox holds notes about Hadron itself. `feedback_create {kind: bug|idea|annoyance|praise, text, area?, attach_diagnostics?}` files one; the planner does it when the user says "feedback: ...". `feedback_list {status?}` reads the inbox; `feedback_update {id, status, note, github_url}` marks progress (new, planned, in_progress, done, wontfix). Each note has an `issue_url` that opens a prefilled GitHub issue.
