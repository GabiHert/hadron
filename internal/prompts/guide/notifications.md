# Notifications

Hadron shows macOS notifications (Notification Center banners) for what needs the user. A click brings Hadron to the front and opens the card, or the planner chat for a planner reply.

Events, each switched on by default:
- `needs_you`: a card arrives in a Needs you column (an open question or a gate). The text says what it waits on.
- `done`, `blocked`: a card session reports done or blocked with `card_report`.
- `planner_reply`: the planner posts a reply with `chat_post` while the Hadron window does not have the focus.
- `message`: the `notify` tool, a rule's `notify` action, and integrations.

Nothing shows for the card the user is already looking at in a focused window. A newer notification for the same card replaces the older one.

Tools:
- `notify {title, body?, card?, workspace_id?, sound?}` posts one (and a toast in the app). `card` (id, nickname or key) is what a click opens. `sound` is `none`, `macos`, `needs_you` or `back_to_work`; left out, the setting decides. The result says `sent` and, if not, the `reason` (a switched-off event, a muted workspace, or macOS).
- `notifications_settings_get` returns the settings and `status.permission` (`allowed`, `denied`, `not_determined`, `unavailable` outside Hadron.app) and `status.alert_style` (`none` means allowed but banners are off).
- `notifications_settings_set {enabled?, events?: {needs_you: bool, ...}, muted_workspaces?: [ids], sound?: off|macos|hadron}` changes them; fields left out are kept. `hadron` plays Hadron's own chimes with a notification. The default is `off`, because Hadron already chimes when a card needs you.
- `notifications_test {card?, sound?}` posts a test notification whatever the switches and mutes say, and warns when macOS will not show it.
- `notifications_open_system_settings` opens Hadron's page in System Settings, Notifications, where the user allows them.

The user sees all of this in Settings, Notifications. macOS asks for permission once, the first time Hadron starts; after a "Don't Allow" only System Settings can change it.
