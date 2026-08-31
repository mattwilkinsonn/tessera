// resolveDisplay (T1): width-match display resolution + null-for-absent, the
// topology-portability primitive the D2-corollary rests on.

import { describe, expect, test } from "bun:test";
import { profile } from "../config/profile.ts";
import type { WmDisplay } from "../driver/types.ts";
import { resolveDisplay } from "./display.ts";

function display(idx: number, w: number): WmDisplay {
	return { idx, frame: { x: 0, y: 0, w, h: 1440 }, spaceIds: [] };
}

describe("resolveDisplay (display_idx)", () => {
	test("matches a connected display by stable width", () => {
		// macOS reordered the indexes: aw (3440) is index 1, g9 (5120) is index 2.
		const displays = [display(1, 3440), display(2, 5120), display(3, 1728)];
		expect(resolveDisplay(profile, "g9", displays)).toBe(2);
		expect(resolveDisplay(profile, "aw", displays)).toBe(1);
		expect(resolveDisplay(profile, "laptop", displays)).toBe(3);
	});

	test("returns null for an absent display (the no-laptop Hyprland rig)", () => {
		const displays = [display(1, 5120), display(2, 3440)];
		expect(resolveDisplay(profile, "laptop", displays)).toBeNull();
	});

	test("returns the first match when a width repeats", () => {
		const displays = [display(1, 5120), display(2, 5120)];
		expect(resolveDisplay(profile, "g9", displays)).toBe(1);
	});
});
