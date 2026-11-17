# Feedback

The feedback inbox holds notes about Hadron itself, in Settings, Feedback. `feedback_create {kind: bug|idea|annoyance|praise, text, area?, attach_diagnostics?}` files one; the planner does it when the user says "feedback: ...". `feedback_list {status?}` reads the inbox; `feedback_update {id, status, note, github_url}` marks progress (new, planned, in_progress, done, wontfix). A note stays on this Mac until the user shares it.

## Sharing a note on GitHub

Shared feedback is a public issue on GabiHert/hadron, labelled `feedback` and the note's kind. Nothing is ever sent on its own:

1. `feedback_send {id, preview: true}` builds the draft: the title, a body with the text, kind, area, Hadron version and macOS version, and the labels. Folder paths, the user name and the host name are replaced (`removed` says which) unless `keep_details: true`. Diagnostics and logs are never sent.
2. Show the user the title and body and let them edit. `private_hits` lists any of their words to keep private found in the text.
3. Only when the user says so, `feedback_send {id, title, body}` with the text they approved. It is refused while the text holds a private word, and for a note that already has a `github_url` (`shared: true`).

With a token saved (`set_feedback_token`, a fine-grained token with Issues: write, kept in the login keychain) the issue is created and its link recorded on the note. With none, a filled-in new-issue page opens in the browser and the user presses Submit there; the body carries a hidden marker, so the note gets its link once `feedback_list_shared` sees the issue.

`feedback_sharing_get` returns the repo, whether a token is saved and the words to keep private; `feedback_sharing_set {private_words}` replaces that list. The list stays in this Mac's database and is left out of diagnostics. Hadron seeds nothing: the user adds their own words (clients, projects, people).

## Everyone's feedback

`feedback_list_shared {refresh?}` lists the open feedback issues of every user, read through the public API with no token and cached for 3 hours (Settings, Feedback, All users refreshes it when opened). Each has its number, title, kind, version, area, link and, when it is in the inbox, `local_id`. `feedback_import_shared {number}` copies one into the inbox as a note with its link.
