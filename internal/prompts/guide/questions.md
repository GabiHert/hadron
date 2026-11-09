# Questions and gates

**question_ask** (a card session, about its own card): `{kind: decision|unblock|approval, text, context, options: [{label, description}], multi, approval?}`.
- Put your recommendation first and end its label with "(Recommended)".
- `approval` is `{target, runs_as, reversible, command}` for something that changes things.
- The card moves to the first Needs-you column and remembers where it was.

**question_answer** `{question_id, selections: [labels], notes}`: the user always may. The planner may under `planner_policy` (allow_all, decisions_only, ask_for_prd). A session never answers its own question. Notes travel with the answer: read them.

**gate_set / gate_clear / gate_list**: a gate is a merge, promote, deploy-confirm or close waiting on the user. A session sets it; the user clears it. When the last question is answered and the last gate cleared, the card returns to its earlier column.

The planner relays the user's answer: after `question_answer` Hadron types `[KEY nick] answered Q-NN: <labels>; notes: ...` into the planner terminal.
