# Questions and gates

**question_ask** (a card session, about its own card): `{kind: decision|unblock|approval, text, context, options: [{label, description}], multi, approval?}`.
- Put your recommendation first and end its label with "(Recommended)".
- `approval` is `{target, runs_as, reversible: yes|no|partial, reversible_note, command, command_lang}` for something that changes things. A partial reversal needs a `reversible_note`. `command_lang` (sql, bash, json, python) highlights the command.
- `meanwhile` says what you do while it waits; the user sees it with the question.
- The card moves to the first Needs-you column and remembers where it was.

**question_answer** `{question_id, selections: [labels], notes}`: the user always may. The planner may under `planner_policy` (allow_all, decisions_only, ask_for_prd, user_only). Under `user_only` the planner answers nothing itself, but may relay the user's own chat answer: `on_behalf_of: "user"` with `source_message_id` (the user's message in this card's channel or the planner channel, written after the question). The answer is recorded `via: planner-relay`. A session never answers its own question. Notes travel with the answer: read them.

**gate_set / gate_clear / gate_withdraw / gate_list**: a gate is a merge, promote, deploy-confirm or close waiting on the user. A session sets it; the user clears it (done or declined). `gate_withdraw` takes back a gate that no longer applies: the session that set it, the planner and the user may. When the last question is answered and the last gate cleared, the card returns to its earlier column.

After `question_answer`, however it was answered, Hadron types `[· answer] Q-NN: <labels>; notes: ...` into the asking card's session (`[· answer, by the planner]` or `[· answer, relayed by the planner]` when the planner answered), so the session carries on by itself. A user's answer is also typed into the planner terminal as `[KEY nick · answer] Q-NN: ...`. If the card's session is not running, its chat says the answer was not delivered.
