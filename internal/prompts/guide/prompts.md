# Prompts

Every session's system prompt is built from layers, in this order:
1. **Core harness**: built into Hadron, always present, read-only (it is what you are reading the summary of).
2. **Workspace prompt**: optional, edited with `prompt_set {kind: planner|session, template}`.
3. **Launch profile prompt**: optional, the profile's `prompt`.
4. **Card prompt**: optional, set with `card_update {prompt}` or at New session.

Every layer supports `{{workspace.name}}`, `{{workspace.root}}`, `{{columns}}`, `{{integrations}}`, `{{rules}}`, `{{policy}}`, `{{card.nick}}`, `{{card.key}}`, `{{card.links}}`.

`prompt_layers_get {kind, card?}` shows each layer and the composed text; `prompt_render` renders it. Changes apply to sessions that start afterwards. `handoff` and `handoff_planner` are typed once into an adopted conversation.
