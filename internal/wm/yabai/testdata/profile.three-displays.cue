package tessera

// The browser every browser slot claims. Swapping browsers is an edit here;
// slot names and laptop-browser labels stay the same.
#browser: {
	app: "Zen"
	// `open -a` only focuses a running Zen. `-n` starts a second process that
	// hands `--new-window` to the running one and exits, or launches Zen if absent.
	spawn: ["open", "-na", "Zen", "--args", "--new-window"]
}

// One monitor plus the laptop (or lid closed): the AW's windows move into the
// side columns, AW left beside the G9 left, AW right beside the G9 right.
#soloColumns: [
	["browser", "obsidian", "browser"],
	["ghostty-wave", "browser"],
	["ghostty-mbp", "vscode", "browser", "linear"],
]

#laptopStack: {
	display: "laptop"
	label:   "laptop"
	kind:    "stack"
	tracks: [["browser", "akiflow", "spotify", "qalculate"]]
}

#g9Solo: {
	display: "g9"
	label:   "main"
	kind:    "columns"
	tracks:  #soloColumns
}

#awSolo: {
	display: "aw"
	label:   "main"
	kind:    "columns"
	tracks:  #soloColumns
}

// Width is the identity key: macOS reorders display indexes on connect, but
// widths are stable per monitor.
displays: {
	g9: {width: 5120}
	// A smaller ultrawide: its three columns split evenly (AW solo).
	aw: {width: 3440, weights: {columns: {"3": [1, 1, 1]}}}
	laptop: {width: 1728}
}

// App and title matchers are RE2 patterns and match substrings. An empty title
// matches any window of that app.
windows: {
	browser: #browser
	"ghostty-wave": {
		app:   "Ghostty"
		title: "devbox"
		spawn: ["open", "-na", "Ghostty"]
	}
	"ghostty-mbp": {
		app:   "Ghostty"
		title: "mbp"
		spawn: ["open", "-na", "Ghostty"]
	}
	vscode:   {app: "Code", spawn: ["open", "-a", "Visual Studio Code"]}
	akiflow:  {app: "Akiflow", spawn: ["open", "-a", "Akiflow"]}
	obsidian: {app: "Obsidian", spawn: ["open", "-a", "Obsidian"]}
	linear:   {app: "Linear", spawn: ["open", "-a", "Linear"]}
	spotify:  {app: "Spotify", spawn: ["open", "-a", "Spotify"]}
	// The Homebrew app is only linked under ~/Applications, where `open -a` misses it.
	qalculate: {
		app:   "Qalculate"
		spawn: ["open", "-b", "com.yourcompany.qalculate-qt"]
	}
}

// Desk tracks. G9 30/40/30 `main`, AW 50/50 `plan`, MBP single-stack
// `laptop`. track[0] is the anchor, the rest of a track stacks.
desk: [
	{
		display: "g9"
		label:   "main"
		kind:    "columns"
		tracks: [["browser", "obsidian"], ["ghostty-wave", "browser", "vscode"], ["ghostty-mbp", "vscode"]]
	},
	{
		display: "aw"
		label:   "plan"
		kind:    "columns"
		tracks: [["browser"], ["browser", "linear"]]
	},
	#laptopStack,
]

// Single-monitor arrangements. A topology lays out exactly its displays, so
// the lid-closed variant is listed separately.
topologies: [
	{
		name:     "g9-laptop"
		displays: ["g9", "laptop"]
		desk: [#g9Solo, #laptopStack]
	},
	{name: "g9", displays: ["g9"], desk: [#g9Solo]},
	{
		name:     "aw-laptop"
		displays: ["aw", "laptop"]
		desk: [#awSolo, #laptopStack]
	},
	{name: "aw", displays: ["aw"], desk: [#awSolo]},
]

// G9 column weights: 30/40/30.
weights: {columns: {"3": [3, 4, 3]}}

// Numpad 1-9 focus order; onDisplay pins a slot to a display. Browser slots
// resolve to whichever window is on that display.
deskSlots: [
	{name: "ghostty-wave"}, // 1 — the G9 centre wave (primary dev)
	{name: "browser", onDisplay: "g9"}, // 2 — G9 left browser
	{name: "ghostty-mbp"}, // 3 — G9 right MBP terminal
	{name: "browser", onDisplay: "aw"}, // 4 — an AW browser
	{name: "browser", onDisplay: "aw"}, // 5 — an AW browser (stack-cycle to reach the other)
	{name: "linear"}, // 6
	{name: "obsidian"}, // 7
	{name: "akiflow"}, // 8
	{name: "vscode"}, // 9
]

// Laptop pinned core: the stable ordered prefix. Each browser occurrence claims
// a distinct window (occurrence-suffixed labels).
laptopPinned: [
	"browser",
	"ghostty-wave",
	"browser",
	"ghostty-mbp",
	"linear",
	"obsidian",
	"browser",
	"akiflow",
	"browser",
]

// Literal yabai app names, not `windows` keys. These comms/system apps stay on
// the home stack instead of flexing.
laptopStackApps: [
	"Messages",
	"System Settings",
	"Slack",
	"1Password",
	"Activity Monitor",
]


