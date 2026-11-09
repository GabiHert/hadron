# Reports

`card_report {kind, text}` from a card's own session:
- `progress`: a step finished or a milestone reached.
- `question`: you need a decision (also call `question_ask`).
- `blocked`: you cannot continue; say what resumes you.
- `done`: finished; link the results.

A report is stored on the card's chat and typed into the planner as `[KEY nick · report] <kind>: <first 200 characters>`, so keep the first sentence self-contained. Reports are not for logs or code; use a widget or a link.
