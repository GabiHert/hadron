package themes

// tok builds a token map from base tokens and the ten tag colours (text, background).
func tok(base map[string]string, tags [10][2]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for i, n := range TagNames {
		out["tag-"+n] = tags[i][0]
		out["tag-"+n+"-b"] = tags[i][1]
	}
	return out
}

func with(tags [10][2]string, over map[string][2]string) [10][2]string {
	for i, n := range TagNames {
		if v, ok := over[n]; ok {
			tags[i] = v
		}
	}
	return tags
}

var classicLightTags = [10][2]string{{"#5c6370", "#eceef1"}, {"#2457b5", "#e3ecfb"}, {"#11756f", "#dcf1ef"}, {"#2f7a4b", "#e2f1e7"}, {"#556a10", "#ebf2d4"}, {"#8a5700", "#fbefd5"}, {"#a24812", "#fbe7da"}, {"#b4322a", "#f9e1df"}, {"#a3316b", "#f8e1ed"}, {"#6b3fb0", "#ece4f8"}}
var classicDarkTags = [10][2]string{{"#b4bac4", "#2a2e35"}, {"#8fb2f0", "#1d2a44"}, {"#6fd0c7", "#163432"}, {"#7fcf9b", "#1f3327"}, {"#bcd86a", "#2b331a"}, {"#e6b65c", "#3a2e14"}, {"#f2a272", "#3d2617"}, {"#f0877d", "#3d1f1d"}, {"#ee8fbe", "#3b1d2c"}, {"#bb9bf0", "#2c2140"}}
var darkTags = [10][2]string{{"#aab1bd", "#2a2f38"}, {"#8fb2ff", "#1f2b45"}, {"#6fd6cc", "#163532"}, {"#7fcf9b", "#1d3126"}, {"#b8d86b", "#2a3317"}, {"#e6b65c", "#372c16"}, {"#f2a26b", "#3a2618"}, {"#f0877d", "#3b2220"}, {"#f08cc0", "#3a1f2f"}, {"#b99af0", "#2b2240"}}

var classicTerm = map[string]string{"tfg": "#ece8df", "tdim": "#9c9da6", "tb": "#8fa3e8", "tg": "#7fcf9b", "ty": "#e6b65c", "tr": "#f0877d", "tm": "#d6a0e8", "tc": "#7fcfd6"}

func merge(ms ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range ms {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// ClassicLight and ClassicDark are the colours of tokens.css; a test keeps them in step.
var ClassicLight = Theme{ID: "classic-light", Name: "Classic Light", Mode: "light", Builtin: true, Tokens: tok(merge(map[string]string{
	"bg": "#fbfbf8", "side": "#f1f2ee", "card": "#ffffff", "fg": "#1d2330", "fg2": "#4b5263", "fg3": "#686e7e", "border": "#dfe1da", "row": "#eceee8", "sel": "#e3e7f5",
	"acc": "#24408e", "on": "#ffffff", "ok": "#2f7a4b", "okb": "#e2f1e7", "warn": "#8a5700", "warnb": "#f8ecd4", "bad": "#b4322a", "badb": "#f6e3e1", "dot": "#d3d5cd", "muted": "#f6f7f3",
	"term": "#151412", "termbar": "#1f1e1c"}, classicTerm), classicLightTags)}

var ClassicDark = Theme{ID: "classic-dark", Name: "Classic Dark", Mode: "dark", Builtin: true, Tokens: tok(merge(map[string]string{
	"bg": "#1b1a18", "side": "#151412", "card": "#232220", "fg": "#ece8df", "fg2": "#bab6ab", "fg3": "#9c9da6", "border": "#35332f", "row": "#2c2a27", "sel": "#2b3142",
	"acc": "#8fa3e8", "on": "#12162a", "ok": "#7fcf9b", "okb": "#1f3327", "warn": "#e6b65c", "warnb": "#3a2e17", "bad": "#f0877d", "badb": "#3b2220", "dot": "#4a4843", "muted": "#1f1e1c",
	"term": "#121110", "termbar": "#191817"}, classicTerm), classicDarkTags)}

// Builtins are the themes Hadron ships, Classic first.
var Builtins = []Theme{
	ClassicLight,
	ClassicDark,
	{ID: "nucleus", Name: "Nucleus", Mode: "light", Builtin: true, Tokens: tok(map[string]string{
		"bg": "#f7f8fb", "side": "#eef0f5", "card": "#ffffff", "fg": "#121726", "fg2": "#454d61", "fg3": "#6a7186", "border": "#dde1ea", "row": "#e9ecf2", "sel": "#e2e8fb",
		"acc": "#2346c8", "on": "#ffffff", "ok": "#1f7a4a", "okb": "#dff2e6", "warn": "#8a5a00", "warnb": "#f8edd2", "bad": "#b42f2a", "badb": "#f8e1df", "dot": "#cfd4df", "muted": "#f2f4f8",
		"term": "#0f1320", "termbar": "#171c2b", "tfg": "#e4e8f2", "tdim": "#8a92a8", "tb": "#86a2ff", "tg": "#6fd39b", "ty": "#eabb5a", "tr": "#f2857b", "tm": "#d79bf5", "tc": "#6fd2e3"}, classicLightTags)},
	{ID: "deep-space", Name: "Deep Space", Mode: "dark", Builtin: true, Tokens: tok(map[string]string{
		"bg": "#0b0e17", "side": "#080a12", "card": "#131826", "fg": "#e6e9f5", "fg2": "#aab1c8", "fg3": "#7d859e", "border": "#232a3d", "row": "#1a2031", "sel": "#1f2945",
		"acc": "#7c9cff", "on": "#0b1030", "ok": "#6fd39b", "okb": "#16301f", "warn": "#eabb5a", "warnb": "#33290f", "bad": "#f2857b", "badb": "#3a1d1c", "dot": "#343c52", "muted": "#0f1320",
		"term": "#070910", "termbar": "#0e1220", "tfg": "#e6e9f5", "tdim": "#7d859e", "tb": "#7c9cff", "tg": "#6fd39b", "ty": "#eabb5a", "tr": "#f2857b", "tm": "#c49bff", "tc": "#5fd6ea"}, darkTags)},
	{ID: "aurora", Name: "Aurora", Mode: "dark", Builtin: true, Tokens: tok(map[string]string{
		"bg": "#2b313c", "side": "#262b35", "card": "#333a47", "fg": "#e5e9f0", "fg2": "#c2c9d6", "fg3": "#a3acbd", "border": "#424b5b", "row": "#3a4250", "sel": "#3d4a5e",
		"acc": "#88c0d0", "on": "#1f2630", "ok": "#a3be8c", "okb": "#36412f", "warn": "#ebcb8b", "warnb": "#46402c", "bad": "#d98a8f", "badb": "#4a3135", "dot": "#4c5567", "muted": "#2f3541",
		"term": "#22272f", "termbar": "#2a303a", "tfg": "#e5e9f0", "tdim": "#8f99ab", "tb": "#81a1c1", "tg": "#a3be8c", "ty": "#ebcb8b", "tr": "#d98a8f", "tm": "#b48ead", "tc": "#88c0d0"},
		with(darkTags, map[string][2]string{"grey": {"#b8c0cc", "#3a414e"}, "blue": {"#88c0d0", "#2b3a48"}, "teal": {"#8fbcbb", "#2a3b3d"}, "green": {"#a3be8c", "#2f3a2e"}}))},
	{ID: "ember", Name: "Ember", Mode: "light", Builtin: true, Tokens: tok(map[string]string{
		"bg": "#f8f5f1", "side": "#f0ebe4", "card": "#fffdfa", "fg": "#231c17", "fg2": "#5a4d43", "fg3": "#7a6b5e", "border": "#e5ddd3", "row": "#eee7de", "sel": "#f6e2d3",
		"acc": "#b4501c", "on": "#ffffff", "ok": "#35703a", "okb": "#e3f0dc", "warn": "#8d5a00", "warnb": "#f8ebcf", "bad": "#b2322c", "badb": "#f7e0dc", "dot": "#d8cec2", "muted": "#f4efe9",
		"term": "#1d1714", "termbar": "#26201b", "tfg": "#efe6dc", "tdim": "#a09184", "tb": "#8fb0e8", "tg": "#9ccf86", "ty": "#f0b75c", "tr": "#f08a72", "tm": "#e29ac0", "tc": "#7fcfc4"}, classicLightTags)},
	{ID: "synthwave", Name: "Synthwave", Mode: "dark", Builtin: true, Tokens: tok(map[string]string{
		"bg": "#130f22", "side": "#0f0c1b", "card": "#1c1631", "fg": "#f2ecff", "fg2": "#bfb2e0", "fg3": "#8f84b5", "border": "#2f2650", "row": "#241d3d", "sel": "#3a1f4f",
		"acc": "#ff4fd8", "on": "#1a0820", "ok": "#3cf0c0", "okb": "#0f3530", "warn": "#ffd166", "warnb": "#3a2e14", "bad": "#ff6b8b", "badb": "#3d1526", "dot": "#3a3160", "muted": "#17122a",
		"term": "#0c0918", "termbar": "#151027", "tfg": "#f2ecff", "tdim": "#8f84b5", "tb": "#6fa8ff", "tg": "#3cf0c0", "ty": "#ffd166", "tr": "#ff6b8b", "tm": "#ff4fd8", "tc": "#4fe3ff"},
		with(darkTags, map[string][2]string{"pink": {"#ff7ad9", "#3d1838"}, "purple": {"#c89bff", "#2e1f4d"}, "teal": {"#4ff0d8", "#123a3a"}}))},
	{ID: "high-contrast", Name: "High Contrast", Mode: "dark", Builtin: true, Tokens: tok(map[string]string{
		"bg": "#000000", "side": "#000000", "card": "#0d0d0d", "fg": "#ffffff", "fg2": "#e0e0e0", "fg3": "#bdbdbd", "border": "#8a8a8a", "row": "#1f1f1f", "sel": "#003a70",
		"acc": "#4db8ff", "on": "#000000", "ok": "#7dff9e", "okb": "#0b3017", "warn": "#ffd24d", "warnb": "#3a2c00", "bad": "#ff8a80", "badb": "#420f0c", "dot": "#8a8a8a", "muted": "#0a0a0a",
		"term": "#000000", "termbar": "#141414", "tfg": "#ffffff", "tdim": "#bdbdbd", "tb": "#8cc8ff", "tg": "#7dff9e", "ty": "#ffd24d", "tr": "#ff8a80", "tm": "#ff9ad5", "tc": "#5ff0e0"},
		[10][2]string{{"#e6e6e6", "#262626"}, {"#8cc8ff", "#0d2440"}, {"#5ff0e0", "#08302c"}, {"#7dff9e", "#0b3017"}, {"#d4ff5c", "#283208"}, {"#ffd24d", "#3a2c00"}, {"#ffad66", "#3d2108"}, {"#ff8a80", "#420f0c"}, {"#ff9ad5", "#3d0f2b"}, {"#d4b0ff", "#2a1347"}})},
}
