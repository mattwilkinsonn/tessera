// focus (T4): the numpad slot resolver. Pure over one
// WorldSnapshot against the real profile. deskSlots (1-based): 1 ghostty-wave,
// 2 arc@g9, 3 ghostty-mbp, 4 arc@aw, 5 arc@aw, 6 linear, 7 obsidian,
// 8 akiflow, 9 vscode.

import { describe, expect, test } from "bun:test";
import { profile } from "../config/profile.ts";
import type { SpaceId, WmDisplay, WmWindow } from "../driver/types.ts";
import { resolveSlot } from "./focus.ts";
import type { WorldSnapshot } from "./world.ts";

function win(
	id: number,
	app: string,
	title: string,
	displayIdx: number,
	minimized = false,
): WmWindow {
	return {
		id,
		app,
		title,
		displayIdx,
		spaceId: "s?" as SpaceId,
		minimized,
		floating: false,
		sticky: false,
		visible: true,
		splitType: "none",
		frame: { x: 0, y: 0, w: 100, h: 100 },
	};
}

function display(idx: number, w: number): WmDisplay {
	return { idx, frame: { x: 0, y: 0, w, h: 1440 }, spaceIds: [] };
}

function world(windows: WmWindow[], displays: WmDisplay[] = []): WorldSnapshot {
	return { windows, spaces: [], displays };
}

describe("resolveSlot", () => {
	test("bare-name slot resolves to first matching non-minimized window anywhere", () => {
		// Slot 6 = linear (bare name). Two Linear windows; first in query order wins.
		const w = world([
			win(10, "Arc", "", 0),
			win(11, "Linear", "Inbox", 1),
			win(12, "Linear", "Roadmap", 0),
		]);
		expect(resolveSlot(profile, w, 6)).toBe(11);
	});

	test("bare-name slot honours the WindowSpec title disambiguation", () => {
		// Slot 1 = ghostty-wave: app /Ghostty/, title /pc/. slot 3 = ghostty-mbp:
		// title /mbp/. Both are Ghostty windows disambiguated by title.
		const w = world([
			win(20, "Ghostty", "mbp session", 0),
			win(21, "Ghostty", "pc session", 0),
		]);
		expect(resolveSlot(profile, w, 1)).toBe(21); // wave = pc
		expect(resolveSlot(profile, w, 3)).toBe(20); // mbp
	});

	test("@display slot prefers the candidate on the resolved display", () => {
		// Slot 2 = arc@g9. g9 width 5120 → idx 2. Arc on aw(idx 1) comes first in
		// query order, but the g9 Arc must win.
		const w = world(
			[win(30, "Arc", "", 1), win(31, "Arc", "", 2)],
			[display(1, 3440), display(2, 5120)],
		);
		expect(resolveSlot(profile, w, 2)).toBe(31);
	});

	test("@display slot falls back to first Arc anywhere when none on that display", () => {
		// Slot 2 = arc@g9, g9 present (5120) but no Arc on it → first Arc anywhere.
		const w = world(
			[win(40, "Arc", "", 1), win(41, "Arc", "", 1)],
			[display(1, 3440), display(2, 5120)],
		);
		expect(resolveSlot(profile, w, 2)).toBe(40);
	});

	test("@display slot with absent display returns first Arc anywhere", () => {
		// Slot 2 = arc@g9, no 5120 display connected → prefer is null.
		const w = world(
			[win(50, "Arc", "", 1), win(51, "Arc", "", 1)],
			[display(1, 3440)],
		);
		expect(resolveSlot(profile, w, 2)).toBe(50);
	});

	test("minimized-only match resolves to null", () => {
		// Slot 6 = linear; the only Linear window is minimized.
		const w = world([win(60, "Linear", "Inbox", 0, true)]);
		expect(resolveSlot(profile, w, 6)).toBeNull();
	});

	test("minimized window skipped in favour of a non-minimized match", () => {
		const w = world([
			win(70, "Linear", "Inbox", 0, true),
			win(71, "Linear", "Roadmap", 0),
		]);
		expect(resolveSlot(profile, w, 6)).toBe(71);
	});

	test("out-of-range slots resolve to null", () => {
		const w = world([win(80, "Linear", "", 0)]);
		expect(resolveSlot(profile, w, 0)).toBeNull();
		expect(resolveSlot(profile, w, -1)).toBeNull();
		expect(resolveSlot(profile, w, profile.deskSlots.length + 1)).toBeNull();
	});

	test("slot whose app is absent from the world resolves to null", () => {
		// Slot 7 = obsidian; world has no Obsidian window.
		const w = world([win(90, "Arc", "", 0), win(91, "Linear", "", 0)]);
		expect(resolveSlot(profile, w, 7)).toBeNull();
	});
});
