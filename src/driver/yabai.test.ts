// YabaiDriver (T5): the argv-golden contract + the query-JSON normalizers.
//
// The pure halves of the construction-vs-execution split are tested here with no
// live yabai: `yabaiArgs.*` returns the exact CLI argv (the primary contract),
// and the normalizers turn a real `query --windows` capture into camelCase
// WmWindow[]. The one `Bun.$` runner is not exercised (it needs live yabai).

import { describe, expect, test } from "bun:test";
import type { SpaceId } from "./types.ts";
import {
	normalizeDisplay,
	normalizeSpace,
	normalizeWindows,
	type RawYabaiDisplay,
	type RawYabaiSpace,
	type RawYabaiWindow,
	yabaiArgs,
} from "./yabai.ts";

// Real `query --windows` capture (same fixture the engine matcher tests use).
const fixtureWindows = (await Bun.file(
	`${import.meta.dir}/../engine/fixtures/windows.live.json`,
).json()) as RawYabaiWindow[];

describe("yabaiArgs argv goldens", () => {
	test("queries", () => {
		expect(yabaiArgs.queryWindows()).toEqual(["-m", "query", "--windows"]);
		expect(yabaiArgs.queryWindowsOnSpace(3)).toEqual([
			"-m",
			"query",
			"--windows",
			"--space",
			"3",
		]);
		expect(yabaiArgs.queryWindow(2383)).toEqual([
			"-m",
			"query",
			"--windows",
			"--window",
			"2383",
		]);
		expect(yabaiArgs.queryFocusedWindow()).toEqual([
			"-m",
			"query",
			"--windows",
			"--window",
		]);
		expect(yabaiArgs.querySpaces()).toEqual(["-m", "query", "--spaces"]);
		expect(yabaiArgs.queryFocusedSpace()).toEqual([
			"-m",
			"query",
			"--spaces",
			"--space",
		]);
		expect(yabaiArgs.queryDisplays()).toEqual(["-m", "query", "--displays"]);
	});

	test("space lifecycle", () => {
		expect(yabaiArgs.createSpace()).toEqual(["-m", "space", "--create"]);
		expect(yabaiArgs.destroySpace(4)).toEqual([
			"-m",
			"space",
			"4",
			"--destroy",
		]);
		expect(yabaiArgs.labelSpace(4, "lap-arc")).toEqual([
			"-m",
			"space",
			"4",
			"--label",
			"lap-arc",
		]);
		expect(yabaiArgs.setSpaceLayout(4, "bsp")).toEqual([
			"-m",
			"space",
			"4",
			"--layout",
			"bsp",
		]);
		expect(yabaiArgs.moveSpaceToIndex(4, 2)).toEqual([
			"-m",
			"space",
			"4",
			"--move",
			"2",
		]);
		expect(yabaiArgs.balanceSpace()).toEqual(["-m", "space", "--balance"]);
		expect(yabaiArgs.balanceSpace(4)).toEqual([
			"-m",
			"space",
			"4",
			"--balance",
		]);
	});

	test("placement — ratio, insert, resize forms", () => {
		expect(yabaiArgs.moveWindowToSpace(78, 3)).toEqual([
			"-m",
			"window",
			"78",
			"--space",
			"3",
		]);
		expect(yabaiArgs.moveWindowToDisplay(78, 2)).toEqual([
			"-m",
			"window",
			"78",
			"--display",
			"2",
		]);
		expect(yabaiArgs.moveWindowToDisplay(78, "next")).toEqual([
			"-m",
			"window",
			"78",
			"--display",
			"next",
		]);
		expect(yabaiArgs.setSplitRatio(78, 0.3)).toEqual([
			"-m",
			"window",
			"78",
			"--ratio",
			"abs:0.3",
		]);
		expect(yabaiArgs.toggleSplit(78)).toEqual([
			"-m",
			"window",
			"78",
			"--toggle",
			"split",
		]);
		expect(yabaiArgs.toggleFloat(78)).toEqual([
			"-m",
			"window",
			"78",
			"--toggle",
			"float",
		]);
		expect(yabaiArgs.armInsert(78, "east")).toEqual([
			"-m",
			"window",
			"78",
			"--insert",
			"east",
		]);
		expect(yabaiArgs.armInsert(78, "stack")).toEqual([
			"-m",
			"window",
			"78",
			"--insert",
			"stack",
		]);
		expect(yabaiArgs.swapWindows("west")).toEqual([
			"-m",
			"window",
			"--swap",
			"west",
		]);
		expect(yabaiArgs.warpWindow("east")).toEqual([
			"-m",
			"window",
			"--warp",
			"east",
		]);
		//— grow pushes right:100:0, shrink pushes left:-100:0.
		expect(yabaiArgs.resizeWindow("right", 100, 0)).toEqual([
			"-m",
			"window",
			"--resize",
			"right:100:0",
		]);
		expect(yabaiArgs.resizeWindow("left", -100, 0)).toEqual([
			"-m",
			"window",
			"--resize",
			"left:-100:0",
		]);
		expect(yabaiArgs.resizeWindow("top", 0, -100)).toEqual([
			"-m",
			"window",
			"--resize",
			"top:0:-100",
		]);
		expect(yabaiArgs.resizeWindow("bottom", 0, 100)).toEqual([
			"-m",
			"window",
			"--resize",
			"bottom:0:100",
		]);
		expect(yabaiArgs.armInsert(78, "west")).toEqual([
			"-m",
			"window",
			"78",
			"--insert",
			"west",
		]);
		expect(yabaiArgs.armInsert(78, "north")).toEqual([
			"-m",
			"window",
			"78",
			"--insert",
			"north",
		]);
		expect(yabaiArgs.armInsert(78, "south")).toEqual([
			"-m",
			"window",
			"78",
			"--insert",
			"south",
		]);
	});

	test("focus selectors incl. stack + display", () => {
		expect(yabaiArgs.focusWindow(78)).toEqual([
			"-m",
			"window",
			"--focus",
			"78",
		]);
		expect(yabaiArgs.focusWindowDir("east")).toEqual([
			"-m",
			"window",
			"--focus",
			"east",
		]);
		expect(yabaiArgs.focusWindowDir("stack.next")).toEqual([
			"-m",
			"window",
			"--focus",
			"stack.next",
		]);
		expect(yabaiArgs.focusWindowDir("stack.first")).toEqual([
			"-m",
			"window",
			"--focus",
			"stack.first",
		]);
		expect(yabaiArgs.focusWindowDir("stack.prev")).toEqual([
			"-m",
			"window",
			"--focus",
			"stack.prev",
		]);
		expect(yabaiArgs.focusWindowDir("stack.last")).toEqual([
			"-m",
			"window",
			"--focus",
			"stack.last",
		]);
		expect(yabaiArgs.focusDisplay(2)).toEqual([
			"-m",
			"display",
			"--focus",
			"2",
		]);
		expect(yabaiArgs.focusDisplay("next")).toEqual([
			"-m",
			"display",
			"--focus",
			"next",
		]);
	});

	test("rule forms", () => {
		expect(yabaiArgs.ruleList()).toEqual(["-m", "rule", "--list"]);
		expect(yabaiArgs.ruleRemove("auto:code")).toEqual([
			"-m",
			"rule",
			"--remove",
			"auto:code",
		]);
		//— a display-pinned app rule.
		expect(
			yabaiArgs.ruleAdd({ label: "auto:code", app: "^Code$", displayIdx: 1 }),
		).toEqual([
			"-m",
			"rule",
			"--add",
			"label=auto:code",
			"app=^Code$",
			"display=1",
		]);
		//— the catch-all space rule.
		expect(
			yabaiArgs.ruleAdd({ label: "auto:default", app: "^.*$", spaceIdx: 2 }),
		).toEqual([
			"-m",
			"rule",
			"--add",
			"label=auto:default",
			"app=^.*$",
			"space=2",
		]);
		//— a subrole float rule (manage=off).
		expect(
			yabaiArgs.ruleAdd({
				label: "auto:akiflow-dialog",
				app: "^Akiflow$",
				subrole: "AXDialog",
				manage: false,
			}),
		).toEqual([
			"-m",
			"rule",
			"--add",
			"label=auto:akiflow-dialog",
			"app=^Akiflow$",
			"subrole=AXDialog",
			"manage=off",
		]);
		expect(yabaiArgs.ruleApply()).toEqual(["-m", "rule", "--apply"]);
	});

	test("signal add", () => {
		//— a display_added signal wiring a multi-token command.
		expect(
			yabaiArgs.signalAdd("display_added", ["tess", "apply", "--desk"]),
		).toEqual([
			"-m",
			"signal",
			"--add",
			"event=display_added",
			"action=tess apply --desk",
		]);
	});
});

describe("normalizeWindows (kebab query JSON → camelCase WmWindow[])", () => {
	// A minimal spaces snapshot mapping every live index the fixture references
	// to a distinct stable id. The fixture windows live on display 1 across
	// indexes 1-17; give each index a stable id offset so index≠id proves the
	// `.space`→SpaceId resolution is real, not an identity pass.
	const rawSpaces: RawYabaiSpace[] = Array.from({ length: 17 }, (_, i) => {
		const index = i + 1;
		return {
			id: 100 + index, // stable id deliberately ≠ index
			index,
			label: "",
			display: 1,
			windows: [],
			type: "bsp",
		};
	});

	const normalized = normalizeWindows(fixtureWindows, rawSpaces);

	test("resolves .space index to the stable SpaceId (index ≠ id)", () => {
		const ghostty = normalized.find((w) => w.id === 2383);
		expect(ghostty).toBeDefined();
		// Fixture: Ghostty id 2383 on space index 3 → stable id 103.
		expect(ghostty?.app).toBe("Ghostty");
		expect(ghostty?.spaceId).toBe("103" as SpaceId);
		expect(ghostty?.displayIdx).toBe(1);
		expect(ghostty?.title).toBe("pc | π ! model-evals");
	});

	test("Ghostty flags + frame normalize to camelCase", () => {
		const ghostty = normalized.find((w) => w.id === 2383);
		expect(ghostty?.minimized).toBe(false);
		expect(ghostty?.floating).toBe(false);
		expect(ghostty?.sticky).toBe(false);
		expect(ghostty?.visible).toBe(true);
		expect(ghostty?.splitType).toBe("none");
		expect(ghostty?.frame).toEqual({ x: 0, y: 32, w: 1728, h: 1085 });
	});

	test("an Arc window normalizes with its own space resolution", () => {
		// Fixture: Arc id 2444 on space index 2 → stable id 102.
		const arc = normalized.find((w) => w.id === 2444);
		expect(arc?.app).toBe("Arc");
		expect(arc?.spaceId).toBe("102" as SpaceId);
		expect(arc?.title).toBe("Rigel Development");
		expect(arc?.minimized).toBe(false);
		expect(arc?.floating).toBe(false);
	});

	test("throws on a window referencing an unknown space index", () => {
		const orphan: RawYabaiWindow = {
			id: 999,
			app: "Ghost",
			title: "",
			display: 1,
			space: 999,
			"is-minimized": false,
			"is-floating": false,
			"is-sticky": false,
			"is-visible": true,
			"split-type": "none",
			frame: { x: 0, y: 0, w: 1, h: 1 },
		};
		expect(() => normalizeWindows([orphan], rawSpaces)).toThrow(
			/unknown space index 999/,
		);
	});
});

describe("normalizeSpace + normalizeDisplay", () => {
	test("normalizeSpace maps id/label/type→layout and windows→windowIds", () => {
		const raw: RawYabaiSpace = {
			id: 42,
			index: 3,
			label: "main",
			display: 2,
			windows: [78, 2444, 141],
			type: "stack",
		};
		expect(normalizeSpace(raw)).toEqual({
			id: "42" as SpaceId,
			label: "main",
			displayIdx: 2,
			windowIds: [78, 2444, 141],
			layout: "stack",
		});
	});

	test("normalizeSpace falls back to bsp for an unknown type", () => {
		const raw: RawYabaiSpace = {
			id: 42,
			index: 3,
			label: "",
			display: 1,
			windows: [],
			type: "float",
		};
		expect(normalizeSpace(raw).layout).toBe("float");
		expect(normalizeSpace({ ...raw, type: "weird" }).layout).toBe("bsp");
	});

	test("normalizeDisplay resolves ordered space INDEXes to stable SpaceIds", () => {
		const rawSpaces: RawYabaiSpace[] = [
			{
				id: 201,
				index: 1,
				label: "home",
				display: 1,
				windows: [],
				type: "bsp",
			},
			{
				id: 202,
				index: 2,
				label: "plan",
				display: 1,
				windows: [],
				type: "bsp",
			},
		];
		const map = new Map<number, SpaceId>(
			rawSpaces.map((s) => [s.index, String(s.id) as SpaceId]),
		);
		const rawDisplay: RawYabaiDisplay = {
			index: 1,
			frame: { x: 0, y: 0, w: 5120, h: 1440 },
			spaces: [1, 2],
		};
		expect(normalizeDisplay(rawDisplay, map)).toEqual({
			idx: 1,
			frame: { x: 0, y: 0, w: 5120, h: 1440 },
			spaceIds: ["201" as SpaceId, "202" as SpaceId],
		});
	});
});
