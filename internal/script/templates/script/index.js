// A user script. A rule runs it with the action run_action {script, action}; the input is
// {card: {id, nick, key, title, assignee, stage, column, fields}, links: [...]}.
// Return {text: "..."} to put a line in the rule log. Declare every host call you use under
// permissions in manifest.json.
var ops = {
  actions: {
    notify_done: function (input) {
      host.notify({ title: "Done", body: input.card.nick + " is done" });
      return { text: input.card.nick + " is done" };
    }
  }
};
